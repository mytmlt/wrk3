package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mytmlt/wrk3/internal/config"
	"github.com/mytmlt/wrk3/internal/ports"
	"github.com/mytmlt/wrk3/internal/runner"
)

// sharedProbeRunner is a scripted Runner capturing ordered calls,
// per-call env, and the Options it was constructed with.
type sharedProbeRunner struct {
	opts  runner.Options
	mu    sync.Mutex
	calls []string
	envs  []map[string]string
}

func (p *sharedProbeRunner) record(call string, env map[string]string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, call)
	cp := map[string]string{}
	for k, v := range env {
		cp[k] = v
	}
	p.envs = append(p.envs, cp)
}

func (p *sharedProbeRunner) Up(_ context.Context, worktreePath string, env map[string]string) error {
	p.record("Up "+worktreePath, env)
	return nil
}

func (p *sharedProbeRunner) Down(_ context.Context, worktreePath string, env map[string]string) error {
	p.record("Down "+worktreePath, env)
	return nil
}

func (p *sharedProbeRunner) Logs(context.Context, string, bool) (string, error) { return "", nil }

func (p *sharedProbeRunner) Exec(_ context.Context, _ string, cmd []string, env map[string]string) error {
	p.record("Exec "+strings.Join(cmd, " "), env)
	return nil
}

func (p *sharedProbeRunner) Status(context.Context, string) (runner.Status, error) {
	return runner.Status{State: runner.StateRunning, Running: true, Detail: "2 container(s)"}, nil
}

type sharedProbeRig struct {
	mu     sync.Mutex
	probes []*sharedProbeRunner
}

func (rig *sharedProbeRig) register(t *testing.T) string {
	t.Helper()
	typ := fmt.Sprintf("shared-probe-%s", strings.Replace(t.Name(), "/", "-", -1))
	runner.Register(typ, func(o runner.Options) runner.Runner {
		p := &sharedProbeRunner{opts: o}
		rig.mu.Lock()
		rig.probes = append(rig.probes, p)
		rig.mu.Unlock()
		return p
	})
	return typ
}

func (rig *sharedProbeRig) bySlug(slug string) *sharedProbeRunner {
	rig.mu.Lock()
	defer rig.mu.Unlock()
	for _, p := range rig.probes {
		if p.opts.Slug == slug {
			return p
		}
	}
	return nil
}

// sharedTestResolved builds a resolved with a shared-services config
// using unique runner type typ (services publish no ports, so the
// readiness wait is a no-op and no daemon is needed).
func sharedTestResolved(t *testing.T, typ string) *resolved {
	t.Helper()
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	cfg.Runner.Type = typ
	cfg.Shared.Docker = config.SharedComposeConfig{
		ComposeFiles: []string{"docker-compose.dev.yml"},
		Project:      "demo-shared",
		Services:     map[string]config.SharedServiceConfig{"db": {}},
	}
	cfg.Shared.WorktreeServices = []string{"app"}
	cfg.Shared.Env = map[string]string{
		"DB_HOST": "host.docker.internal",
		"DB_NAME": "myapp_${slug_underscore}",
	}
	cfg.Shared.Setup = []string{"echo shared-setup-${slug_dash}"}
	return &resolved{cfg: cfg, stateP: filepath.Join(t.TempDir(), ".wrk3-state.json")}
}

func sharedTestRec(t *testing.T, parent string) ports.WorktreeRecord {
	t.Helper()
	dir := filepath.Join(parent, "feat-x")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return ports.WorktreeRecord{
		Branch:         "feat-x",
		Slug:           "feat-x",
		AbsPath:        dir,
		Index:          1,
		Ports:          map[string]int{"app": 8001},
		ComposeProject: "demo-feat-x",
	}
}

