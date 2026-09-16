package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mytmlt/wrk3/internal/config"
	"github.com/mytmlt/wrk3/internal/ports"
	"github.com/mytmlt/wrk3/internal/source"
)

type mainStubSource struct {
	stubSource
	infos   []source.WorktreeInfo
	listErr error
}

func (s *mainStubSource) List(repoPath string) ([]source.WorktreeInfo, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.infos, nil
}

func writeTestConfig(t *testing.T, repoRoot string) *config.Config {
	t.Helper()
	content := `project:
  worktreeBase: .worktrees
source:
  type: git
  git: {remote: origin, fetchPrune: true}
runner:
  type: docker
  docker:
    composeFiles: [docker-compose.yml]
    projectPrefix: demo
entry:
  setup: ["echo setup"]
  run: "echo run"
  stop: "echo stop"
  logs: "echo logs"
ports:
  base: {app: 8000}
  step: 100
`
	path := filepath.Join(repoRoot, "wrk3.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func initMainTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command("git", "init", "-b", "main")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	cmd = exec.Command("git", "config", "user.email", "t@t")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git config email: %v: %s", err, out)
	}
	cmd = exec.Command("git", "config", "user.name", "t")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git config name: %v: %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-m", "init"}} {
		cmd = exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	return dir
}

