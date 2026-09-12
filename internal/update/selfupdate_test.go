package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAssetName(t *testing.T) {
	got, err := AssetName("v0.2.0", "linux", "arm64")
	if err != nil {
		t.Fatal(err)
	}
	if want := "wrk3_0.2.0_linux_arm64.tar.gz"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	got, err = AssetName("0.2.0", "windows", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if want := "wrk3_0.2.0_windows_amd64.zip"; got != want {
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

func TestFindAssetID(t *testing.T) {
	raw := []byte(`{"tag_name":"v0.3.0","assets":[{"id":11,"name":"checksums.txt"},{"id":22,"name":"wrk3_v0.3.0_linux_amd64.tar.gz"}]}`)
	id, err := findAssetID(raw, "wrk3_v0.3.0_linux_amd64.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	if id != 22 {
		t.Fatalf("got id %d want 22", id)
	}
	if _, err := findAssetID(raw, "missing.tar.gz"); err == nil {
		t.Fatal("expected error for missing asset")
	}
}

func TestDownloadAssetViaAPIStripsAuthOnRedirect(t *testing.T) {
	const payload = "signed-storage-bytes"
	var sawAuthOnStorage bool
	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			sawAuthOnStorage = true
		}
		_, _ = w.Write([]byte(payload))
	}))
	defer storage.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/mytmlt/wrk3/releases/tags/v0.3.0":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"tag_name":"v0.3.0","assets":[{"id":7,"name":"wrk3_v0.3.0_linux_amd64.tar.gz"}]}`)
		case r.URL.Path == "/repos/mytmlt/wrk3/releases/assets/7":
			if r.Header.Get("Accept") != "application/octet-stream" {
				http.Error(w, "want octet-stream", http.StatusBadRequest)
				return
			}
			// Rewrite to a different hostname so the hop models the
			// cross-domain github.com -> signed-storage redirect.
			loc := strings.Replace(storage.URL, "127.0.0.1", "localhost", 1) + "/blob?sig=abc"
			http.Redirect(w, r, loc, http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	oldBase, oldClient, oldRepo := APIBase, httpClientOverride, Repo
	APIBase, httpClientOverride = srv.URL, srv.Client()
	defer func() { APIBase, httpClientOverride, Repo = oldBase, oldClient, oldRepo }()
	t.Setenv("GITHUB_TOKEN", "test-token")
	t.Setenv("GH_TOKEN", "")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	got, err := downloadAssetViaAPI(ctx, "v0.3.0", "wrk3_v0.3.0_linux_amd64.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != payload {
		t.Fatalf("got %q want %q", got, payload)
	}
	if sawAuthOnStorage {
		t.Fatal("Authorization header leaked to redirect storage host")
	}
}
