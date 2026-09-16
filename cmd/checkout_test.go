package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mytmlt/wrk3/internal/ports"
)

// checkoutTestRepo builds a temp git repo with a config, one managed
// worktree directory on disk, and matching state. It chdirs into the repo
// (so config discovery works) and returns the repo root, the worktree dir,
// and a cleanup restoring cwd, env, and globals.
func checkoutTestRepo(t *testing.T) (repo, wtDir string) {
	t.Helper()
	repo = initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	wtDir = filepath.Join(repo, ".worktrees", "feature-foo")
	if err := os.MkdirAll(wtDir, 0o755); err != nil {
		t.Fatal(err)
	}
	recs := []ports.WorktreeRecord{{
		Branch:         "feature/foo",
		Slug:           "feature-foo",
		AbsPath:        wtDir,
		Index:          1,
		Ports:          map[string]int{"app": 8100},
		Status:         ports.StatusStopped,
		ComposeProject: "demo-feature-foo",
	}}
	if err := ports.Save(cfg.StatePath(), recs); err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	oldFile, oldPrint := fileFlag, checkoutPrint
	fileFlag, checkoutPrint = "", false
	t.Setenv(directiveCDFileEnv, "")
	t.Cleanup(func() {
		_ = os.Chdir(cwd)
		fileFlag, checkoutPrint = oldFile, oldPrint
	})
	return repo, wtDir
}

func TestCheckout_PrintsPathBySlug(t *testing.T) {
	_, wtDir := checkoutTestRepo(t)
	out, _, err := executeCmd("checkout", "feature-foo")
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}
	if strings.TrimSpace(out) != wtDir {
		t.Errorf("checkout output = %q, want %q", out, wtDir)
	}
}

func TestCheckout_AcceptsBranchName(t *testing.T) {
	_, wtDir := checkoutTestRepo(t)
	out, _, err := executeCmd("checkout", "feature/foo")
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}
	if strings.TrimSpace(out) != wtDir {
		t.Errorf("checkout output = %q, want %q", out, wtDir)
	}
}

func TestCheckout_ResolvesMainCheckout(t *testing.T) {
	repo, _ := checkoutTestRepo(t)
	out, _, err := executeCmd("checkout", "main")
	if err != nil {
		t.Fatalf("checkout main: %v", err)
	}
	got := strings.TrimSpace(out)
	// TempDir on macOS goes through the /var -> /private/var symlink;
	// compare resolved paths (same tolerance as sameRepoRoot).
	resolve := func(p string) string {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return filepath.Clean(r)
		}
		return filepath.Clean(p)
	}
	if resolve(got) != resolve(repo) {
		t.Errorf("checkout main = %q, want repo root %q", got, repo)
	}
}

func TestCheckout_UnknownWorktreeErrors(t *testing.T) {
	checkoutTestRepo(t)
	if _, _, err := executeCmd("checkout", "nope"); err == nil {
		t.Error("checkout unknown = nil, want error")
	}
}

func TestCheckout_StaleDirErrors(t *testing.T) {
	repo, _ := checkoutTestRepo(t)
	stateP := filepath.Join(repo, ".worktrees", ports.StateFileName)
	// Append a record pointing at a directory that does not exist.
	stored, err := ports.Load(stateP)
	if err != nil {
		t.Fatal(err)
	}
	stored = append(stored, ports.WorktreeRecord{
		Branch:         "gone",
		Slug:           "gone",
		AbsPath:        filepath.Join(repo, ".worktrees", "gone"),
		Index:          5,
		Ports:          map[string]int{"app": 8500},
		Status:         ports.StatusStopped,
		ComposeProject: "demo-gone",
	})
	if err := ports.Save(stateP, stored); err != nil {
		t.Fatal(err)
	}
	_, _, err = executeCmd("checkout", "gone")
	if err == nil {
		t.Fatal("checkout stale = nil, want error")
	}
	if !strings.Contains(err.Error(), "stale") {
		t.Errorf("checkout stale error = %q, want it to mention stale", err)
	}
}

func TestCheckout_WritesDirectiveFileWhenWrapperActive(t *testing.T) {
	_, wtDir := checkoutTestRepo(t)
	cdFile := filepath.Join(t.TempDir(), "cd-directive")
	t.Setenv(directiveCDFileEnv, cdFile)
	out, _, err := executeCmd("checkout", "feature-foo")
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}
	if out != "" {
		t.Errorf("checkout stdout = %q, want empty when wrapper is active", out)
	}
	raw, err := os.ReadFile(cdFile)
	if err != nil {
		t.Fatalf("read directive file: %v", err)
	}
	// Raw path: exact bytes, no shell quoting, no trailing newline.
	if string(raw) != wtDir {
		t.Errorf("directive file = %q, want %q", raw, wtDir)
	}
}

func TestCheckout_PrintFlagSkipsDirectiveFile(t *testing.T) {
	_, wtDir := checkoutTestRepo(t)
	cdFile := filepath.Join(t.TempDir(), "cd-directive")
	t.Setenv(directiveCDFileEnv, cdFile)
	out, _, err := executeCmd("checkout", "--print", "feature-foo")
	if err != nil {
		t.Fatalf("checkout --print: %v", err)
	}
	if strings.TrimSpace(out) != wtDir {
		t.Errorf("checkout --print output = %q, want %q", out, wtDir)
	}
	if _, err := os.Stat(cdFile); !os.IsNotExist(err) {
		t.Errorf("directive file should not be written with --print (stat err = %v)", err)
	}
}

func TestCheckout_AliasesResolve(t *testing.T) {
	_, wtDir := checkoutTestRepo(t)
	for _, alias := range []string{"co", "switch"} {
		out, _, err := executeCmd(alias, "feature-foo")
		if err != nil {
			t.Fatalf("%s: %v", alias, err)
		}
		if strings.TrimSpace(out) != wtDir {
			t.Errorf("%s output = %q, want %q", alias, out, wtDir)
		}
	}
}
