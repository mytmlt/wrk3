package runner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestPodmanRunnerProjectName(t *testing.T) {
	r := NewPodman(Options{ProjectPrefix: "demo", Slug: "feature-foo"})
	if got := r.ProjectName(); got != "demo-feature-foo" {
		t.Errorf("PodmanRunner.ProjectName = %q, want demo-feature-foo", got)
	}
}

func TestPodmanComposeArgs(t *testing.T) {
	r := NewPodman(Options{ComposeFiles: []string{"docker-compose.yml"}, ProjectPrefix: "demo", Slug: "a/b"})
	got := r.composeArgs("up", "-d", "--build")
	want := []string{"compose", "-p", "demo-a-b", "-f", "docker-compose.yml", "up", "-d", "--build"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("composeArgs = %v, want %v", got, want)
	}
}

func TestPodmanRunnerValidationNoDaemon(t *testing.T) {
	ctx := context.Background()
	r := NewPodman(Options{ProjectPrefix: "demo", Slug: "x"})
	if err := r.Up(ctx, "", nil); err == nil {
		t.Error("Up with empty path should fail without a daemon")
	}
	if err := r.Down(ctx, "  ", nil); err == nil {
		t.Error("Down with blank path should fail without a daemon")
	}
	if _, err := r.Logs(ctx, "", false); err == nil {
		t.Error("Logs with empty path should fail without a daemon")
	}
	if err := r.Exec(ctx, t.TempDir(), nil, nil); err == nil {
		t.Error("Exec with empty cmd should fail without a daemon")
	}
	if err := r.Exec(ctx, "", []string{"true"}, nil); err == nil {
		t.Error("Exec with empty path should fail without a daemon")
	}
	if _, err := r.Status(ctx, ""); err == nil {
		t.Error("Status with empty path should fail without a daemon")
	}
}

func TestPodmanUpFailsFastOnContainerNameNoDaemon(t *testing.T) {
	dir := t.TempDir()
	compose := "services:\n  app:\n    image: alpine:3.19\n    container_name: fixed\n"
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(compose), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewPodman(Options{ComposeFiles: []string{"docker-compose.yml"}, ProjectPrefix: "demo", Slug: "x"})
	if err := r.Up(context.Background(), dir, nil); err == nil {
		t.Fatal("Up with container_name = nil, want offender error (no daemon needed)")
	}
}

func TestPodmanRunnerExecHostCommand(t *testing.T) {
	// Exercises Exec's cwd+env plumbing with a host binary, no daemon.
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skipf("sh not found: %v", err)
	}
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r := NewPodman(Options{ProjectPrefix: "demo", Slug: "exec-test"})
	env := map[string]string{"WRK3_TEST_PORT": "8123"}
	if err := r.Exec(ctx, dir, []string{"sh", "-c", "test \"$WRK3_TEST_PORT\" = 8123 && test \"$COMPOSE_PROJECT_NAME\" = demo-exec-test"}, env); err != nil {
		t.Fatalf("Exec sh: %v", err)
	}
}

func TestPodmanRunnerTimeoutDefault(t *testing.T) {
	r := NewPodman(Options{})
	if got := r.timeout(); got != defaultPodmanTimeout {
		t.Errorf("timeout() = %v, want %v", got, defaultPodmanTimeout)
	}
	r.Timeout = time.Minute
	if got := r.timeout(); got != time.Minute {
		t.Errorf("timeout() = %v, want 1m", got)
	}
}

func TestPodmanRunnerErrorPrefixes(t *testing.T) {
	ctx := context.Background()
	r := NewPodman(Options{ProjectPrefix: "demo", Slug: "x"})
	if err := r.Up(ctx, "", nil); err == nil || !strings.Contains(err.Error(), "podman up") {
		t.Errorf("Up error = %v, want podman up prefix", err)
	}
	if err := r.Down(ctx, "", nil); err == nil || !strings.Contains(err.Error(), "podman down") {
		t.Errorf("Down error = %v, want podman down prefix", err)
	}
	if _, err := r.Logs(ctx, "", false); err == nil || !strings.Contains(err.Error(), "podman logs") {
		t.Errorf("Logs error = %v, want podman logs prefix", err)
	}
	if _, err := r.Status(ctx, ""); err == nil || !strings.Contains(err.Error(), "podman status") {
		t.Errorf("Status error = %v, want podman status prefix", err)
	}
}

// TestPodmanRunnerIntegration brings up a stub compose repo
// (sleep infinity) and asserts Up/Status/Logs/Exec/Down. Skips
// gracefully when podman or a working backend is unavailable.
func TestPodmanRunnerIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("podman integration: run with go test -short=false")
	}
	if runtime.GOOS == "windows" {
		t.Skip("linux-only test image (alpine) has no windows manifest")
	}
	if _, err := exec.LookPath("podman"); err != nil {
		t.Skip("podman binary not found")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, "podman", "info").Run(); err != nil {
		t.Skipf("podman backend unavailable: %v", err)
	}

	dir := t.TempDir()
	compose := `services:
  app:
    image: alpine:3.19
    command: ["sleep", "infinity"]
`
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(compose), 0o644); err != nil {
		t.Fatal(err)
	}
	slug := "itest-podman-a"
	r := NewPodman(Options{
		ComposeFiles:  []string{"docker-compose.yml"},
		ProjectPrefix: "demotest",
		Slug:          slug,
	})
	r.Timeout = 2 * time.Minute
	env := map[string]string{"APP_PORT": "8951"}

	opCtx, cancelOp := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancelOp()
	if err := r.Up(opCtx, dir, env); err != nil {
		t.Fatalf("Up: %v", err)
	}
	t.Cleanup(func() {
		dctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		_ = r.Down(dctx, dir, env)
	})

	st, err := r.Status(opCtx, dir)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.State != StateRunning || !st.Running {
		t.Fatalf("Status = %+v, want running", st)
	}

	logs, err := r.Logs(opCtx, dir, false)
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	_ = logs // sleep infinity emits nothing; success is the assertion.

	if err := r.Exec(opCtx, dir, []string{"true"}, env); err != nil {
		t.Fatalf("Exec true: %v", err)
	}

	if err := r.Down(opCtx, dir, env); err != nil {
		t.Fatalf("Down: %v", err)
	}
	st, err = r.Status(opCtx, dir)
	if err != nil {
		t.Fatalf("Status after down: %v", err)
	}
	if st.State != StateStopped || st.Running {
		t.Fatalf("Status after down = %+v, want stopped", st)
	}
}
