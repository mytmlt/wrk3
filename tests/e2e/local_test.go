//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeLocalE2EConfig writes a CLI-only wrk3.yaml (no compose, no ports).
func writeLocalE2EConfig(t *testing.T, repoDir string) string {
	t.Helper()
	const cfg = `project:
  worktreeBase: .worktrees
source:
  type: git
  git:
    remote: origin
    fetchPrune: true
runner:
  type: local
entry:
  setup: ["echo setup"]
  run: "echo built"
  stop: "echo stop"
  logs: "echo logs"
`
	p := filepath.Join(repoDir, "wrk3.yaml")
	if err := os.WriteFile(p, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write wrk3.yaml: %v", err)
	}
	return p
}

// TestE2E_LocalAddUpStatusDown (L01): CLI-only runner creates a worktree
// without ports/.env, up/status/logs/down succeed, status shows running
// then stopped with "-" for ports and compose project.
func TestE2E_LocalAddUpStatusDown(t *testing.T) {
	repoDir := mkThrowawayRepo(t)
	cfg := writeLocalE2EConfig(t, repoDir)

	if out, errOut, err := runWrk3(t, repoDir, cfg, "add", "feature/foo", "--no-create"); err != nil {
		t.Fatalf("add: %v\nstdout: %s\nstderr: %s", err, out, errOut)
	}

	wt := filepath.Join(repoDir, ".worktrees", "feature-foo")
	if st, err := os.Stat(wt); err != nil || !st.IsDir() {
		t.Fatalf("worktree dir %q missing: %v", wt, err)
	}
	if _, err := os.Stat(filepath.Join(wt, ".env")); !os.IsNotExist(err) {
		t.Fatalf("local runner must not write .env: %v", err)
	}

	recs := happyReadState(t, repoDir)
	if len(recs) != 1 {
		t.Fatalf("state recs = %d, want 1", len(recs))
	}
	if len(recs[0].Ports) != 0 {
		t.Errorf("Ports = %v, want empty", recs[0].Ports)
	}
	if recs[0].ComposeProject != "" {
		t.Errorf("ComposeProject = %q, want empty", recs[0].ComposeProject)
	}

	if out, errOut, err := runWrk3(t, repoDir, cfg, "up", "feature/foo"); err != nil {
		t.Fatalf("up: %v\nstdout: %s\nstderr: %s", err, out, errOut)
	} else if strings.Contains(out, "compose up") {
		t.Errorf("up stdout must not mention compose: %s", out)
	}

	stdout, errOut, err := runWrk3(t, repoDir, cfg, "status")
	if err != nil {
		t.Fatalf("status: %v\nstdout: %s\nstderr: %s", err, stdout, errOut)
	}
	if !strings.Contains(stdout, "feature-foo") {
		t.Errorf("status missing feature-foo:\n%s", stdout)
	}
	if !strings.Contains(stdout, "running") {
		t.Errorf("status after up should show running:\n%s", stdout)
	}
	if strings.Contains(stdout, "APP_PORT") || strings.Contains(stdout, "app=800") {
		t.Errorf("status should not allocate ports:\n%s", stdout)
	}

	if out, errOut, err := runWrk3(t, repoDir, cfg, "logs", "feature/foo"); err != nil {
		t.Fatalf("logs: %v\nstdout: %s\nstderr: %s", err, out, errOut)
	}

	if out, errOut, err := runWrk3(t, repoDir, cfg, "down", "feature/foo"); err != nil {
		t.Fatalf("down: %v\nstdout: %s\nstderr: %s", err, out, errOut)
	} else if strings.Contains(out, "compose down") {
		t.Errorf("down stdout must not mention compose: %s", out)
	}

	stdout, errOut, err = runWrk3(t, repoDir, cfg, "status")
	if err != nil {
		t.Fatalf("status after down: %v\nstdout: %s\nstderr: %s", err, stdout, errOut)
	}
	if !strings.Contains(stdout, "stopped") {
		t.Errorf("status after down should show stopped:\n%s", stdout)
	}
}
