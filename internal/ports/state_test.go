package ports

import (
	"os"
	"path/filepath"
	"testing"
)

func TestState_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, StateFileName)
	a := Allocator{Base: DefaultBase(), Step: DefaultStep}
	records := []WorktreeRecord{
		{
			Branch:         "feature/foo",
			Slug:           "feature-foo",
			AbsPath:        filepath.Join(dir, "feature-foo"),
			Index:          0,
			Ports:          a.Allocate(0).Ports,
			ComposeProject: "demo-feature-foo",
			Status:         "running",
		},
		{
			Branch:         "feature/bar",
			Slug:           "feature-bar",
			AbsPath:        filepath.Join(dir, "feature-bar"),
			Index:          1,
			Ports:          a.Allocate(1).Ports,
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
		Ports:   Allocator{Base: DefaultBase(), Step: DefaultStep}.Allocate(0).Ports,
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
