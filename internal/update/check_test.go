package update

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func withCacheDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	oldOverride := cachePathOverride
	oldCache, oldXDG, oldNoCheck := os.Getenv("WRK3_CACHE_HOME"), os.Getenv("XDG_CACHE_HOME"), os.Getenv("WRK3_NO_UPDATE_CHECK")
	cachePathOverride = filepath.Join(dir, "latest-check.json")
	t.Cleanup(func() {
		cachePathOverride = oldOverride
		_ = os.Setenv("WRK3_CACHE_HOME", oldCache)
		if oldXDG == "" {
			_ = os.Unsetenv("XDG_CACHE_HOME")
		} else {
			_ = os.Setenv("XDG_CACHE_HOME", oldXDG)
		}
		if oldNoCheck == "" {
			_ = os.Unsetenv("WRK3_NO_UPDATE_CHECK")
		} else {
			_ = os.Setenv("WRK3_NO_UPDATE_CHECK", oldNoCheck)
		}
		_ = os.Unsetenv("WRK3_NO_UPDATE_CHECK")
	})
}

func TestSaveAndFreshCache(t *testing.T) {
	withCacheDir(t)
	path, err := CachePath()
	if err != nil {
		t.Fatalf("CachePath: %v", err)
	}
	if err := saveCache(path, "v0.2.0"); err != nil {
		t.Fatalf("saveCache: %v", err)
	}
	c := loadCache(path)
	if c.Latest != "v0.2.0" {
		t.Fatalf("loadCache latest = %q", c.Latest)
	}
	if time.Since(c.CheckedAt) > time.Minute {
		t.Fatalf("CheckedAt not recent: %v", c.CheckedAt)
	}
}

func TestNoticeUsesFreshCacheWithoutNetwork(t *testing.T) {
	withCacheDir(t)
	path, _ := CachePath()
	if err := saveCache(path, "v9.9.9"); err != nil {
		t.Fatal(err)
	}
	// Point API at an unreachable stub; fresh cache must win without HTTP.
	oldBase, oldClient := APIBase, httpClientOverride
	APIBase = "http://127.0.0.1:1"
	httpClientOverride = &http.Client{Timeout: time.Millisecond}
	t.Cleanup(func() { APIBase, httpClientOverride = oldBase, oldClient })

	if got := Notice("v0.1.0"); got == "" {
		t.Fatal("expected notice from fresh cache, got empty")
	}
	if got := Notice("v9.9.9"); got != "" {
		t.Fatalf("expected no notice when current == latest, got %q", got)
	}
}

func TestNoticeStaleCacheFallsBackOnError(t *testing.T) {
	withCacheDir(t)
	path, _ := CachePath()
	// Write a stale cache entry manually (checked 48h ago).
	old := checkCache{Latest: "v9.9.9", CheckedAt: time.Now().UTC().Add(-48 * time.Hour)}
	raw := `{"latest":"` + old.Latest + `","checkedAt":"` + old.CheckedAt.Format(time.RFC3339) + `"}`
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	oldBase, oldClient := APIBase, httpClientOverride
	APIBase, httpClientOverride = srv.URL, srv.Client()
	t.Cleanup(func() { APIBase, httpClientOverride = oldBase, oldClient })

	if got := Notice("v0.1.0"); got == "" {
		t.Fatal("expected fallback to stale cache on API error")
	}
}

func TestNoticeSkipsDevAndOptOut(t *testing.T) {
	withCacheDir(t)
	if got := Notice("dev"); got != "" {
		t.Fatalf("dev build should never nag, got %q", got)
	}
	path, _ := CachePath()
	if err := saveCache(path, "v9.9.9"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WRK3_NO_UPDATE_CHECK", "1")
	if got := Notice("v0.1.0"); got != "" {
		t.Fatalf("opt-out should suppress notice, got %q", got)
	}
}