func TestMainRecord_RealGit(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	r := &resolved{cfg: cfg, src: &source.GitSource{}}
	rec, err := mainRecord(r, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rec == nil {
		t.Fatal("expected main record, got nil")
	}
	if rec.Branch != "main" {
		t.Errorf("Branch = %q, want main", rec.Branch)
	}
	if rec.Slug != "main" {
		t.Errorf("Slug = %q, want main", rec.Slug)
	}
	if rec.AbsPath != filepath.Clean(repo) {
		t.Errorf("AbsPath = %q, want %q", rec.AbsPath, filepath.Clean(repo))
	}
	if rec.Index != mainWorktreeIndex {
		t.Errorf("Index = %d, want %d", rec.Index, mainWorktreeIndex)
	}
	if rec.Ports["app"] != 8000 {
		t.Errorf("app port = %d, want 8000", rec.Ports["app"])
	}
	if rec.ComposeProject != "demo-main" {
		t.Errorf("ComposeProject = %q, want demo-main", rec.ComposeProject)
	}
}

func TestMainRecord_CustomBaseEqualsBase(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	cfg.Ports.Base = map[string]int{"app": 9000, "web": 3000}
	r := &resolved{cfg: cfg, src: &source.GitSource{}}
	rec, err := mainRecord(r, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rec == nil {
		t.Fatal("expected main record, got nil")
	}
	if rec.Index != mainWorktreeIndex {
		t.Errorf("Index = %d, want %d", rec.Index, mainWorktreeIndex)
	}
	if rec.Ports["app"] != 9000 || rec.Ports["web"] != 3000 {
		t.Errorf("Ports = %v, want exactly the configured base {app:9000 web:3000}", rec.Ports)
	}
}

func TestMainRecord_SkipsWhenRegistered(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	r := &resolved{cfg: cfg, src: &source.GitSource{}}
	recs := []ports.WorktreeRecord{{Branch: "main", Slug: "main", AbsPath: "/other", Index: 0}}
	rec, err := mainRecord(r, recs)
	if err != nil {
		t.Fatal(err)
	}
	if rec != nil {
		t.Errorf("expected nil when branch registered, got %+v", rec)
	}
}

func TestMainRecord_ListErrorSkips(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	r := &resolved{cfg: cfg, src: &mainStubSource{listErr: os.ErrClosed}}
	rec, err := mainRecord(r, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rec != nil {
		t.Errorf("expected nil on list error, got %+v", rec)
	}
}

func TestMainRecord_PortCollision(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	r := &resolved{cfg: cfg, src: &source.GitSource{}}
	// Reserved index collision: state must never hold mainWorktreeIndex.
	// (Pre-fix state files may still carry a managed index 0 allocation;
	// those now collide with main, which owns the base ports.)
	recs := []ports.WorktreeRecord{{
		Branch: "feature", Slug: "feature", AbsPath: "/other",
		Index: mainWorktreeIndex, Ports: map[string]int{"app": 8000},
	}}
	if _, err := mainRecord(r, recs); err == nil {
		t.Fatal("expected port collision error")
	}
}

func TestMainRecord_PortValueCollision(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	// Multi-port base where a managed allocation shares a port value
	// with main's allocation under a different name: main holds
	// {app:8000, web:7900} and index 1 holds {app:8100, web:8000},
	// so managed web == main app == 8000.
	cfg.Ports.Base = map[string]int{"app": 8000, "web": 7900}
	r := &resolved{cfg: cfg, src: &source.GitSource{}}
	recs := []ports.WorktreeRecord{{
		Branch: "feature", Slug: "feature", AbsPath: "/other",
		Index: 1, Ports: map[string]int{"app": 8100, "web": 8000},
	}}
	if _, err := mainRecord(r, recs); err == nil {
		t.Fatal("expected port value collision error")
	}
}

func TestResolveTargetsWithMain_BareIncludesMain(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	r := &resolved{cfg: cfg, src: &source.GitSource{}}
	recs := []ports.WorktreeRecord{{
		Branch: "feature", Slug: "feature", AbsPath: filepath.Join(repo, ".worktrees", "feature"),
		Index: 1, Ports: map[string]int{"app": 8100},
	}}
	targets, err := resolveTargetsWithMain(r, recs, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 2 {
		t.Fatalf("got %d targets, want 2", len(targets))
	}
	// Sorted by branch: feature before main.
	if targets[0].Branch != "feature" || targets[1].Branch != "main" {
		t.Errorf("unexpected order: %v", targets)
	}
}

func TestResolveTargetsWithMain_EmptyStateRunsMain(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	r := &resolved{cfg: cfg, src: &source.GitSource{}}
	targets, err := resolveTargetsWithMain(r, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].Branch != "main" {
		t.Errorf("got %+v, want [main]", targets)
	}
}

func TestResolveTargetsWithMain_ExplicitMain(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	r := &resolved{cfg: cfg, src: &source.GitSource{}}
	targets, err := resolveTargetsWithMain(r, nil, []string{"main"})
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].AbsPath != filepath.Clean(repo) {
		t.Errorf("got %+v, want main at repo root", targets)
	}
}

func TestEnsureWorktreeEnv_WritesDotEnv(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	r := &resolved{cfg: cfg, src: &source.GitSource{}}
	rec, err := mainRecord(r, nil)
	if err != nil {
		t.Fatal(err)
	}
	if warns, err := ensureWorktreeEnv(r, *rec); err != nil {
		t.Fatal(err)
	} else if len(warns) != 0 {
		t.Errorf("warns = %v, want none for fresh .env", warns)
	}
	content, err := os.ReadFile(filepath.Join(repo, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if len(content) == 0 {
		t.Error("empty .env")
	}
}

func TestEnsureWorktreeEnv_PreservesSecrets(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	r := &resolved{cfg: cfg, src: &source.GitSource{}}
	rec, err := mainRecord(r, nil)
	if err != nil {
		t.Fatal(err)
	}
	secret := "SECRET=topsecret\n"
	if err := os.WriteFile(filepath.Join(repo, ".env"), []byte(secret), 0o644); err != nil {
		t.Fatal(err)
	}
	if warns, err := ensureWorktreeEnv(r, *rec); err != nil {
		t.Fatal(err)
	} else if len(warns) != 0 {
		t.Errorf("warns = %v, want none", warns)
	}
	content, err := os.ReadFile(filepath.Join(repo, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(content)
	if !strings.Contains(s, secret) {
		t.Errorf("secret lost from main .env.\n%s", s)
	}
	if !strings.Contains(s, "APP_PORT=8000\n") {
		t.Errorf("managed APP_PORT missing from main .env.\n%s", s)
	}
}

func TestEnsureWorktreeEnv_ManagedWorktreeGapFilled(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	r := &resolved{cfg: cfg, src: &source.GitSource{}}
	wt := t.TempDir()
	// Manually created worktree .env with secrets but no port vars:
	// ensure must insert the managed section without touching the rest.
	if err := os.WriteFile(filepath.Join(wt, ".env"), []byte("SECRET=topsecret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := ports.WorktreeRecord{
		Branch:  "feature-foo",
		Slug:    "feature-foo",
		AbsPath: wt,
		Index:   0,
		Ports:   map[string]int{"app": 8000},
	}
	if warns, err := ensureWorktreeEnv(r, rec); err != nil {
		t.Fatal(err)
	} else if len(warns) != 0 {
		t.Errorf("warns = %v, want none", warns)
	}
	content, err := os.ReadFile(filepath.Join(wt, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(content)
	if !strings.Contains(s, "SECRET=topsecret\n") {
		t.Errorf("secret lost.\n%s", s)
	}
	if !strings.Contains(s, "APP_PORT=8000\n") {
		t.Errorf("managed APP_PORT not inserted.\n%s", s)
	}
}

func TestEnsureWorktreeEnv_DivergenceWarnsNeverOverwrites(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	r := &resolved{cfg: cfg, src: &source.GitSource{}}
	wt := t.TempDir()
	if err := os.WriteFile(filepath.Join(wt, ".env"), []byte("APP_PORT=5000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := ports.WorktreeRecord{
		Branch:  "feature-foo",
		Slug:    "feature-foo",
		AbsPath: wt,
		Index:   0,
		Ports:   map[string]int{"app": 8000},
	}
	warns, err := ensureWorktreeEnv(r, rec)
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) == 0 {
		t.Fatalf("warns empty, want divergence warning for APP_PORT")
	}
	found := false
	for _, w := range warns {
		if strings.Contains(w, "APP_PORT") && strings.Contains(w, "5000") && strings.Contains(w, "8000") {
			found = true
		}
	}
	if !found {
		t.Errorf("warns missing APP_PORT divergence: %v", warns)
	}
	content, err := os.ReadFile(filepath.Join(wt, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "APP_PORT=5000\n") {
		t.Errorf("existing APP_PORT was overwritten.\n%s", content)
	}
}
