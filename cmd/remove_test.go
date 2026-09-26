package cmd

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mytmlt/wrk3/internal/telemetry"
)

// dirtyWorktreeRemoveError sets up a temp repo with a worktree that has an
// untracked file and returns the error from `wrk3 remove` on it without
// --force. The git helper runs inside the repo (cwd-independent assertions).
func dirtyWorktreeRemoveError(t *testing.T) error {
	t.Helper()
	repo := initMainTestRepo(t)
	_ = writeTestConfig(t, repo)

	wtDir := filepath.Join(repo, ".worktrees", "feature-foo")
	if err := os.MkdirAll(wtDir, 0o755); err != nil {
		t.Fatal(err)
	}

	git := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}

	git("branch", "feature/foo")
	git("worktree", "add", wtDir, "feature/foo")

	if err := os.WriteFile(filepath.Join(wtDir, "dirty.txt"), []byte("uncommitted"), 0o644); err != nil {
		t.Fatal(err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	oldFile := fileFlag
	fileFlag = ""
	oldForce := removeForce
	t.Cleanup(func() {
		_ = os.Chdir(cwd)
		fileFlag = oldFile
		removeForce = oldForce
	})

	_, _, err = executeCmd("remove", "feature/foo")
	return err
}

func TestRemove_HintsForceOnDirtyWorktree(t *testing.T) {
	err := dirtyWorktreeRemoveError(t)
	if err == nil {
		t.Fatal("expected error for dirty worktree remove, got nil")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, "hint:") {
		t.Errorf("error should contain hint, got: %q", errMsg)
	}
	if !strings.Contains(errMsg, "--force") {
		t.Errorf("error should mention --force, got: %q", errMsg)
	}
}

func TestRemove_DirtyWorktreeIsNonReportable(t *testing.T) {
	err := dirtyWorktreeRemoveError(t)
	if err == nil {
		t.Fatal("expected error for dirty worktree remove, got nil")
	}
	var nr telemetry.NonReportable
	if !errors.As(err, &nr) {
		t.Fatalf("dirty-worktree error should implement telemetry.NonReportable, got %T", err)
	}
	if !nr.NonReportable() {
		t.Error("NonReportable() = false, want true")
	}
}
