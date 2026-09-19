//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// binWrk3 is the absolute path to the built wrk3 binary under test.
// It is set once by TestMain in e2e_test.go.
var binWrk3 string

// runGit runs git with args in dir, returning combined output on failure
// wrapped with context.
func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s in %s: %w: %s", strings.Join(args, " "), dir, err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// mkThrowawayRepo creates an isolated throwaway git repo for e2e tests:
// <root>/origin.git (bare) + <root>/repo (main checkout) with f.txt and a
// minimal docker-compose.yml committed, pushed to origin, plus a
// feature/foo branch pushed to origin. It returns the repo checkout dir.
//
// HOME and XDG_CONFIG_HOME point at fresh temp dirs so registry writes
// (~/.config/wrk3) never touch the user's real config. All git failures
// are wrapped with context. The compose file uses alpine:3.19
// (sleep infinity) so `up`/`down` succeed against a real daemon without
// pulling; docker_test.go rewrites the same file and tolerates
// "nothing to commit".
func mkThrowawayRepo(t *testing.T) (repoDir string) {
	t.Helper()

	// Capture the real docker config before HOME isolation: Docker Desktop
	// installs the compose plugin under ~/.docker/cli-plugins, so a bare
	// temp HOME would make `docker compose` resolve as "unknown command".
	// Pointing DOCKER_CONFIG at the real dir keeps the daemon reachable
	// while wrk3 state (~/.config/wrk3) stays isolated via HOME/XDG below.
	realDockerConfig := os.Getenv("DOCKER_CONFIG")
	if realDockerConfig == "" {
		if realHome, ok := os.LookupEnv("HOME"); ok && strings.TrimSpace(realHome) != "" {
			realDockerConfig = filepath.Join(realHome, ".docker")
		}
	}

	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if realDockerConfig != "" {
		t.Setenv("DOCKER_CONFIG", realDockerConfig)
	}

	bare := filepath.Join(root, "origin.git")
	repoDir = filepath.Join(root, "repo")

	if _, err := runGit(root, "init", "--bare", "-b", "main", bare); err != nil {
		t.Fatalf("init bare origin: %v", err)
	}
	if out, err := exec.Command("git", "init", "-b", "main", repoDir).CombinedOutput(); err != nil {
		t.Fatalf("git init repo: %v\n%s", err, strings.TrimSpace(string(out)))
	}
	if _, err := runGit(repoDir, "config", "user.email", "e2e@example.com"); err != nil {
		t.Fatalf("git config email: %v", err)
	}
	if _, err := runGit(repoDir, "config", "user.name", "E2E"); err != nil {
		t.Fatalf("git config name: %v", err)
	}

	if err := os.WriteFile(filepath.Join(repoDir, "f.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write f.txt: %v", err)
	}
	const compose = `services:
  app:
    image: alpine:3.19
    command: ["sleep", "infinity"]
`
	if err := os.WriteFile(filepath.Join(repoDir, "docker-compose.yml"), []byte(compose), 0o644); err != nil {
		t.Fatalf("write docker-compose.yml: %v", err)
	}
	if _, err := runGit(repoDir, "add", "f.txt", "docker-compose.yml"); err != nil {
		t.Fatalf("git add: %v", err)
	}
	if _, err := runGit(repoDir, "commit", "-m", "init"); err != nil {
		t.Fatalf("git commit: %v", err)
	}
	if _, err := runGit(repoDir, "remote", "add", "origin", bare); err != nil {
		t.Fatalf("git remote add origin: %v", err)
	}
	if _, err := runGit(repoDir, "push", "origin", "main"); err != nil {
		t.Fatalf("git push origin main: %v", err)
	}

	if _, err := runGit(repoDir, "checkout", "-b", "feature/foo"); err != nil {
		t.Fatalf("git checkout -b feature/foo: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "feature.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatalf("write feature.txt: %v", err)
	}
	if _, err := runGit(repoDir, "add", "feature.txt"); err != nil {
		t.Fatalf("git add feature.txt: %v", err)
	}
	if _, err := runGit(repoDir, "commit", "-m", "feature"); err != nil {
		t.Fatalf("git commit feature: %v", err)
	}
	if _, err := runGit(repoDir, "push", "origin", "feature/foo"); err != nil {
		t.Fatalf("git push origin feature/foo: %v", err)
	}
	if _, err := runGit(repoDir, "checkout", "main"); err != nil {
		t.Fatalf("git checkout main: %v", err)
	}

	return repoDir
}

// writeE2EConfig writes a minimal no-op wrk3.yaml into repoDir and returns
// its path. Entry commands are echo-only; ports allocate app from 8000
// with step 100 so the first managed worktree gets 8100.
func writeE2EConfig(t *testing.T, repoDir string) string {
	t.Helper()

	const cfg = `project:
  worktreeBase: .worktrees
source:
  type: git
  git:
    remote: origin
    fetchPrune: true
runner:
  type: docker
  docker:
    composeFiles: [docker-compose.yml]
    projectPrefix: e2e
entry:
  setup: ["echo setup"]
  run: "echo run"
  stop: "echo stop"
  logs: "echo logs"
ports:
  base: {app: 8000}
  step: 100
`
	p := filepath.Join(repoDir, "wrk3.yaml")
	if err := os.WriteFile(p, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write wrk3.yaml: %v", err)
	}
	return p
}

// runWrk3 executes the built wrk3 binary with -f cfg plus args in repoDir,
// with a 60s timeout, capturing stdout and stderr separately.
func runWrk3(t *testing.T, repoDir, cfg string, args ...string) (string, string, error) {
	t.Helper()

	if strings.TrimSpace(binWrk3) == "" {
		t.Fatalf("binWrk3 not set (TestMain build failed?)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	full := append([]string{"-f", cfg}, args...)
	cmd := exec.CommandContext(ctx, binWrk3, full...)
	cmd.Dir = repoDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return stdout.String(), stderr.String(), fmt.Errorf("wrk3 %s: timeout after 60s: %w", strings.Join(full, " "), ctx.Err())
	}
	return stdout.String(), stderr.String(), err
}

// mustContain fails the test when out does not contain sub.
func mustContain(t *testing.T, out, sub string) {
	t.Helper()
	if !strings.Contains(out, sub) {
		t.Fatalf("output missing %q, got:\n%s", sub, out)
	}
}
