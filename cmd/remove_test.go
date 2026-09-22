package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemove_HintsForceOnDirtyWorktree(t *testing.T) {
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
	defer func() {
		_ = os.Chdir(cwd)
		fileFlag = oldFile
	}()

	// save/restore removeForce global
	oldForce := removeForce
	defer func() { removeForce = oldForce }()

	_, _, err = executeCmd("remove", "feature/foo")
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