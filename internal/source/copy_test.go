package source

import (
	"os"
	"path/filepath"
	"testing"
)

func writeCopyFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCopyIncluded_FileAndDir(t *testing.T) {
	root := t.TempDir()
	wt := t.TempDir()
	writeCopyFile(t, filepath.Join(root, ".env.local"), "secret=1\n")
	writeCopyFile(t, filepath.Join(root, "certs", "a.pem"), "pem\n")

	warns, err := CopyIncluded(root, wt, []string{".env.local", "certs/"})
	if err != nil {
		t.Fatalf("CopyIncluded: %v", err)
	}
	if len(warns) != 0 {
		t.Fatalf("warns = %v, want none", warns)
	}
	got, err := os.ReadFile(filepath.Join(wt, ".env.local"))
	if err != nil || string(got) != "secret=1\n" {
		t.Fatalf("file copy = %q, %v", got, err)
	}
	got, err = os.ReadFile(filepath.Join(wt, "certs", "a.pem"))
	if err != nil || string(got) != "pem\n" {
		t.Fatalf("dir copy = %q, %v", got, err)
	}
}

func TestCopyIncluded_Glob(t *testing.T) {
	root := t.TempDir()
	wt := t.TempDir()
	writeCopyFile(t, filepath.Join(root, "storage", "a.sqlite"), "a")
	writeCopyFile(t, filepath.Join(root, "storage", "b.sqlite"), "b")
	writeCopyFile(t, filepath.Join(root, "storage", "skip.txt"), "x")

	warns, err := CopyIncluded(root, wt, []string{"storage/*.sqlite"})
	if err != nil {
		t.Fatalf("CopyIncluded: %v", err)
	}
	if len(warns) != 0 {
		t.Fatalf("warns = %v, want none", warns)
	}
	for _, f := range []string{"a.sqlite", "b.sqlite"} {
		if _, err := os.Stat(filepath.Join(wt, "storage", f)); err != nil {
			t.Fatalf("want %s copied: %v", f, err)
		}
	}
	if _, err := os.Stat(filepath.Join(wt, "storage", "skip.txt")); !os.IsNotExist(err) {
		t.Fatalf("skip.txt must not copy")
	}
}

func TestCopyIncluded_DoubleStar(t *testing.T) {
	root := t.TempDir()
	wt := t.TempDir()
	writeCopyFile(t, filepath.Join(root, "data", "sub", "deep.txt"), "deep")

	warns, err := CopyIncluded(root, wt, []string{"data/**/*.txt"})
	if err != nil {
		t.Fatalf("CopyIncluded: %v", err)
	}
	if len(warns) != 0 {
		t.Fatalf("warns = %v, want none", warns)
	}
	got, err := os.ReadFile(filepath.Join(wt, "data", "sub", "deep.txt"))
	if err != nil || string(got) != "deep" {
		t.Fatalf("double-star copy = %q, %v", got, err)
	}
}

func TestCopyIncluded_MissingWarns(t *testing.T) {
	root := t.TempDir()
	wt := t.TempDir()

	warns, err := CopyIncluded(root, wt, []string{"nope.env"})
	if err != nil {
		t.Fatalf("CopyIncluded: %v", err)
	}
	if len(warns) != 1 {
		t.Fatalf("warns = %v, want one skip warning", warns)
	}
}

func TestCopyIncluded_NoClobber(t *testing.T) {
	root := t.TempDir()
	wt := t.TempDir()
	writeCopyFile(t, filepath.Join(root, "keep.txt"), "new")
	writeCopyFile(t, filepath.Join(wt, "keep.txt"), "old")

	warns, err := CopyIncluded(root, wt, []string{"keep.txt"})
	if err != nil {
		t.Fatalf("CopyIncluded: %v", err)
	}
	if len(warns) != 1 {
		t.Fatalf("warns = %v, want one exists warning", warns)
	}
	got, _ := os.ReadFile(filepath.Join(wt, "keep.txt"))
	if string(got) != "old" {
		t.Fatalf("existing file overwritten: %q", got)
	}
}

func TestCopyIncluded_RejectsUnsafe(t *testing.T) {
	root := t.TempDir()
	wt := t.TempDir()
	// abs is absolute on any OS (TempDir is always absolute); "/abs"
	// is not absolute on Windows and would only warn, not error.
	abs := filepath.Join(t.TempDir(), "abs")
	for _, p := range []string{abs, "../escape"} {
		if _, err := CopyIncluded(root, wt, []string{p}); err == nil {
			t.Fatalf("pattern %q must error", p)
		}
	}
}

func TestCopyIncluded_SkipsGit(t *testing.T) {
	root := t.TempDir()
	wt := t.TempDir()
	writeCopyFile(t, filepath.Join(root, ".git", "HEAD"), "ref\n")
	writeCopyFile(t, filepath.Join(root, "ok.txt"), "ok")

	warns, err := CopyIncluded(root, wt, []string{".git", "ok.txt"})
	if err != nil {
		t.Fatalf("CopyIncluded: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wt, ".git")); !os.IsNotExist(err) {
		t.Fatalf(".git must never copy")
	}
	if _, err := os.Stat(filepath.Join(wt, "ok.txt")); err != nil {
		t.Fatalf("ok.txt must copy: %v (warns=%v)", err, warns)
	}
}

func TestCopyIncluded_SkipsDotEnv(t *testing.T) {
	root := t.TempDir()
	wt := t.TempDir()
	writeCopyFile(t, filepath.Join(root, ".env"), "APP_PORT=8000\n")
	writeCopyFile(t, filepath.Join(root, ".env.local"), "secret=1\n")
	writeCopyFile(t, filepath.Join(root, "subdir", ".env"), "nested\n")
	writeCopyFile(t, filepath.Join(root, "ok.txt"), "ok")

	warns, err := CopyIncluded(root, wt, []string{".env*", "subdir", "ok.txt"})
	if err != nil {
		t.Fatalf("CopyIncluded: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wt, ".env")); !os.IsNotExist(err) {
		t.Fatalf(".env must never copy (warns=%v)", warns)
	}
	if _, err := os.Stat(filepath.Join(wt, "subdir", ".env")); !os.IsNotExist(err) {
		t.Fatalf("subdir/.env must never copy (warns=%v)", warns)
	}
	got, err := os.ReadFile(filepath.Join(wt, ".env.local"))
	if err != nil || string(got) != "secret=1\n" {
		t.Fatalf(".env.local must copy: %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(wt, "ok.txt")); err != nil {
		t.Fatalf("ok.txt must copy: %v", err)
	}
}

func TestCopyIncluded_EmptyPatterns(t *testing.T) {
	warns, err := CopyIncluded(t.TempDir(), t.TempDir(), nil)
	if err != nil || len(warns) != 0 {
		t.Fatalf("empty = %v, %v; want nil, nil", warns, err)
	}
}