func TestUpOneSharedOrdering(t *testing.T) {
	rig := &sharedProbeRig{}
	r := sharedTestResolved(t, rig.register(t))
	rec := sharedTestRec(t, t.TempDir())
	var logs []string
	logf := func(format string, a ...any) { logs = append(logs, fmt.Sprintf(format, a...)) }
	if err := upOne(context.Background(), r, rec, logf); err != nil {
		t.Fatal(err)
	}
	// Shared project runner: fixed project, shared scope, no slug.
	sharedProbe := rig.bySlug("")
	if sharedProbe == nil {
		t.Fatal("shared runner was never constructed")
	}
	if got := sharedProbe.opts.ProjectName(); got != "demo-shared" {
		t.Errorf("shared project = %q, want demo-shared", got)
	}
	if len(sharedProbe.opts.Services) != 1 || sharedProbe.opts.Services[0] != "db" {
		t.Errorf("shared services = %v, want [db]", sharedProbe.opts.Services)
	}
	// Worktree runner: scoped to app with --no-deps and the overlay.
	wt := rig.bySlug("feat-x")
	if wt == nil {
		t.Fatal("worktree runner was never constructed")
	}
	if !wt.opts.NoDeps || len(wt.opts.Services) != 1 || wt.opts.Services[0] != "app" {
		t.Errorf("worktree scope = noDeps:%v services:%v", wt.opts.NoDeps, wt.opts.Services)
	}
	if len(wt.opts.ExtraFiles) != 1 {
		t.Fatalf("worktree overlay missing from runner options: %+v", wt.opts)
	}
	raw, err := os.ReadFile(wt.opts.ExtraFiles[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "DB_NAME: myapp_feat_x") {
		t.Errorf("worktree overlay missing expanded env:\n%s", raw)
	}
	// Exec order: shared setup before entry setup before run.
	var execs []string
	var execEnv map[string]string
	for i, c := range wt.calls {
		if strings.HasPrefix(c, "Exec ") {
			execs = append(execs, strings.TrimPrefix(c, "Exec "))
			if execEnv == nil {
				execEnv = wt.envs[i]
			}
		}
	}
	want := []string{
		"sh -c echo shared-setup-feat-x",
		"sh -c echo setup",
		"sh -c echo run",
	}
	if strings.Join(execs, "\n") != strings.Join(want, "\n") {
		t.Errorf("exec order = %v, want %v", execs, want)
	}
	if execEnv["DB_NAME"] != "myapp_feat_x" {
		t.Errorf("DB_NAME = %q, want myapp_feat_x", execEnv["DB_NAME"])
	}
	if execEnv["WRK3_SLUG"] != "feat-x" || execEnv["WRK3_SHARED_PROJECT"] != "demo-shared" {
		t.Errorf("WRK3 vars missing: %v", execEnv)
	}
	// Shared up was logged.
	joined := strings.Join(logs, "\n")
	for _, want := range []string{"[shared] compose up (demo-shared: db)", "[shared] up", "shared setup:"} {
		if !strings.Contains(joined, want) {
			t.Errorf("logs missing %q:\n%s", want, joined)
		}
	}
}

func TestDownOneSharedUntouched(t *testing.T) {
	rig := &sharedProbeRig{}
	r := sharedTestResolved(t, rig.register(t))
	rec := sharedTestRec(t, t.TempDir())
	var logs []string
	logf := func(format string, a ...any) { logs = append(logs, fmt.Sprintf(format, a...)) }
	if err := downOne(context.Background(), r, rec, logf); err != nil {
		t.Fatal(err)
	}
	// down never constructs the shared runner: the project-scoped
	// compose down cannot reach the shared project by construction.
	if p := rig.bySlug(""); p != nil {
		t.Errorf("down must not construct the shared runner (calls: %v)", p.calls)
	}
	wt := rig.bySlug("feat-x")
	if wt == nil {
		t.Fatal("worktree runner was never constructed")
	}
	downs := 0
	for _, c := range wt.calls {
		if strings.HasPrefix(c, "Down ") {
			downs++
		}
	}
	if downs != 1 {
		t.Errorf("worktree Down calls = %d, want 1 (%v)", downs, wt.calls)
	}
	joined := strings.Join(logs, "\n")
	if !strings.Contains(joined, "shared demo-shared untouched") {
		t.Errorf("down should reassure that shared survives:\n%s", joined)
	}
}

func TestSharedStatusLine(t *testing.T) {
	rig := &sharedProbeRig{}
	r := sharedTestResolved(t, rig.register(t))
	state, detail := probeSharedStatus(context.Background(), r)
	if state != "running" || detail != "2 container(s)" {
		t.Errorf("probeSharedStatus = %q %q", state, detail)
	}
	got := sharedStatusLine(r.cfg, state, detail)
	want := "shared: demo-shared running (2 container(s)) [services: db]"
	if got != want {
		t.Errorf("sharedStatusLine = %q, want %q", got, want)
	}
}
