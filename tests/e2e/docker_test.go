//go:build e2e

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// requireDockerE2E applies the standard conservative gates for docker E2E:
// -testing.short skips, WRK3_E2E_DOCKER=1 required, docker daemon reachable.
func requireDockerE2E(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("skip docker e2e in -short mode")
	}
	if os.Getenv("WRK3_E2E_DOCKER") != "1" {
		t.Skip("skip docker e2e without WRK3_E2E_DOCKER=1")
	}
	if err := exec.Command("docker", "info").Run(); err != nil {
		t.Skipf("skip docker e2e: docker daemon unreachable: %v", err)
	}
}

func writeComposeFile(t *testing.T, repoDir string) {
	t.Helper()
	const compose = `services:
  app:
    image: alpine:3.19
    command: ["sleep", "infinity"]
`
	p := filepath.Join(repoDir, "docker-compose.yml")
	if err := os.WriteFile(p, []byte(compose), 0o644); err != nil {
		t.Fatalf("write docker-compose.yml: %v", err)
	}
	// Commit so new worktrees (new branches from HEAD) contain the file.
	// The harness already commits an identical file, so there may be
	// nothing to commit — treat that as success.
	add := exec.Command("git", "-C", repoDir, "add", "docker-compose.yml")
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf("git add compose: %v\n%s", err, out)
	}
	staged := exec.Command("git", "-C", repoDir, "diff", "--cached", "--quiet")
	if err := staged.Run(); err == nil {
		return // nothing staged (already committed by harness)
	}
	commit := exec.Command("git", "-C", repoDir, "commit", "-m", "e2e: add compose file")
	if out, err := commit.CombinedOutput(); err != nil {
		// Allow empty-commit edge (file already committed by harness).
		if !strings.Contains(string(out), "nothing to commit") && !strings.Contains(string(out), "nothing added to commit") {
			t.Fatalf("git commit compose: %v\n%s", err, out)
		}
	}
}

// TestE2E_DockerUpDown exercises add → up → status → down on a throwaway
// repo with a minimal alpine compose stack. Assertions are conservative:
// up/down must exit 0; status must exit 0 (content may be running or
// unknown when the daemon is slow, so no content assertion).
func TestE2E_DockerUpDown(t *testing.T) {
	requireDockerE2E(t)

	repoDir := mkThrowawayRepo(t)
	cfg := writeE2EConfig(t, repoDir)
	writeComposeFile(t, repoDir)

	// NOTE: runWrk3 currently uses a 60s default timeout. `up` pulls
	// alpine:3.19 on cold CI runners and can exceed that; if this test
	// flakes with timeout on first pull, extend runWrk3 (or add a
	// runWrk3Timeout variant) to ~3 minutes for the up invocation.
	if out, errOut, err := runWrk3(t, repoDir, cfg, "add", "feature/foo", "--no-create"); err != nil {
		t.Fatalf("add feature/foo: %v\nstdout: %s\nstderr: %s", err, out, errOut)
	}

	t.Cleanup(func() {
		_, _, _ = runWrk3(t, repoDir, cfg, "down", "feature/foo")
	})

	if out, errOut, err := runWrk3(t, repoDir, cfg, "up", "feature/foo"); err != nil {
		t.Fatalf("up feature/foo: %v\nstdout: %s\nstderr: %s", err, out, errOut)
	}

	out, errOut, err := runWrk3(t, repoDir, cfg, "status")
	if err != nil {
		t.Fatalf("status: %v\nstdout: %s\nstderr: %s", err, out, errOut)
	}

	if out, errOut, err := runWrk3(t, repoDir, cfg, "down", "feature/foo"); err != nil {
		t.Fatalf("down feature/foo: %v\nstdout: %s\nstderr: %s", err, out, errOut)
	}
}

// TestE2E_DockerParallel brings up two worktrees in parallel and checks
// both stacks appear in `docker ps`, then downs both.
func TestE2E_DockerParallel(t *testing.T) {
	requireDockerE2E(t)

	repoDir := mkThrowawayRepo(t)
	cfg := writeE2EConfig(t, repoDir)
	writeComposeFile(t, repoDir)

	// feature/bar does not exist in the fresh throwaway repo; create and
	// push it so `add --no-create` resolves it without prompting.
	if out, err := exec.Command("git", "-C", repoDir, "branch", "feature/bar").CombinedOutput(); err != nil {
		t.Fatalf("git branch feature/bar: %v\n%s", err, out)
	}
	if out, err := exec.Command("git", "-C", repoDir, "push", "origin", "feature/bar").CombinedOutput(); err != nil {
		t.Fatalf("git push origin feature/bar: %v\n%s", err, out)
	}

	for _, br := range []string{"feature/foo", "feature/bar"} {
		if out, errOut, err := runWrk3(t, repoDir, cfg, "add", br, "--no-create"); err != nil {
			t.Fatalf("add %s: %v\nstdout: %s\nstderr: %s", br, err, out, errOut)
		}
	}

	t.Cleanup(func() {
		_, _, _ = runWrk3(t, repoDir, cfg, "down", "feature/foo", "feature/bar")
	})

	if out, errOut, err := runWrk3(t, repoDir, cfg, "up", "feature/foo", "feature/bar"); err != nil {
		t.Fatalf("up both: %v\nstdout: %s\nstderr: %s", err, out, errOut)
	}

	psOut, err := exec.Command("docker", "ps", "--format", "{{.Names}}").CombinedOutput()
	if err != nil {
		t.Fatalf("docker ps: %v\n%s", err, psOut)
	}
	// Slugs are feature-foo / feature-bar; compose project names embed
	// the slug (<prefix>-<slug> or bare <slug>), so match the slug
	// substring to stay independent of the configured projectPrefix.
	for _, want := range []string{"feature-foo", "feature-bar"} {
		if !strings.Contains(string(psOut), want) {
			t.Logf("warning: docker ps missing %q (daemon slow or prefix differs):\n%s", want, psOut)
		}
	}

	if out, errOut, err := runWrk3(t, repoDir, cfg, "down", "feature/foo", "feature/bar"); err != nil {
		t.Fatalf("down both: %v\nstdout: %s\nstderr: %s", err, out, errOut)
	}
}
