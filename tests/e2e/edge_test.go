//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Edge-case e2e tests for issue #16 (PR2). Each test is hermetic: it builds
// its own throwaway repo via the harness (mkThrowawayRepo/writeE2EConfig)
// and never touches the user's checkouts.
//
// These tests assume the harness API in package e2e (harness.go):
//   - var binWrk3 string
//   - func mkThrowawayRepo(t *testing.T) string
//   - func writeE2EConfig(t *testing.T, repoDir string) string
//   - func runWrk3(t *testing.T, repoDir, cfg string, args ...string) (stdout, stderr string, err error)
//   - func mustContain(t *testing.T, out, sub string)

// edgeStateRecord mirrors ports.WorktreeRecord for state-file assertions.
type edgeStateRecord struct {
	Branch         string         `json:"branch"`
	Slug           string         `json:"slug"`
	AbsPath        string         `json:"absPath"`
	Index          int            `json:"index"`
	Ports          map[string]int `json:"ports"`
	ComposeProject string         `json:"composeProject"`
	Status         string         `json:"status"`
}

// edgeTryGit runs git in repoDir and returns its combined output.
func edgeTryGit(repoDir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = repoDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// edgeRunGit runs git in repoDir, failing the test on error.
func edgeRunGit(t *testing.T, repoDir string, args ...string) string {
	t.Helper()
	out, err := edgeTryGit(repoDir, args...)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// edgeStatePath returns the conventional state-file path for a repo whose
// worktreeBase is .worktrees.
func edgeStatePath(repoDir string) string {
	return filepath.Join(repoDir, ".worktrees", ".wrk3-state.json")
}

// edgeReadState parses the state file; a missing file means zero records.
func edgeReadState(t *testing.T, repoDir string) []edgeStateRecord {
	t.Helper()
	raw, err := os.ReadFile(edgeStatePath(repoDir))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	var recs []edgeStateRecord
	if err := json.Unmarshal(raw, &recs); err != nil {
		t.Fatalf("parse state file: %v", err)
	}
	return recs
}

// edgeFindRecord returns the state record for branch (or slug), failing the
// test when it is absent.
func edgeFindRecord(t *testing.T, recs []edgeStateRecord, branch string) edgeStateRecord {
	t.Helper()
	for _, rec := range recs {
		if rec.Branch == branch || rec.Slug == branch {
			return rec
		}
	}
	t.Fatalf("state has no record for %q", branch)
	return edgeStateRecord{}
}

// edgeHasRecord reports whether recs contains branch (or slug).
func edgeHasRecord(recs []edgeStateRecord, branch string) bool {
	for _, rec := range recs {
		if rec.Branch == branch || rec.Slug == branch {
			return true
		}
	}
	return false
}

// edgeReadDotEnv reads the worktree .env, failing the test when missing.
func edgeReadDotEnv(t *testing.T, worktreePath string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(worktreePath, ".env"))
	if err != nil {
		t.Fatalf("read worktree .env: %v", err)
	}
	return string(raw)
}

// edgeCombined joins stdout+stderr for "either stream" assertions.
func edgeCombined(stdout, stderr string) string {
	return stdout + "\n" + stderr
}

// edgeCurrentBranch reports the checked-out branch of repoDir.
func edgeCurrentBranch(t *testing.T, repoDir string) string {
	t.Helper()
	out := edgeRunGit(t, repoDir, "rev-parse", "--abbrev-ref", "HEAD")
	return strings.TrimSpace(out)
}

// edgeEnsureRemoteBranch creates newBranch from base and pushes it to origin.
func edgeEnsureRemoteBranch(t *testing.T, repoDir, newBranch, base string) {
	t.Helper()
	edgeRunGit(t, repoDir, "branch", newBranch, base)
	edgeRunGit(t, repoDir, "push", "origin", newBranch)
}

// TestE2E_CreateWithFlag (E01): add with --create registers the new branch,
// creates the worktree dir, and persists state.
func TestE2E_CreateWithFlag(t *testing.T) {
	repoDir := mkThrowawayRepo(t)
	cfg := writeE2EConfig(t, repoDir)

	stdout, _, err := runWrk3(t, repoDir, cfg, "add", "feat/new", "--create")
	if err != nil {
		t.Fatalf("add --create: %v", err)
	}
	mustContain(t, stdout, "added")

	rec := edgeFindRecord(t, edgeReadState(t, repoDir), "feat/new")
	st, err := os.Stat(rec.AbsPath)
	if err != nil {
		t.Fatalf("stat worktree dir %q: %v", rec.AbsPath, err)
	}
	if !st.IsDir() {
		t.Fatalf("worktree path %q is not a directory", rec.AbsPath)
	}
}

// TestE2E_NoCreateFailsFast (E02): add with --no-create errors on unknown
// names, points at --create, and creates nothing.
func TestE2E_NoCreateFailsFast(t *testing.T) {
	repoDir := mkThrowawayRepo(t)
	cfg := writeE2EConfig(t, repoDir)

	stdout, stderr, err := runWrk3(t, repoDir, cfg, "add", "feat/unknown", "--no-create")
	if err == nil {
		t.Fatalf("add --no-create on unknown branch succeeded, want failure (out=%q)", stdout)
	}
	mustContain(t, edgeCombined(stdout, stderr), "--create")

	if recs := edgeReadState(t, repoDir); edgeHasRecord(recs, "feat/unknown") {
		t.Fatalf("state contains feat/unknown after failed --no-create add")
	}
	if _, statErr := os.Stat(filepath.Join(repoDir, ".worktrees", "feat-unknown")); !os.IsNotExist(statErr) {
		t.Fatalf("worktree dir for feat/unknown exists after failed --no-create add")
	}
}

// TestE2E_CreatePromptEOF (E03): bare add of an unknown branch with empty
// stdin (EOF) cancels instead of hanging, hinting at --create.
// The interactive y-path is deliberately not exercised here to avoid hangs.
func TestE2E_CreatePromptEOF(t *testing.T) {
	repoDir := mkThrowawayRepo(t)
	cfg := writeE2EConfig(t, repoDir)

	stdout, stderr, err := runWrk3(t, repoDir, cfg, "add", "feat/new2")
	if err == nil {
		t.Fatalf("bare add of unknown branch with EOF stdin succeeded, want cancellation")
	}
	combined := edgeCombined(stdout, stderr)
	mustContain(t, combined, "cancelled")
	mustContain(t, combined, "--create")

	if recs := edgeReadState(t, repoDir); edgeHasRecord(recs, "feat/new2") {
		t.Fatalf("state contains feat/new2 after cancelled add")
	}
}

// TestE2E_PortAllocation (E04): the first managed worktree gets app=8100,
// the second app=8200 (base 8000, step 100; index 0 is reserved for main).
func TestE2E_PortAllocation(t *testing.T) {
	repoDir := mkThrowawayRepo(t)
	cfg := writeE2EConfig(t, repoDir)

	// feature/bar must exist on the remote so plain `add` picks it up.
	base := "main"
	if _, err := edgeTryGit(repoDir, "rev-parse", "--verify", "--quiet", "refs/heads/main"); err != nil {
		base = edgeCurrentBranch(t, repoDir)
	}
	edgeEnsureRemoteBranch(t, repoDir, "feature/bar", base)

	if _, _, err := runWrk3(t, repoDir, cfg, "add", "feature/foo", "--create"); err != nil {
		t.Fatalf("add feature/foo --create: %v", err)
	}
	if _, _, err := runWrk3(t, repoDir, cfg, "add", "feature/bar"); err != nil {
		t.Fatalf("add feature/bar: %v", err)
	}

	recs := edgeReadState(t, repoDir)
	foo := edgeFindRecord(t, recs, "feature/foo")
	bar := edgeFindRecord(t, recs, "feature/bar")
	if foo.Ports["app"] != 8100 {
		t.Errorf("feature/foo app port = %d, want 8100", foo.Ports["app"])
	}
	if bar.Ports["app"] != 8200 {
		t.Errorf("feature/bar app port = %d, want 8200", bar.Ports["app"])
	}
}

// TestE2E_EnvPreservesSecrets (E05): user-owned .env keys survive `up`
// (managed keys are gap-filled, never overwritten).
func TestE2E_EnvPreservesSecrets(t *testing.T) {
	repoDir := mkThrowawayRepo(t)
	cfg := writeE2EConfig(t, repoDir)

	if _, _, err := runWrk3(t, repoDir, cfg, "add", "feat/secrets", "--create"); err != nil {
		t.Fatalf("add feat/secrets --create: %v", err)
	}
	rec := edgeFindRecord(t, edgeReadState(t, repoDir), "feat/secrets")

	f, err := os.OpenFile(filepath.Join(rec.AbsPath, ".env"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open worktree .env: %v", err)
	}
	if _, err := f.WriteString("SECRET=topsecret\n"); err != nil {
		_ = f.Close()
		t.Fatalf("append SECRET to worktree .env: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close worktree .env: %v", err)
	}

	// up re-ensures .env first; the compose result itself is not the point
	// of this test, only that the .env round-trips secrets.
	_, _, _ = runWrk3(t, repoDir, cfg, "up", "feat/secrets")

	content := edgeReadDotEnv(t, rec.AbsPath)
	mustContain(t, content, "SECRET=topsecret")
	mustContain(t, content, "APP_PORT=")
}

// TestE2E_EnvDivergenceWarns (E06): a pre-existing managed key with a
// different value is left intact and reported as a warning on `up`.
func TestE2E_EnvDivergenceWarns(t *testing.T) {
	repoDir := mkThrowawayRepo(t)
	cfg := writeE2EConfig(t, repoDir)

	if _, _, err := runWrk3(t, repoDir, cfg, "add", "feat/diverged", "--create"); err != nil {
		t.Fatalf("add feat/diverged --create: %v", err)
	}
	rec := edgeFindRecord(t, edgeReadState(t, repoDir), "feat/diverged")

	if err := os.WriteFile(filepath.Join(rec.AbsPath, ".env"), []byte("APP_PORT=5000\n"), 0o644); err != nil {
		t.Fatalf("write diverged worktree .env: %v", err)
	}

	stdout, stderr, _ := runWrk3(t, repoDir, cfg, "up", "feat/diverged")
	combined := edgeCombined(stdout, stderr)
	mustContain(t, combined, "APP_PORT")
	mustContain(t, strings.ToLower(combined), "warning")

	content := edgeReadDotEnv(t, rec.AbsPath)
	mustContain(t, content, "APP_PORT=5000")
}

// TestE2E_AdoptsOrphanAfterStateLoss (E07): deleting the state file does not
// lose worktrees — the next read re-adopts on-disk checkouts.
func TestE2E_AdoptsOrphanAfterStateLoss(t *testing.T) {
	repoDir := mkThrowawayRepo(t)
	cfg := writeE2EConfig(t, repoDir)

	if _, _, err := runWrk3(t, repoDir, cfg, "add", "feature/foo", "--create"); err != nil {
		t.Fatalf("add feature/foo --create: %v", err)
	}
	if err := os.Remove(edgeStatePath(repoDir)); err != nil {
		t.Fatalf("remove state file: %v", err)
	}

	stdout, _, err := runWrk3(t, repoDir, cfg, "status")
	if err != nil {
		t.Fatalf("status after state loss: %v", err)
	}
	mustContain(t, stdout, "feature/foo")

	if recs := edgeReadState(t, repoDir); !edgeHasRecord(recs, "feature/foo") {
		t.Fatalf("state was not rebuilt with feature/foo after status")
	}
}

// TestE2E_MainCheckout (E09): a fresh repo lists the implicit main checkout
// at the base allocation, and main can never be removed.
func TestE2E_MainCheckout(t *testing.T) {
	repoDir := mkThrowawayRepo(t)
	cfg := writeE2EConfig(t, repoDir)

	main := edgeCurrentBranch(t, repoDir)

	stdout, _, err := runWrk3(t, repoDir, cfg, "status")
	if err != nil {
		t.Fatalf("status on fresh repo: %v", err)
	}
	mustContain(t, stdout, main)
	mustContain(t, stdout, "8000")

	stdout, stderr, err := runWrk3(t, repoDir, cfg, "remove", main)
	if err == nil {
		t.Fatalf("remove of main checkout succeeded, want refusal (out=%q)", stdout)
	}
	mustContain(t, edgeCombined(stdout, stderr), "refusing")
}

// TestE2E_ExecEnv (E10): exec runs with the worktree's allocated ports in
// the environment.
func TestE2E_ExecEnv(t *testing.T) {
	repoDir := mkThrowawayRepo(t)
	cfg := writeE2EConfig(t, repoDir)

	if _, _, err := runWrk3(t, repoDir, cfg, "add", "feature/foo", "--create"); err != nil {
		t.Fatalf("add feature/foo --create: %v", err)
	}
	if _, _, err := runWrk3(t, repoDir, cfg, "exec", "feature/foo", "--", "sh", "-c", `test "$APP_PORT" = 8100`); err != nil {
		t.Fatalf("exec with APP_PORT=8100: %v", err)
	}
}

// TestE2E_StaleDir (E11): a worktree whose directory was deleted shows as
// stale (exit 0) and `remove --force` drops it from state.
func TestE2E_StaleDir(t *testing.T) {
	repoDir := mkThrowawayRepo(t)
	cfg := writeE2EConfig(t, repoDir)

	if _, _, err := runWrk3(t, repoDir, cfg, "add", "feat/stale", "--create"); err != nil {
		t.Fatalf("add feat/stale --create: %v", err)
	}
	rec := edgeFindRecord(t, edgeReadState(t, repoDir), "feat/stale")

	if err := os.RemoveAll(rec.AbsPath); err != nil {
		t.Fatalf("remove worktree dir %q: %v", rec.AbsPath, err)
	}

	stdout, stderr, err := runWrk3(t, repoDir, cfg, "status")
	if err != nil {
		t.Fatalf("status with stale dir: %v", err)
	}
	combined := edgeCombined(stdout, stderr)
	if !strings.Contains(combined, "stale") && !strings.Contains(combined, "?") {
		t.Fatalf("status output mentions neither stale nor ?: %q", combined)
	}

	if _, _, err := runWrk3(t, repoDir, cfg, "remove", "feat/stale", "--force"); err != nil {
		t.Fatalf("remove --force of stale worktree: %v", err)
	}
	if recs := edgeReadState(t, repoDir); edgeHasRecord(recs, "feat/stale") {
		t.Fatalf("state still contains feat/stale after remove --force")
	}
}
