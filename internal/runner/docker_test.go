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

func TestSanitizeProjectName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"demo-feature-foo", "demo-feature-foo"},
		{"Feature/Foo", "feature-foo"},
		{"DEMO", "demo"},
		{"UPPER_CASE.Name-1", "upper_case.name-1"},
		{"a B:c", "a-b-c"},
		{"feature//foo", "feature-foo"},
		{"---demo---", "demo"},
		{"", "wrk3"},
		{"///", "wrk3"},
		{"demo--feature-foo", "demo--feature-foo"},
	}
	for _, c := range cases {
		if got := SanitizeProjectName(c.in); got != c.want {
			t.Errorf("SanitizeProjectName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestOptionsProjectName(t *testing.T) {
	cases := []struct {
		opts Options
		want string
	}{
		{Options{ProjectPrefix: "demo", Slug: "feature-foo"}, "demo-feature-foo"},
		{Options{ProjectPrefix: "DEMO!", Slug: "Feature/Foo"}, "demo--feature-foo"},
		{Options{ProjectPrefix: "demo"}, "demo"},
		{Options{Slug: "my-branch"}, "my-branch"},
		{Options{}, "wrk3"},
	}
	for _, c := range cases {
		if got := c.opts.ProjectName(); got != c.want {
			t.Errorf("ProjectName(%+v) = %q, want %q", c.opts, got, c.want)
		}
		if r := New(c.opts); r.ProjectName() != c.want {
			t.Errorf("DockerRunner.ProjectName(%+v) = %q, want %q", c.opts, r.ProjectName(), c.want)
		}
	}
}

func TestBuildComposeArgs(t *testing.T) {
	got := buildComposeArgs("demo-foo",
		[]string{"docker-compose.yml", "docker-compose.override.yml"},
		"up", "-d", "--build")
	want := []string{"compose", "-p", "demo-foo",
		"-f", "docker-compose.yml", "-f", "docker-compose.override.yml",
		"up", "-d", "--build"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildComposeArgs = %v, want %v", got, want)
	}

	got = buildComposeArgs("demo-foo", nil, "ps", "-q")
	want = []string{"compose", "-p", "demo-foo", "ps", "-q"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildComposeArgs no-files = %v, want %v", got, want)
	}

	// Empty file entries are skipped.
	got = buildComposeArgs("demo-foo", []string{"", "a.yml"}, "down")
	want = []string{"compose", "-p", "demo-foo", "-f", "a.yml", "down"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildComposeArgs empty-file = %v, want %v", got, want)
	}

	// Runner-level helper threads project name + files through.
	r := New(Options{ComposeFiles: []string{"docker-compose.yml"}, ProjectPrefix: "demo", Slug: "a/b"})
	got = r.composeArgs("up", "-d")
	want = []string{"compose", "-p", "demo-a-b", "-f", "docker-compose.yml", "up", "-d"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("composeArgs = %v, want %v", got, want)
	}
}

func TestBuildEnvMapping(t *testing.T) {
	base := []string{"PATH=/usr/bin", "APP_PORT=1", "DUP=old"}
	extra := map[string]string{
		"APP_PORT":             "8001",
		"WEB_PORT":             "3000",
		"COMPOSE_PROJECT_NAME": "hacker",
	}
	got := buildEnv(base, "demo-foo", extra)
	env := map[string]string{}
	count := map[string]int{}
	for _, kv := range got {
		k := kv[:strings.IndexByte(kv, '=')]
		v := kv[strings.IndexByte(kv, '=')+1:]
		env[k] = v
		count[k]++
	}
	for k, n := range count {
		if n != 1 {
			t.Errorf("key %q appears %d times, want 1", k, n)
		}
	}
	if env["APP_PORT"] != "8001" {
		t.Errorf("APP_PORT = %q, want 8001", env["APP_PORT"])
	}
	if env["WEB_PORT"] != "3000" {
		t.Errorf("WEB_PORT = %q, want 3000", env["WEB_PORT"])
	}
	// Project isolation wins over caller-supplied COMPOSE_PROJECT_NAME.
	if env["COMPOSE_PROJECT_NAME"] != "demo-foo" {
		t.Errorf("COMPOSE_PROJECT_NAME = %q, want demo-foo", env["COMPOSE_PROJECT_NAME"])
	}
	if env["PATH"] != "/usr/bin" || env["DUP"] != "old" {
		t.Errorf("base env not preserved: %v", env)
	}
}

func TestStubsNotImplemented(t *testing.T) {
	ctx := context.Background()
	env := map[string]string{"APP_PORT": "8001"}
	for _, r := range []Runner{PortainerRunner{}, NomadRunner{}, &PortainerRunner{}, &NomadRunner{}} {
		if err := r.Up(ctx, "/tmp/x", env); !isNotImplemented(err) {
			t.Errorf("%T.Up = %v, want not implemented", r, err)
		}
		if err := r.Down(ctx, "/tmp/x", env); !isNotImplemented(err) {
			t.Errorf("%T.Down = %v, want not implemented", r, err)
		}
		if _, err := r.Logs(ctx, "/tmp/x", false); !isNotImplemented(err) {
			t.Errorf("%T.Logs = %v, want not implemented", r, err)
		}
		if err := r.Exec(ctx, "/tmp/x", []string{"make", "test"}, env); !isNotImplemented(err) {
			t.Errorf("%T.Exec = %v, want not implemented", r, err)
		}
		if _, err := r.Status(ctx, "/tmp/x"); !isNotImplemented(err) {
			t.Errorf("%T.Status = %v, want not implemented", r, err)
		}
	}
}

func isNotImplemented(err error) bool {
	return err != nil && strings.Contains(err.Error(), "not implemented")
}

func TestDockerRunnerValidationNoDaemon(t *testing.T) {
	ctx := context.Background()
	r := New(Options{ProjectPrefix: "demo", Slug: "x"})
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

func TestDockerRunnerExecHostCommand(t *testing.T) {
	// Exercises Exec's cwd+env plumbing with a host binary, no daemon.
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skipf("sh not found: %v", err)
	}
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r := New(Options{ProjectPrefix: "demo", Slug: "exec-test"})
	env := map[string]string{"WRK3_TEST_PORT": "8123"}
	if err := r.Exec(ctx, dir, []string{"sh", "-c", "test \"$WRK3_TEST_PORT\" = 8123 && test \"$COMPOSE_PROJECT_NAME\" = demo-exec-test"}, env); err != nil {
		t.Fatalf("Exec sh: %v", err)
	}
}

func TestDockerRunnerParallelDistinctProjects(t *testing.T) {
	if testing.Short() {
		t.Skip("docker integration: run with go test -short=false")
	}
	if runtime.GOOS == "windows" {
		t.Skip("linux-only test image (alpine) has no windows manifest")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker binary not found")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, "docker", "info").Run(); err != nil {
		t.Skipf("docker daemon unavailable: %v", err)
	}

	mkWorktree := func() string {
		dir := t.TempDir()
		compose := `services:
  app:
    image: alpine:3.19
    command: ["sleep", "infinity"]
`
		if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(compose), 0o644); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	dirA, dirB := mkWorktree(), mkWorktree()
	rA := New(Options{ComposeFiles: []string{"docker-compose.yml"}, ProjectPrefix: "demotest", Slug: "par-a"})
	rB := New(Options{ComposeFiles: []string{"docker-compose.yml"}, ProjectPrefix: "demotest", Slug: "par-b"})
	rA.Timeout, rB.Timeout = 2*time.Minute, 2*time.Minute
	if rA.ProjectName() == rB.ProjectName() {
		t.Fatalf("project names collide: %q", rA.ProjectName())
	}
	envA := map[string]string{"APP_PORT": "8941"}
	envB := map[string]string{"APP_PORT": "9041"}

	opCtx, cancelOp := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancelOp()
	t.Cleanup(func() {
		dctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		_ = rA.Down(dctx, dirA, envA)
		_ = rB.Down(dctx, dirB, envB)
	})

	errCh := make(chan error, 2)
	go func() { errCh <- rA.Up(opCtx, dirA, envA) }()
	go func() { errCh <- rB.Up(opCtx, dirB, envB) }()
	for range 2 {
		if err := <-errCh; err != nil {
			t.Fatalf("parallel Up: %v", err)
		}
	}

	for i, tc := range []struct {
		r   *DockerRunner
		dir string
		env map[string]string
	}{
		{rA, dirA, envA},
		{rB, dirB, envB},
	} {
		st, err := tc.r.Status(opCtx, tc.dir)
		if err != nil || !st.Running {
			t.Fatalf("runner %d Status = %+v, err = %v, want running", i, st, err)
		}
	}

	// Both compose projects visible in docker ps with distinct names.
	psCtx, cancelPs := context.WithTimeout(context.Background(), time.Minute)
	defer cancelPs()
	out, err := exec.CommandContext(psCtx, "docker", "ps", "--format", "{{.Names}}").Output()
	if err != nil {
		t.Fatalf("docker ps: %v", err)
	}
	for _, want := range []string{"demotest-par-a", "demotest-par-b"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("docker ps output missing project %q:\n%s", want, out)
		}
	}

	for i, tc := range []struct {
		r   *DockerRunner
		dir string
		env map[string]string
	}{
		{rA, dirA, envA},
		{rB, dirB, envB},
	} {
		if err := tc.r.Down(opCtx, tc.dir, tc.env); err != nil {
			t.Fatalf("runner %d Down: %v", i, err)
		}
	}
}

// TestDockerRunnerIntegration brings up a stub compose repo
// (sleep infinity) and asserts Up/Status/Logs/Exec/Down. Skips
// gracefully when docker or a daemon is unavailable.
func TestDockerRunnerIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("docker integration: run with go test -short=false")
	}
	if runtime.GOOS == "windows" {
		t.Skip("linux-only test image (alpine) has no windows manifest")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker binary not found")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, "docker", "info").Run(); err != nil {
		t.Skipf("docker daemon unavailable: %v", err)
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
	slug := "itest-a"
	r := New(Options{
		ComposeFiles:  []string{"docker-compose.yml"},
		ProjectPrefix: "demotest",
		Slug:          slug,
	})
	r.Timeout = 2 * time.Minute
	env := map[string]string{"APP_PORT": "8931"}

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
