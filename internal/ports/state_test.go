package ports

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestState_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, StateFileName)
	a := Allocator{Base: DefaultBase(), Ranges: DefaultRanges()}
	records := []WorktreeRecord{
		{
			Branch:         "feature/foo",
			Slug:           "feature-foo",
			AbsPath:        filepath.Join(dir, "feature-foo"),
			Index:          0,
			Ports:          a.BaseAllocation(),
			ComposeProject: "demo-feature-foo",
			Status:         "running",
		},
		{
			Branch:         "feature/bar",
			Slug:           "feature-bar",
			AbsPath:        filepath.Join(dir, "feature-bar"),
			Index:          1,
			Ports:          map[string]int{"app": 8001},
			ComposeProject: "demo-feature-bar",
			Status:         "stopped",
		},
	}
	if err := Save(path, records); err != nil {
		t.Fatalf("Save() = %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if len(loaded) != len(records) {
		t.Fatalf("Load() len = %d, want %d", len(loaded), len(records))
	}
	for i := range records {
		if loaded[i].Branch != records[i].Branch ||
			loaded[i].Slug != records[i].Slug ||
			loaded[i].AbsPath != records[i].AbsPath ||
			loaded[i].Index != records[i].Index ||
			loaded[i].ComposeProject != records[i].ComposeProject ||
			loaded[i].Status != records[i].Status {
			t.Errorf("record %d mismatch: got %+v, want %+v", i, loaded[i], records[i])
		}
		for name, want := range records[i].Ports {
			if loaded[i].Ports[name] != want {
				t.Errorf("record %d port %q = %d, want %d", i, name, loaded[i].Ports[name], want)
			}
		}
		if !filepath.IsAbs(loaded[i].AbsPath) {
			t.Errorf("record %d AbsPath not absolute: %q", i, loaded[i].AbsPath)
		}
	}
}

func TestState_RejectsRelativePaths(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, StateFileName)
	bad := []WorktreeRecord{{
		Branch:  "feature/foo",
		Slug:    "feature-foo",
		AbsPath: "relative/path",
		Index:   0,
		Ports:   Allocator{Base: DefaultBase(), Ranges: DefaultRanges()}.BaseAllocation(),
	}}
	if err := Save(path, bad); err == nil {
		t.Errorf("Save() with relative AbsPath = nil, want error")
	}
	// Write a bad file by hand; Load must reject it.
	raw := `[{"branch":"b","slug":"s","absPath":"rel","index":0,"ports":{},"composeProject":"p","status":"s"}]`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Errorf("Load() with relative AbsPath = nil, want error")
	}
}

func TestState_LoadMissingReturnsEmpty(t *testing.T) {
	loaded, err := Load(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err != nil {
		t.Fatalf("Load() missing = %v, want nil", err)
	}
	if len(loaded) != 0 {
		t.Fatalf("Load() missing len = %d, want 0", len(loaded))
	}
}

func TestStatePath(t *testing.T) {
	base := filepath.Join("tmp", "wt")
	if got := StatePath(base); got != filepath.Join(base, StateFileName) {
		t.Errorf("StatePath() = %q", got)
	}
}

func TestState_RoundTripWithURLs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, StateFileName)
	records := []WorktreeRecord{
		{
			Branch:         "feature/foo",
			Slug:           "feature-foo",
			AbsPath:        filepath.Join(dir, "feature-foo"),
			Index:          0,
			Ports:          map[string]int{"app": 8000},
			Urls:           map[string]int{"APP_URL": 8000},
			ComposeProject: "demo-feature-foo",
			Status:         "running",
		},
		{
			Branch:         "feature/bar",
			Slug:           "feature-bar",
			AbsPath:        filepath.Join(dir, "feature-bar"),
			Index:          1,
			Ports:          map[string]int{"app": 8001},
			Urls:           map[string]int{"APP_URL": 8001, "BASE_URL": 9000},
			ComposeProject: "demo-feature-bar",
			Status:         "stopped",
		},
	}
	if err := Save(path, records); err != nil {
		t.Fatalf("Save() = %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if len(loaded) != len(records) {
		t.Fatalf("Load() len = %d, want %d", len(loaded), len(records))
	}
	for i := range records {
		if loaded[i].Urls == nil && len(records[i].Urls) > 0 {
			t.Errorf("record %d Urls is nil, want %v", i, records[i].Urls)
			continue
		}
		for varName, want := range records[i].Urls {
			if loaded[i].Urls[varName] != want {
				t.Errorf("record %d URL %q = %d, want %d", i, varName, loaded[i].Urls[varName], want)
			}
		}
	}
}

func TestState_LoadOldFormatWithoutUrls(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, StateFileName)
	// Old format state without urls field must load without error.
	// Marshal a record with nil Urls so the urls field is omitted (omitempty),
	// with proper JSON escaping of AbsPath on all platforms (e.g. Windows).
	rawBytes, err := json.Marshal([]WorktreeRecord{{
		Branch:         "feat-a",
		Slug:           "feat-a",
		AbsPath:        filepath.Join(dir, "feat-a"),
		Index:          0,
		Ports:          map[string]int{"app": 8000},
		ComposeProject: "demo-feat-a",
		Status:         "stopped",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, rawBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("Load() len = %d, want 1", len(loaded))
	}
	if loaded[0].Urls != nil {
		t.Errorf("expected nil Urls for old format, got %v", loaded[0].Urls)
	}
}
