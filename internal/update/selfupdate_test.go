package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
)

func TestAssetName(t *testing.T) {
	got, err := AssetName("v0.2.0", "linux", "arm64")
	if err != nil {
		t.Fatal(err)
	}
	if want := "wrk3_v0.2.0_linux_arm64.tar.gz"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	got, err = AssetName("0.2.0", "windows", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if want := "wrk3_v0.2.0_windows_amd64.zip"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if _, err := AssetName("v0.2.0", "plan9", "amd64"); err == nil {
		t.Fatal("expected error for unsupported os")
	}
	if _, err := AssetName("v0.2.0", "windows", "arm64"); err == nil {
		t.Fatal("expected error for windows/arm64 (no asset built)")
	}
}

func TestVerifyChecksum(t *testing.T) {
	data := []byte("fake-binary")
	sum := sha256.Sum256(data)
	sums := fmt.Sprintf("%s  wrk3_v0.2.0_linux_amd64.tar.gz\n", hex.EncodeToString(sum[:]))
	if err := VerifyChecksum("wrk3_v0.2.0_linux_amd64.tar.gz", sums, data); err != nil {
		t.Fatalf("valid checksum rejected: %v", err)
	}
	if err := VerifyChecksum("wrk3_v0.2.0_linux_amd64.tar.gz", sums, []byte("tampered")); err == nil {
		t.Fatal("tampered payload should fail verification")
	}
	if err := VerifyChecksum("other.tar.gz", sums, data); err == nil {
		t.Fatal("missing entry should fail verification")
	}
}

func makeTarGz(t *testing.T, name string, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(data))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func makeZip(t *testing.T, name string, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestExtractBinary(t *testing.T) {
	payload := []byte("ELF-fake")
	pkg := makeTarGz(t, "wrk3", payload)
	got, err := extractBinary(pkg, "tar.gz", "linux")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("tar.gz payload mismatch")
	}
	zpkg := makeZip(t, "wrk3.exe", payload)
	got, err = extractBinary(zpkg, "zip", "windows")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("zip payload mismatch")
	}
	if _, err := extractBinary(pkg, "tar.gz", "windows"); err == nil {
		t.Fatal("expected missing wrk3.exe in linux package")
	}
}
