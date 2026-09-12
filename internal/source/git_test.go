package source

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v: %s", args, dir, err, out)
	}
	return string(out)
}

// initRepo creates a temp git repo with one commit on main. No network.
func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "t@t")
	runGit(t, dir, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "init")
	return dir
}

func TestAddListRemoveCycle(t *testing.T) {
	repo := initRepo(t)
	src := &GitSource{}
	runGit(t, repo, "branch", "feature/foo")

	wt := filepath.Join(t.TempDir(), "wt-foo")
	if err := src.Add(repo, "feature/foo", wt, "origin"); err != nil {
		t.Fatalf("Add: %v", err)
	}

	infos, err := src.List(repo)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	// macOS: t.TempDir() is a /var -> /private/var symlink; git reports
	// the resolved path, so compare resolved forms. Windows: git
	// reports forward slashes (C:/...) while filepath uses backslashes,
	// and short (8.3) vs long names may differ — normalize both sides.
	wantWt, err := filepath.EvalSymlinks(wt)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	wantWt = normWorktreePath(wantWt)
	found := false
	for _, in := range infos {
		if normWorktreePath(in.Path) == wantWt {
			found = true
			if in.Branch != "feature/foo" {
				t.Errorf("Branch = %q, want feature/foo", in.Branch)
			}
			if in.Commit == "" {
				t.Error("Commit empty")
			}
		}
	}
	if !found {
		t.Fatalf("List missing %s: %+v", wt, infos)
	}

	if err := src.Remove(repo, wt, false); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	infos, err = src.List(repo)
	if err != nil {
		t.Fatalf("List after remove: %v", err)
	}
	for _, in := range infos {
		if normWorktreePath(in.Path) == wantWt {
			t.Fatalf("worktree still listed after remove: %+v", infos)
		}
	}
}

// normWorktreePath normalizes a `git worktree list` path for comparison:
// resolves symlinks when possible, cleans separators, and folds case on
// Windows (short vs long names, slash style).
func normWorktreePath(p string) string {
	if ep, err := filepath.EvalSymlinks(p); err == nil {
		p = ep
	}
	p = filepath.Clean(p)
	if runtime.GOOS == "windows" {
		p = strings.ToLower(p)
	}
	return p
}

func TestAdd_InvalidBranchErrors(t *testing.T) {
	repo := initRepo(t)
	src := &GitSource{}
	wt := filepath.Join(t.TempDir(), "wt-bad")
	err := src.Add(repo, "does-not-exist-xyz", wt, "origin")
	if err == nil {
		t.Fatal("expected error for unknown branch")
	}
	if !strings.Contains(err.Error(), "worktree") {
		t.Errorf("error should mention worktree: %v", err)
	}
	if err := src.Add(repo, "", wt, "origin"); err == nil {
		t.Error("expected error for empty branch")
	}
	if err := src.Remove(repo, "", false); err == nil {
		t.Error("expected error for empty worktree path")
	}
}

func TestAdd_TracksRemoteOnlyBranch(t *testing.T) {
	origin := t.TempDir()
	runGit(t, origin, "init", "--bare")
	repo := initRepo(t)
	runGit(t, repo, "remote", "add", "origin", origin)
	runGit(t, repo, "push", "origin", "main")
	runGit(t, repo, "branch", "remote-only")
	runGit(t, repo, "push", "origin", "remote-only")
	runGit(t, repo, "checkout", "-q", "main")
	runGit(t, repo, "branch", "-D", "remote-only")

	src := &GitSource{}
	if err := src.Fetch(repo, "origin"); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	wt := filepath.Join(t.TempDir(), "wt-tracked")
	if err := src.Add(repo, "remote-only", wt, "origin"); err != nil {
		t.Fatalf("Add remote-only: %v", err)
	}
	// Tracking branch now exists locally.
	cmd := exec.Command("git", "-C", repo, "show-ref", "--verify", "--quiet", "refs/heads/remote-only")
	if err := cmd.Run(); err != nil {
		t.Fatalf("expected local tracking branch refs/heads/remote-only: %v", err)
	}
	infos, err := src.List(repo)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := false
	for _, in := range infos {
		if in.Branch == "remote-only" {
			found = true
		}
	}
	if !found {
		t.Fatalf("worktree for remote-only not listed: %+v", infos)
	}
}

func TestRemove_ForceDirtyWorktree(t *testing.T) {
	repo := initRepo(t)
	src := &GitSource{}
	runGit(t, repo, "branch", "dirty")
	wt := filepath.Join(t.TempDir(), "wt-dirty")
	if err := src.Add(repo, "dirty", wt, "origin"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	// Make the worktree dirty with an untracked file + a modification.
	if err := os.WriteFile(filepath.Join(wt, "new.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := src.Remove(repo, wt, true); err != nil {
		t.Fatalf("force Remove of dirty worktree: %v", err)
	}
}

func TestRefs_NoRemotesEmpty(t *testing.T) {
	repo := initRepo(t)
	src := &GitSource{}
	refs, err := src.Refs(repo, "origin")
	if err != nil {
		t.Fatalf("Refs: %v", err)
	}
	if len(refs) != 0 {
		t.Fatalf("expected no refs without remotes, got %v", refs)
	}
}

func TestFetchAndRefs_LocalBareOrigin(t *testing.T) {
	// Offline: use a local bare repo as origin, never touch the network.
	origin := t.TempDir()
	runGit(t, origin, "init", "--bare")
	repo := initRepo(t)
	runGit(t, repo, "remote", "add", "origin", origin)
	runGit(t, repo, "push", "origin", "main")
	runGit(t, repo, "branch", "feature/bar")
	runGit(t, repo, "push", "origin", "feature/bar")

	src := &GitSource{}
	if err := src.Fetch(repo, "origin"); err != nil {
		t.Fatalf("Fetch against local bare origin: %v", err)
	}
	refs, err := src.Refs(repo, "origin")
	if err != nil {
		t.Fatalf("Refs: %v", err)
	}
	has := map[string]bool{}
	for _, r := range refs {
		has[r] = true
		if strings.Contains(r, "->") || strings.HasPrefix(r, "origin/") {
			t.Errorf("ref not normalized: %q", r)
		}
	}
	for _, want := range []string{"main", "feature/bar"} {
		if !has[want] {
			t.Errorf("Refs = %v, missing %q", refs, want)
		}
	}
}

func TestList_InvalidRepoErrors(t *testing.T) {
	src := &GitSource{}
	if _, err := src.List(t.TempDir()); err == nil {
		t.Fatal("expected error listing non-repo dir")
	}
	if err := src.Fetch(t.TempDir(), "origin"); err == nil {
		t.Fatal("expected error fetching non-repo dir")
	}
}

func TestParseWorktreePorcelain(t *testing.T) {
	out := "worktree /r\nHEAD abc123\nbranch refs/heads/main\n\n" +
		"worktree /r-wt\nHEAD def456\nbranch refs/heads/feature/foo\n\n" +
		"worktree /r-det\nHEAD abc123\ndetached\n\n"
	infos := parseWorktreePorcelain(out)
	if len(infos) != 3 {
		t.Fatalf("got %d infos: %+v", len(infos), infos)
	}
	if infos[0].Branch != "main" || infos[0].Commit != "abc123" {
		t.Errorf("first entry wrong: %+v", infos[0])
	}
	if infos[1].Branch != "feature/foo" {
		t.Errorf("second entry wrong: %+v", infos[1])
	}
	if infos[2].Branch != "" {
		t.Errorf("detached should have empty branch: %+v", infos[2])
	}
}
