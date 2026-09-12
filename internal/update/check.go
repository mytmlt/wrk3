package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Tunables (vars so tests can shrink them).
var (
	// Repo is owner/name used for GitHub API + download URLs.
	Repo = "mytmlt/wrk3"
	// APIBase lets tests point at a stub server.
	APIBase = "https://api.github.com"
	// CheckTTL bounds GitHub API calls to once per interval.
	CheckTTL = 24 * time.Hour
	// CheckTimeout caps a single foreground check.
	CheckTimeout = 2 * time.Second
	// httpClientOverride stubs transport in tests.
	httpClientOverride *http.Client
)

// releaseResp is the subset of the GitHub releases/latest payload we need.
type releaseResp struct {
	TagName string `json:"tag_name"`
}

// httpClient returns the test stub or a default client.
func httpClient(timeout time.Duration) *http.Client {
	if httpClientOverride != nil {
		return httpClientOverride
	}
	return &http.Client{Timeout: timeout}
}

// CheckDisabled reports whether the notice is opted out.
func CheckDisabled() bool {
	v := strings.TrimSpace(os.Getenv("WRK3_NO_UPDATE_CHECK"))
	if v == "" {
		return false
	}
	switch strings.ToLower(v) {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

// FetchLatest queries GitHub for the latest release tag (e.g. "v0.2.0").
func FetchLatest(ctx context.Context) (string, error) {
	url := strings.TrimSuffix(APIBase, "/") + "/repos/" + Repo + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("build latest-release request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "wrk3-update-check")
	if tok := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := httpClient(CheckTimeout).Do(req)
	if err != nil {
		return "", fmt.Errorf("query latest release: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read latest release: %w", err)
	}
	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("query latest release: no releases published yet for %s (status 404)", Repo)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("query latest release: unexpected status %s", resp.Status)
	}
	var r releaseResp
	if err := json.Unmarshal(raw, &r); err != nil {
		return "", fmt.Errorf("parse latest release: %w", err)
	}
	if strings.TrimSpace(r.TagName) == "" {
		return "", fmt.Errorf("parse latest release: empty tag_name")
	}
	return strings.TrimSpace(r.TagName), nil
}

// LatestAvailable returns the cached latest tag, refreshing from GitHub
// when the cache is stale. Errors are swallowed (best-effort): a stale or
// empty cache yields "".
func LatestAvailable(current string) string {
	if IsDev(current) || CheckDisabled() {
		return ""
	}
	path, err := CachePath()
	if err != nil {
		return ""
	}
	cached := loadCache(path)
	if cached.Latest != "" && !IsDev(cached.Latest) {
		if !time.Now().UTC().After(cached.CheckedAt.Add(CheckTTL)) {
			return cached.Latest
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), CheckTimeout)
	defer cancel()
	latest, err := FetchLatest(ctx)
	if err != nil {
		// Offline / rate-limited / slow: fall back to stale cache.
		return cached.Latest
	}
	_ = saveCache(path, latest) // best-effort; ignore failure
	return latest
}

// Notice returns "" when up to date, or the stderr hint:
// `A new version of wrk3 is available: vX.Y.Z (you have vA.B.C). Run "wrk3 update" to update.`
func Notice(current string) string {
	latest := LatestAvailable(current)
	if latest == "" || !IsNewer(current, latest) {
		return ""
	}
	return fmt.Sprintf("A new version of wrk3 is available: %s (you have %s). Run \"wrk3 update\" to update.",
		NormalizeVersion(latest), NormalizeVersion(current))
}
