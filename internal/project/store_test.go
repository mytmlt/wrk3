package project

import (
	"os"
	"path/filepath"
	"testing"
)

// newTestStore creates a FileStore isolated to t.TempDir via WithPath.
func newTestStore(t *testing.T) *FileStore {
	t.Helper()
	s, err := NewFileStore(WithPath(filepath.Join(t.TempDir(), "projects.yaml")))
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	return s
}

func TestAddGet(t *testing.T) {
	s := newTestStore(t)
	want := filepath.Join(t.TempDir(), "wrk3.yaml")
	if err := s.Add("demo", want); err != nil {
		t.Fatalf("Add: %v", err)
	}
	p, err := s.Get("demo")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if p.Name != "demo" {
		t.Fatalf("Name = %q, want demo", p.Name)
	}
	if p.ConfigPath != filepath.Clean(want) {
		t.Fatalf("ConfigPath = %q, want %q", p.ConfigPath, filepath.Clean(want))
	}
	if p.AddedAt.IsZero() {
		t.Fatal("AddedAt must be set")
	}
}

func TestAddRejectsEmpty(t *testing.T) {
	s := newTestStore(t)
	if err := s.Add("", "/tmp/x/wrk3.yaml"); err == nil {
		t.Fatal("Add with empty name must fail")
	}
	if err := s.Add("demo", ""); err == nil {
		t.Fatal("Add with empty config path must fail")
	}
}

func TestAddResolvesRelativeToAbsolute(t *testing.T) {
	s := newTestStore(t)
	workdir := t.TempDir()
	rel := "rel/wrk3.yaml"
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(workdir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer func() { _ = os.Chdir(cwd) }()
	if err := s.Add("demo", rel); err != nil {
		t.Fatalf("Add: %v", err)
	}
	p, err := s.Get("demo")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	want, err := filepath.Abs(rel)
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}
	if p.ConfigPath != want {
		t.Fatalf("ConfigPath = %q, want %q", p.ConfigPath, want)
	}
	if !filepath.IsAbs(p.ConfigPath) {
		t.Fatalf("ConfigPath %q is not absolute", p.ConfigPath)
	}
}

func TestListSorted(t *testing.T) {
	s := newTestStore(t)
	for _, n := range []string{"zeta", "alpha", "mid"} {
		if err := s.Add(n, "/tmp/"+n+"/wrk3.yaml"); err != nil {
			t.Fatalf("Add %s: %v", n, err)
		}
	}
	got, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("List len = %d, want 3", len(got))
	}
	if got[0].Name != "alpha" || got[1].Name != "mid" || got[2].Name != "zeta" {
		t.Fatalf("List order = %v, want [alpha mid zeta]", got)
	}
}

