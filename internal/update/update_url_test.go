package update

import (
	"strings"
	"testing"
)

// The "/repos/<owner>/<repo>" prefix belongs to the api.github.com shape
// (see FetchLatest); browser download URLs must not contain it, or every
// self-update download 404s (v0.8.0: "unexpected status 404 Not Found").
func TestAssetURLShape(t *testing.T) {
	asset, err := AssetName("v0.8.0", "darwin", "arm64")
	if err != nil {
		t.Fatal(err)
	}
	got := AssetURL("v0.8.0", asset)
	want := "https://github.com/mytmlt/wrk3/releases/download/v0.8.0/wrk3_0.8.0_darwin_arm64.tar.gz"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if strings.Contains(got, "/repos/") {
		t.Fatalf("download URL must not use the API /repos/ prefix: %q", got)
	}
}

func TestChecksumURLShape(t *testing.T) {
	got := ChecksumURL("v0.8.0")
	want := "https://github.com/mytmlt/wrk3/releases/download/v0.8.0/checksums.txt"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if strings.Contains(got, "/repos/") {
		t.Fatalf("download URL must not use the API /repos/ prefix: %q", got)
	}
}
