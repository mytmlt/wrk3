package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// DownloadBase lets tests point at a stub server.
var DownloadBase = "https://github.com"

// DownloadTimeout caps a full self-update download.
var DownloadTimeout = 2 * time.Minute

// ErrAlreadyUpToDate signals update was a no-op.
var ErrAlreadyUpToDate = errors.New("already up to date")

// exePathOverride stubs os.Executable in tests.
var exePathOverride string

// AssetName maps version+platform to the GoReleaser asset name, e.g.
// wrk3_v0.2.0_linux_arm64.tar.gz (windows uses .zip).
func AssetName(version, goos, goarch string) (string, error) {
	switch goos {
	case "linux", "darwin", "windows":
	default:
		return "", fmt.Errorf("unsupported os %q (supported: linux, darwin, windows)", goos)
	}
	switch goarch {
	case "amd64", "arm64":
	default:
		return "", fmt.Errorf("unsupported arch %q (supported: amd64, arm64)", goarch)
	}
	if goos == "windows" && goarch == "arm64" {
		return "", fmt.Errorf("unsupported platform windows/arm64 (no release asset built)")
	}
	ext := "tar.gz"
	if goos == "windows" {
		ext = "zip"
	}
	return fmt.Sprintf("wrk3_%s_%s_%s.%s", NormalizeVersion(version), goos, goarch, ext), nil
}

// AssetURL returns the release download URL for an asset file.
func AssetURL(version, asset string) string {
	return strings.TrimSuffix(DownloadBase, "/") + "/repos/" + Repo + "/releases/download/" +
		NormalizeVersion(version) + "/" + asset
}

// ChecksumURL returns the checksums.txt download URL for a release.
func ChecksumURL(version string) string {
	return strings.TrimSuffix(DownloadBase, "/") + "/repos/" + Repo + "/releases/download/" +
		NormalizeVersion(version) + "/checksums.txt"
}

// VerifyChecksum checks asset bytes against the checksums.txt entry for asset.
func VerifyChecksum(asset, checksumsText string, data []byte) error {
	want := ""
	for _, line := range strings.Split(checksumsText, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		// Format: "<sha256>  <filename>"; filename may include a path.
		name := fields[len(fields)-1]
		if name == asset || strings.HasSuffix(name, "/"+asset) {
			want = fields[0]
			break
		}
	}
	if want == "" {
		return fmt.Errorf("checksum entry for %q not found", asset)
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); !strings.EqualFold(got, strings.TrimSpace(want)) {
		return fmt.Errorf("checksum mismatch for %q", asset)
	}
	return nil
}

// CurrentExePath resolves the running binary (symlinks evaluated).
func CurrentExePath() (string, error) {
	if exePathOverride != "" {
		return exePathOverride, nil
	}
	p, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate current binary: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		p = resolved
	}
	return p, nil
}

// download fetches url into memory (capped at 256 MiB).
func download(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build download request: %w", err)
	}
	req.Header.Set("User-Agent", "wrk3-update")
	if tok := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	client := httpClient(DownloadTimeout)
	if httpClientOverride == nil {
		client = &http.Client{Timeout: DownloadTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: unexpected status %s", url, resp.Status)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 256<<20))
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	return raw, nil
}

// binaryName returns the expected binary name inside the archive.
func binaryName(goos string) string {
	if goos == "windows" {
		return "wrk3.exe"
	}
	return "wrk3"
}

// extractBinary pulls the wrk3 binary out of a .tar.gz/.zip package.
func extractBinary(pkg []byte, ext, goos string) ([]byte, error) {
	want := binaryName(goos)
	switch ext {
	case "tar.gz":
		return extractTarGz(pkg, want)
	case "zip":
		return extractZip(pkg, want)
	default:
		return nil, fmt.Errorf("unknown package extension %q", ext)
	}
}

func extractTarGz(pkg []byte, want string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(pkg))
	if err != nil {
		return nil, fmt.Errorf("open package: %w", err)
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read package: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if filepath.Base(hdr.Name) != want {
			continue
		}
		raw, err := io.ReadAll(io.LimitReader(tr, 256<<20))
		if err != nil {
			return nil, fmt.Errorf("extract %q: %w", want, err)
		}
		if len(raw) == 0 {
			return nil, fmt.Errorf("extract %q: empty binary", want)
		}
		return raw, nil
	}
	return nil, fmt.Errorf("binary %q not found in package", want)
}