func TestRemove(t *testing.T) {
	s := newTestStore(t)
	if err := s.Add("demo", "/tmp/x/wrk3.yaml"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := s.Remove("demo"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := s.Get("demo"); err == nil {
		t.Fatal("Get after Remove must fail")
	}
	if err := s.Remove("demo"); err == nil {
		t.Fatal("double Remove must fail")
	}
}

func TestSetCurrentAndCurrent(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Current(); err == nil {
		t.Fatal("Current with nothing set must fail")
	}
	if err := s.Add("a", "/tmp/a/wrk3.yaml"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := s.Add("b", "/tmp/b/wrk3.yaml"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := s.SetCurrent("a"); err != nil {
		t.Fatalf("SetCurrent: %v", err)
	}
	cur, err := s.Current()
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if cur.Name != "a" {
		t.Fatalf("Current = %q, want a", cur.Name)
	}
	if err := s.SetCurrent("missing"); err == nil {
		t.Fatal("SetCurrent missing must fail")
	}
	// Removing current clears it.
	if err := s.Remove("a"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := s.Current(); err == nil {
		t.Fatal("Current after removing current must fail")
	}
}

func TestXDGOverride(t *testing.T) {
	base := t.TempDir()
	t.Setenv(envOverride, base)
	got, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	want := filepath.Join(base, "wrk3", "projects.yaml")
	if got != want {
		t.Fatalf("DefaultPath = %q, want %q", got, want)
	}
	s, err := NewFileStore()
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	if s.Path() != want {
		t.Fatalf("Path = %q, want %q", s.Path(), want)
	}
	if err := s.Add("demo", "/tmp/x/wrk3.yaml"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("registry file not created at %q: %v", want, err)
	}
}

func TestXDGConfigHome(t *testing.T) {
	base := t.TempDir()
	t.Setenv(envOverride, "")
	t.Setenv(envConfigHome, base)
	got, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	want := filepath.Join(base, "wrk3", "projects.yaml")
	if got != want {
		t.Fatalf("DefaultPath = %q, want %q", got, want)
	}
}

func TestCwdIndependence(t *testing.T) {
	// Registry lives under an env-overridden dir; entries added from
	// one cwd resolve identically from another cwd.
	cfgHome := t.TempDir()
	t.Setenv(envOverride, cfgHome)
	s, err := NewFileStore()
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	dirA := t.TempDir()
	dirB := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(dirA); err != nil {
		t.Fatalf("Chdir A: %v", err)
	}
	// Absolute path stays as-is regardless of cwd.
	absTarget := filepath.Join(dirA, "wrk3.yaml")
	if err := s.Add("proj", absTarget); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := os.Chdir(dirB); err != nil {
		t.Fatalf("Chdir B: %v", err)
	}
	p, err := s.Get("proj")
	if err != nil {
		t.Fatalf("Get from different cwd: %v", err)
	}
	if p.ConfigPath != absTarget {
		t.Fatalf("ConfigPath = %q, want %q", p.ConfigPath, absTarget)
	}
	// A fresh store instance on the same path sees the same data.
	s2, err := NewFileStore()
	if err != nil {
		t.Fatalf("NewFileStore 2: %v", err)
	}
	p2, err := s2.Get("proj")
	if err != nil {
		t.Fatalf("Get via second store: %v", err)
	}
	if p2.ConfigPath != absTarget {
		t.Fatalf("second store ConfigPath = %q, want %q", p2.ConfigPath, absTarget)
	}
}

func TestResolveOrder(t *testing.T) {
	s := newTestStore(t)
	if err := s.Add("one", "/tmp/one/wrk3.yaml"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := s.Add("two", "/tmp/two/wrk3.yaml"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := s.SetCurrent("one"); err != nil {
		t.Fatalf("SetCurrent: %v", err)
	}
	// Explicit wins over env and current.
	t.Setenv(envProjectName, "two")
	p, err := s.Resolve("one")
	if err != nil {
		t.Fatalf("Resolve explicit: %v", err)
	}
	if p.Name != "one" {
		t.Fatalf("explicit resolve = %q, want one", p.Name)
	}
	// Env wins over current.
	p, err = s.Resolve("")
	if err != nil {
		t.Fatalf("Resolve env: %v", err)
	}
	if p.Name != "two" {
		t.Fatalf("env resolve = %q, want two", p.Name)
	}
	// Current when env unset (and no wrk3.yaml above temp cwd).
	t.Setenv(envProjectName, "")
	emptyDir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(emptyDir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	p, err = s.Resolve("")
	if err != nil {
		t.Fatalf("Resolve current: %v", err)
	}
	if p.Name != "one" {
		t.Fatalf("current resolve = %q, want one", p.Name)
	}
}

func TestResolveCwdScan(t *testing.T) {
	s := newTestStore(t)
	t.Setenv(envProjectName, "")
	root := t.TempDir()
	cfg := filepath.Join(root, ConfigFileName)
	if err := os.WriteFile(cfg, []byte("project: {}\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(sub); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	p, err := s.Resolve("")
	if err != nil {
		t.Fatalf("Resolve cwd scan: %v", err)
	}
	wantCfg, err := filepath.Abs(cfg)
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}
	// Normalize symlinks (/var vs /private/var on darwin).
	if eval, err := filepath.EvalSymlinks(wantCfg); err == nil {
		wantCfg = eval
	}
	gotCfg := p.ConfigPath
	if eval, err := filepath.EvalSymlinks(gotCfg); err == nil {
		gotCfg = eval
	}
	if gotCfg != wantCfg {
		t.Fatalf("cwd resolve = %q, want %q", p.ConfigPath, cfg)
	}
}

func TestFindConfigUpwardsMissing(t *testing.T) {
	got, err := FindConfigUpwards(t.TempDir())
	if err != nil {
		t.Fatalf("FindConfigUpwards: %v", err)
	}
	if got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestPersistenceRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "projects.yaml")
	s, err := NewFileStore(WithPath(path))
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	wantCfg := filepath.Join(t.TempDir(), "x", "wrk3.yaml")
	if err := s.Add("demo", wantCfg); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := s.SetCurrent("demo"); err != nil {
		t.Fatalf("SetCurrent: %v", err)
	}
	s2, err := NewFileStore(WithPath(path))
	if err != nil {
		t.Fatalf("NewFileStore 2: %v", err)
	}
	cur, err := s2.Current()
	if err != nil {
		t.Fatalf("Current after reopen: %v", err)
	}
	if cur.Name != "demo" || cur.ConfigPath != filepath.Clean(wantCfg) {
		t.Fatalf("round trip = %+v, want config %q", cur, filepath.Clean(wantCfg))
	}
}
