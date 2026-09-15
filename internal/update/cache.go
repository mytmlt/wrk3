package update

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// checkCache is the on-disk shape of the latest-release check cache.
type checkCache struct {
	Latest    string    `json:"latest"`
	CheckedAt time.Time `json:"checkedAt"`
}

// cachePathOverride stubs the cache location in tests.
var cachePathOverride string

// CachePath resolves the version-check cache file.
// Precedence: $WRK3_CACHE_HOME/wrk3/latest-check.json >
// $XDG_CACHE_HOME/wrk3/latest-check.json > os.UserCacheDir()/wrk3/... >
// ~/.cache/wrk3/latest-check.json.
func CachePath() (string, error) {
	if cachePathOverride != "" {
		return cachePathOverride, nil
	}
	if v := os.Getenv("WRK3_CACHE_HOME"); v != "" {
		return filepath.Join(v, "wrk3", "latest-check.json"), nil
	}
	if v := os.Getenv("XDG_CACHE_HOME"); v != "" {
		return filepath.Join(v, "wrk3", "latest-check.json"), nil
	}
	if dir, err := os.UserCacheDir(); err == nil && dir != "" {
		return filepath.Join(dir, "wrk3", "latest-check.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve cache home: %w", err)
	}
	return filepath.Join(home, ".cache", "wrk3", "latest-check.json"), nil
}

// loadCache reads the cache file; missing/empty/corrupt yields zero value.
func loadCache(path string) checkCache {
	var c checkCache
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 {
		return c
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return checkCache{}
	}
	return c
}

// saveCache writes latest + now atomically (temp file + rename).
func saveCache(path, latest string) error {
	if latest == "" {
		return fmt.Errorf("latest version must not be empty")
	}
	c := checkCache{Latest: latest, CheckedAt: time.Now().UTC()}
	raw, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("encode update cache: %w", err)
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create cache dir %q: %w", filepath.Dir(path), err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "latest-check-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp cache file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp cache file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp cache file: %w", err)
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return fmt.Errorf("chmod temp cache file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace cache %q: %w", path, err)
	}
	return nil
}