func extractZip(pkg []byte, want string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(pkg), int64(len(pkg)))
	if err != nil {
		return nil, fmt.Errorf("open package: %w", err)
	}
	for _, f := range zr.File {
		if filepath.Base(f.Name) != want {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("extract %q: %w", want, err)
		}
		raw, err := io.ReadAll(io.LimitReader(rc, 256<<20))
		_ = rc.Close()
		if err != nil {
			return nil, fmt.Errorf("extract %q: %w", want, err)
		}
		if len(raw) == 0 {
			return nil, fmt.Errorf("extract %q: empty binary", want)
		}
		return raw, nil
	}
	return nil, fmt.Errorf("binary %q not found in package", want)
}

// replaceExe atomically swaps the running binary with newBin.
func replaceExe(exePath string, newBin []byte) error {
	dir := filepath.Dir(exePath)
	tmp, err := os.CreateTemp(dir, "wrk3-update-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp binary: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(newBin); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp binary: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp binary: %w", err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(tmpName, 0o755); err != nil {
			return fmt.Errorf("chmod temp binary: %w", err)
		}
	}
	if err := os.Rename(tmpName, exePath); err != nil {
		// Windows locks the running .exe; stage a .new file instead.
		if runtime.GOOS == "windows" {
			staged := exePath + ".new"
			if err2 := os.Rename(tmpName, staged); err2 != nil {
				return fmt.Errorf("replace binary %q: %w (staged copy also failed: %v)", exePath, err, err2)
			}
			return fmt.Errorf("replace binary %q: %w (new binary staged at %q; move it into place after restart)",
				exePath, err, staged)
		}
		return fmt.Errorf("replace binary %q: %w", exePath, err)
	}
	return nil
}

// fetchLatestLong uses a generous timeout for explicit update runs
// (unlike the 2s foreground notice check).
func fetchLatestLong(ctx context.Context) (string, error) {
	// Temporarily widen the client timeout for this call.
	prev := httpClientOverride
	if prev == nil {
		httpClientOverride = &http.Client{Timeout: 30 * time.Second}
		defer func() { httpClientOverride = nil }()
	}
	return FetchLatest(ctx)
}

// UpdateTo downloads target (or "latest" when empty) and replaces the
// running binary. Returns the installed version, or ErrAlreadyUpToDate.
func UpdateTo(ctx context.Context, current, target string) (string, error) {
	if strings.TrimSpace(target) == "" || strings.TrimSpace(target) == "latest" {
		latest, err := fetchLatestLong(ctx)
		if err != nil {
			return "", fmt.Errorf("resolve latest release: %w", err)
		}
		target = latest
	}
	target = NormalizeVersion(strings.TrimSpace(target))
	if !IsDev(current) && !IsNewer(current, target) && Normalize(current) == Normalize(target) {
		return target, ErrAlreadyUpToDate
	}
	asset, err := AssetName(target, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return "", err
	}
	ext := "tar.gz"
	if runtime.GOOS == "windows" {
		ext = "zip"
	}
	pkg, err := download(ctx, AssetURL(target, asset))
	if err != nil {
		return "", err
	}
	sums, err := download(ctx, ChecksumURL(target))
	if err != nil {
		return "", fmt.Errorf("download checksums: %w", err)
	}
	if err := VerifyChecksum(asset, string(sums), pkg); err != nil {
		return "", err
	}
	bin, err := extractBinary(pkg, ext, runtime.GOOS)
	if err != nil {
		return "", err
	}
	exe, err := CurrentExePath()
	if err != nil {
		return "", err
	}
	if err := replaceExe(exe, bin); err != nil {
		return "", err
	}
	// Refresh the notice cache so the hint disappears immediately.
	if path, err := CachePath(); err == nil {
		_ = saveCache(path, target)
	}
	return target, nil
}
