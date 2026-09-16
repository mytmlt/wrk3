package source

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// cloneRepo clones origin into a fresh temp dir (branch main, tracking
// origin/main). No network: origin is a local bare repo.
func cloneRepo(t *testing.T, origin string) string {
	t.Helper()
	parent := t.TempDir()
	dst := filepath.Join(parent, "clone")
	cmd := exec.Command("git", "clone", "-b", "main", origin, dst)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git clone %s: %v: %s", origin, err, out)
	}
	runGit(t, dst, "config", "user.email", "t@t")
	runGit(t, dst, "config", "user.name", "t")
	return dst
}

func TestPull_FastForwardsTrackingBranch(t *testing.T) {
	origin := t.TempDir()
	runGit(t, origin, "init", "--bare")
	seed := initRepo(t)
	runGit(t, seed, "remote", "add", "origin", origin)
	runGit(t, seed, "push", "origin", "main")

	wt := cloneRepo(t, origin)

	// Advance origin/main from the seed side.
	if err := os.WriteFile(filepath.Join(seed, "new.txt"), []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, seed, "add", ".")
	runGit(t, seed, "commit", "-m", "second")
	runGit(t, seed, "push", "origin", "main")

	src := &GitSource{}
	if err := src.Pull(wt, PullOptions{}); err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wt, "new.txt")); err != nil {
		t.Errorf("new.txt missing after pull: %v", err)
	}
}

func TestPull_FFOnlySucceedsOnFastForward(t *testing.T) {
	origin := t.TempDir()
	runGit(t, origin, "init", "--bare")
	seed := initRepo(t)
	runGit(t, seed, "remote", "add", "origin", origin)
	runGit(t, seed, "push", "origin", "main")

	wt := cloneRepo(t, origin)

	if err := os.WriteFile(filepath.Join(seed, "ff.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, seed, "add", ".")
	runGit(t, seed, "commit", "-m", "ff")
	runGit(t, seed, "push", "origin", "main")

	src := &GitSource{}
	if err := src.Pull(wt, PullOptions{FFOnly: true}); err != nil {
		t.Fatalf("Pull --ff-only: %v", err)
	}
}

func TestPull_RebaseSucceedsOnFastForward(t *testing.T) {
	origin := t.TempDir()
	runGit(t, origin, "init", "--bare")
	seed := initRepo(t)
	runGit(t, seed, "remote", "add", "origin", origin)
	runGit(t, seed, "push", "origin", "main")

	wt := cloneRepo(t, origin)

	if err := os.WriteFile(filepath.Join(seed, "rb.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, seed, "add", ".")
	runGit(t, seed, "commit", "-m", "rb")
	runGit(t, seed, "push", "origin", "main")

	src := &GitSource{}
	if err := src.Pull(wt, PullOptions{Rebase: true}); err != nil {
		t.Fatalf("Pull --rebase: %v", err)
	}
}

func TestPull_EmptyPathErrors(t *testing.T) {
	src := &GitSource{}
	if err := src.Pull("", PullOptions{}); err == nil {
		t.Error("expected error for empty worktree path")
	}
	if err := src.Pull("   ", PullOptions{}); err == nil {
		t.Error("expected error for blank worktree path")
	}
}

func TestPull_RebaseAndFFOnlyExclusive(t *testing.T) {
	src := &GitSource{}
	err := src.Pull(t.TempDir(), PullOptions{Rebase: true, FFOnly: true})
	if err == nil {
		t.Fatal("expected error for --rebase + --ff-only")
	}
}
