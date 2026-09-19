//go:build e2e

package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Happy-path e2e tests for issue #16 (PR1: H01-H06). Each test is hermetic:
// it builds its own throwaway repo via the harness and never touches real
// checkouts. Dir is passed explicitly via runWrk3 (no t.Chdir). These tests
// intentionally never skip on testing.Short: they run with or without extra
// setup (the harness commits a minimal alpine compose file so up/down work
// against a real daemon; entry commands are echo-only).

// happyStateRecord mirrors ports.WorktreeRecord for state-file assertions.
type happyStateRecord struct {
	Branch         string         `json:"branch"`
	Slug           string         `json:"slug"`
	AbsPath        string         `json:"absPath"`
	Index          int            `json:"index"`
	Ports          map[string]int `json:"ports"`
	ComposeProject string         `json:"composeProject"`
	Status         string         `json:"status"`
}

// happyReadState parses .worktrees/.wrk3-state.json for repoDir.
func happyReadState(t *testing.T, repoDir string) []happyStateRecord {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoDir, ".worktrees", ".wrk3-state.json"))
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	var recs []happyStateRecord
	if err := json.Unmarshal(raw, &recs); err != nil {
		t.Fatalf("parse state file: %v", err)
	}
	return recs
}

// happyAddFoo adds feature/foo --no-create, failing the test on error.
func happyAddFoo(t *testing.T, repoDir, cfg string) {
	t.Helper()
	if out, errOut, err := runWrk3(t, repoDir, cfg, "add", "feature/foo", "--no-create"); err != nil {
		t.Fatalf("add feature/foo --no-create: %v\nstdout: %s\nstderr: %s", err, out, errOut)
	}
}

// TestE2E_FetchListsBranch (H01): fetch lists the pushed feature/foo branch.
func TestE2E_FetchListsBranch(t *testing.T) {
	repoDir := mkThrowawayRepo(t)
	cfg := writeE2EConfig(t, repoDir)

	stdout, errOut, err := runWrk3(t, repoDir, cfg, "fetch")
	if err != nil {
		t.Fatalf("fetch: %v\nstdout: %s\nstderr: %s", err, stdout, errOut)
	}
	mustContain(t, stdout, "feature/foo")
}

// TestE2E_AddExistingBranch (H02): add registers the worktree dir, state
// entry (absolute path + app 8001), and .env APP_PORT.
func TestE2E_AddExistingBranch(t *testing.T) {
	repoDir := mkThrowawayRepo(t)
	cfg := writeE2EConfig(t, repoDir)

	happyAddFoo(t, repoDir, cfg)

	wt := filepath.Join(repoDir, ".worktrees", "feature-foo")
	if st, err := os.Stat(wt); err != nil || !st.IsDir() {
		t.Fatalf("worktree dir %q missing: %v", wt, err)
	}

	raw, err := os.ReadFile(filepath.Join(repoDir, ".worktrees", ".wrk3-state.json"))
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	mustContain(t, string(raw), wt)
	mustContain(t, string(raw), "8001")

	recs := happyReadState(t, repoDir)
	found := false
	for _, r := range recs {
		if r.Branch == "feature/foo" && r.Ports["app"] == 8001 {
			found = true
		}
	}
	if !found {
		t.Fatalf("state has no feature/foo record with app=8001: %s", string(raw))
	}

	env, err := os.ReadFile(filepath.Join(wt, ".env"))
	if err != nil {
		t.Fatalf("read worktree .env: %v", err)
	}
	mustContain(t, string(env), "APP_PORT=8001")
}

// TestE2E_StatusShowsWorktree (H03): status lists the added worktree with
// its allocated port.
func TestE2E_StatusShowsWorktree(t *testing.T) {
	repoDir := mkThrowawayRepo(t)
	cfg := writeE2EConfig(t, repoDir)

	happyAddFoo(t, repoDir, cfg)

	stdout, errOut, err := runWrk3(t, repoDir, cfg, "status")
	if err != nil {
		t.Fatalf("status: %v\nstdout: %s\nstderr: %s", err, stdout, errOut)
	}
	mustContain(t, stdout, "feature/foo")
	mustContain(t, stdout, "app=8001")
}

// TestE2E_UpLogsDown (H04): up, logs, and down each exit 0 with echo entries.
// Skips without a daemon (compose up needs one even for the echo-entry
// alpine stack); up/down get a 3m timeout for cold image pulls.
func TestE2E_UpLogsDown(t *testing.T) {
	if !haveDockerDaemon() {
		t.Skip("skip up/logs/down e2e without a docker daemon")
	}
	repoDir := mkThrowawayRepo(t)
	cfg := writeE2EConfig(t, repoDir)

	happyAddFoo(t, repoDir, cfg)

	t.Cleanup(func() {
		_, _, _ = runWrk3Timeout(t, repoDir, cfg, 3*time.Minute, "down", "feature/foo")
	})

	if out, errOut, err := runWrk3Timeout(t, repoDir, cfg, 3*time.Minute, "up", "feature/foo"); err != nil {
		t.Fatalf("up feature/foo: %v\nstdout: %s\nstderr: %s", err, out, errOut)
	}
	if out, errOut, err := runWrk3(t, repoDir, cfg, "logs", "feature/foo"); err != nil {
		t.Fatalf("logs feature/foo: %v\nstdout: %s\nstderr: %s", err, out, errOut)
	}
	if out, errOut, err := runWrk3Timeout(t, repoDir, cfg, 3*time.Minute, "down", "feature/foo"); err != nil {
		t.Fatalf("down feature/foo: %v\nstdout: %s\nstderr: %s", err, out, errOut)
	}
}

// TestE2E_RemoveCleansUp (H05): remove --force deletes the dir and status
// still exits 0.
func TestE2E_RemoveCleansUp(t *testing.T) {
	repoDir := mkThrowawayRepo(t)
	cfg := writeE2EConfig(t, repoDir)

	happyAddFoo(t, repoDir, cfg)

	if out, errOut, err := runWrk3(t, repoDir, cfg, "remove", "feature/foo", "--force"); err != nil {
		t.Fatalf("remove --force: %v\nstdout: %s\nstderr: %s", err, out, errOut)
	}
	if _, err := os.Stat(filepath.Join(repoDir, ".worktrees", "feature-foo")); !os.IsNotExist(err) {
		t.Fatalf("worktree dir still exists after remove --force")
	}
	if out, errOut, err := runWrk3(t, repoDir, cfg, "status"); err != nil {
		t.Fatalf("status after remove: %v\nstdout: %s\nstderr: %s", err, out, errOut)
	}
}

// TestE2E_UpAfterRemoveErrors (H06): up after remove fails with a useful
// unknown-worktree error.
func TestE2E_UpAfterRemoveErrors(t *testing.T) {
	repoDir := mkThrowawayRepo(t)
	cfg := writeE2EConfig(t, repoDir)

	happyAddFoo(t, repoDir, cfg)

	if out, errOut, err := runWrk3(t, repoDir, cfg, "remove", "feature/foo", "--force"); err != nil {
		t.Fatalf("remove --force: %v\nstdout: %s\nstderr: %s", err, out, errOut)
	}
	stdout, stderr, err := runWrk3(t, repoDir, cfg, "up", "feature/foo")
	if err == nil {
		t.Fatalf("up after remove succeeded, want unknown-worktree error (out=%q)", stdout)
	}
	mustContain(t, stdout+"\n"+stderr, "unknown worktree")
}
