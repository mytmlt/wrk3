package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("project: {worktreeBase: .w}\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestDiscoverExplicit(t *testing.T) {
	got, err := DiscoverFile("rel/wrk3.yaml", "/tmp/base")
	if err != nil {
		t.Fatalf("DiscoverFile: %v", err)
	}
	// Explicit paths absolutize against the process cwd; just check the
	// returned path is absolute and has the right base name.
	if !filepath.IsAbs(got) {
		t.Fatalf("expected absolute path, got %q", got)
	}
	if filepath.Base(got) != "wrk3.yaml" {
		t.Fatalf("base = %q, want wrk3.yaml", filepath.Base(got))
	}
}

func TestDiscoverPrefersYamlOverYml(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "wrk3.yml"))
	writeFile(t, filepath.Join(root, "wrk3.yaml"))
	got, err := DiscoverFile("", root)
	if err != nil {
		t.Fatalf("DiscoverFile: %v", err)
	}
	if filepath.Base(got) != "wrk3.yaml" {
		t.Fatalf("got %q, want wrk3.yaml", got)
	}
}

func TestDiscoverYmlFallback(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "wrk3.yml"))
	got, err := DiscoverFile("", root)
	if err != nil {
		t.Fatalf("DiscoverFile: %v", err)
	}
	if filepath.Base(got) != "wrk3.yml" {
		t.Fatalf("got %q, want wrk3.yml", got)
	}
}

func TestDiscoverNearestDirWins(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "wrk3.yaml"))
	sub := filepath.Join(root, "a", "b")
	writeFile(t, filepath.Join(sub, "wrk3.yml"))
	got, err := DiscoverFile("", filepath.Join(sub, "deep"))
	if err != nil {
		t.Fatalf("DiscoverFile: %v", err)
	}
	want := filepath.Join(sub, "wrk3.yml")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestDiscoverMissing(t *testing.T) {
	got, err := DiscoverFile("", t.TempDir())
	if err != nil {
		t.Fatalf("DiscoverFile: %v", err)
	}
	if got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}
