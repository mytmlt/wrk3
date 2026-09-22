package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mytmlt/wrk3/internal/ports"
	"github.com/mytmlt/wrk3/internal/runner"
)

// upProbeRunner is a scripted Runner used to verify the semantics of
// runUpTargets: worktrees execute in parallel, and the operation returns
// only after every entry command has actually finished running.
//
// Exec records each call only after its simulated work (execDelay) has
// completed, so the recorded list doubles as a "this command fully
// finished" ledger; maxActive proves concurrency by counting how many
// Exec calls were in flight at the same time.
type upProbeRunner struct {
	mu sync.Mutex
	// active/maxActive track how many Exec calls were in flight at once.
	active, maxActive int
	// execDone records "slug: <argv>" for every Exec call that completed;
	// upDone records every completed Up call.
	execDone []string
	upDone   []string
	// execDelay simulates per-worktree command runtime.
	execDelay map[string]time.Duration
	// execErr simulates per-worktree failures (setup/run return err).
	execErr map[string]error
}

func (p *upProbeRunner) Up(_ context.Context, worktreePath string, _ map[string]string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.upDone = append(p.upDone, filepath.Base(worktreePath))
	return nil
}

func (p *upProbeRunner) Down(context.Context, string, map[string]string) error { return nil }

func (p *upProbeRunner) Logs(context.Context, string, bool) (string, error) { return "", nil }

func (p *upProbeRunner) Exec(_ context.Context, worktreePath string, cmd []string, _ map[string]string) error {
	slug := filepath.Base(worktreePath)
	p.mu.Lock()
	p.active++
	if p.active > p.maxActive {
		p.maxActive = p.active
	}
	p.mu.Unlock()
	if d := p.execDelay[slug]; d > 0 {
		time.Sleep(d)
	}
	p.mu.Lock()
	p.execDone = append(p.execDone, slug+":"+strings.Join(cmd, " "))
	p.active--
	p.mu.Unlock()
	return p.execErr[slug]
}

func (p *upProbeRunner) Status(context.Context, string) (runner.Status, error) {
	return runner.Status{State: runner.StateRunning, Running: true}, nil
}

func (p *upProbeRunner) completed() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.execDone...)
}

func (p *upProbeRunner) maxInFlight() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.maxActive
}

var upProbeSeq atomic.Uint64

// registerUpProbe registers probe under a unique runner type so parallel
// tests never collide in the global runner registry.
func registerUpProbe(probe *upProbeRunner) string {
	typ := fmt.Sprintf("up-probe-%d", upProbeSeq.Add(1))
	runner.Register(typ, func(runner.Options) runner.Runner { return probe })
	return typ
}

// upProbeResolved builds a resolved with a fake runner type and a scratch
// state file, reusing the standard hermetic test config.
func upProbeResolved(t *testing.T, typ string) *resolved {
	t.Helper()
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	cfg.Runner.Type = typ
	return &resolved{
		cfg:    cfg,
		stateP: filepath.Join(t.TempDir(), ".wrk3-state.json"),
	}
}

// upProbeTargets builds worktree records for slugs, each backed by a real
// directory whose basename equals the slug (so the probe can key on it).
func upProbeTargets(t *testing.T, parent string, slugs []string) []ports.WorktreeRecord {
	t.Helper()
	recs := make([]ports.WorktreeRecord, 0, len(slugs))
	for i, slug := range slugs {
		dir := filepath.Join(parent, slug)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		recs = append(recs, ports.WorktreeRecord{
			Branch:         slug,
			Slug:           slug,
			AbsPath:        dir,
			Index:          i + 1,
			Ports:          map[string]int{"app": 8000 + i + 1},
			ComposeProject: "probe-" + slug,
		})
	}
	return recs
}

func noopLogf(string, ...any) {}

func runUpProbe(t *testing.T, r *resolved, targets []ports.WorktreeRecord) error {
	t.Helper()
	// In real use `add`/reconcile populate the state file before `up`;
	// mirror that so markStatus finds the records it updates.
	if err := saveState(r, targets); err != nil {
		t.Fatalf("seed state: %v", err)
	}
	return runUpTargets(context.Background(), r, targets, noopLogf)
}

func stateStatus(t *testing.T, r *resolved, slug string) string {
	t.Helper()
	recs, err := loadState(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, rec := range recs {
		if rec.Slug == slug {
			return rec.Status
		}
	}
	return "<missing>"
}

// TestRunUpTargets_RunsWorktreesInParallel proves up executes worktrees
// concurrently: with three worktrees each running two 250ms commands,
// serial execution would take ~1.5s; parallel finishes in ~0.5s and the
// probe must observe ≥2 Exec calls in flight at the same time.
func TestRunUpTargets_RunsWorktreesInParallel(t *testing.T) {
	delay := 250 * time.Millisecond
	probe := &upProbeRunner{execDelay: map[string]time.Duration{
		"a": delay, "b": delay, "c": delay,
	}}
	r := upProbeResolved(t, registerUpProbe(probe))
	targets := upProbeTargets(t, t.TempDir(), []string{"a", "b", "c"})

	start := time.Now()
	if err := runUpProbe(t, r, targets); err != nil {
		t.Fatalf("up: %v", err)
	}
	elapsed := time.Since(start)

	if max := probe.maxInFlight(); max < 2 {
		t.Errorf("expected ≥2 Exec calls to overlap (parallel execution), max concurrent = %d", max)
	}
	if got := probe.completed(); len(got) != 6 {
		t.Errorf("expected all 6 commands (setup+run × 3 worktrees) to complete, got %d: %v", len(got), got)
	}
	// Serial execution takes 3×(2×250ms)=1.5s; parallel takes ~0.5s.
	if elapsed > 1100*time.Millisecond {
		t.Errorf("up took %v; execution serialized instead of running in parallel", elapsed)
	}
	for _, slug := range []string{"a", "b", "c"} {
		if got := stateStatus(t, r, slug); got != ports.StatusRunning {
			t.Errorf("worktree %q status = %q, want %q", slug, got, ports.StatusRunning)
		}
	}
}

// TestRunUpTargets_WaitsForEveryCommand proves up does not return until
// every entry command (setup and run — the run entry is a project's final
// command, e.g. a health check) has fully finished: the slow worktree's
// commands must be recorded as completed, in order, before the operation
// returns, and the wall clock must span the slow worktree's runtime.
func TestRunUpTargets_WaitsForEveryCommand(t *testing.T) {
	probe := &upProbeRunner{execDelay: map[string]time.Duration{
		"fast": 10 * time.Millisecond,
		"slow": 300 * time.Millisecond,
	}}
	r := upProbeResolved(t, registerUpProbe(probe))
	targets := upProbeTargets(t, t.TempDir(), []string{"fast", "slow"})

	start := time.Now()
	if err := runUpProbe(t, r, targets); err != nil {
		t.Fatalf("up: %v", err)
	}
	elapsed := time.Since(start)

	done := probe.completed()
	if len(done) != 4 {
		t.Fatalf("expected 4 completed commands (setup+run × 2), got %d: %v", len(done), done)
	}
	slowSetup := indexOf(done, "slow:sh -c echo setup")
	slowRun := indexOf(done, "slow:sh -c echo run")
	if slowSetup < 0 || slowRun < 0 {
		t.Fatalf("slow worktree commands missing from completed ledger: %v", done)
	}
	if slowSetup > slowRun {
		t.Errorf("per-worktree commands out of order (setup must precede run): %v", done)
	}
	// The slow worktree alone takes 2×300ms; if up did not wait for its
	// commands the whole operation would return in tens of milliseconds.
	if elapsed < 500*time.Millisecond {
		t.Errorf("up returned in %v; it did not wait for the slow worktree's commands to finish", elapsed)
	}
}

// TestRunUpTargets_FailedWorktreeKeepsSiblingsRunning proves the plain
// WaitGroup semantics: one failing worktree marks that worktree failed but
// sibling worktrees run to completion and are marked running.
func TestRunUpTargets_FailedWorktreeKeepsSiblingsRunning(t *testing.T) {
	probe := &upProbeRunner{
		execDelay: map[string]time.Duration{"good": 150 * time.Millisecond},
		execErr:   map[string]error{"bad": errors.New("boom")},
	}
	r := upProbeResolved(t, registerUpProbe(probe))
	targets := upProbeTargets(t, t.TempDir(), []string{"bad", "good"})

	err := runUpProbe(t, r, targets)
	if err == nil {
		t.Fatal("expected joined error from the failing worktree")
	}
	if !strings.Contains(err.Error(), "bad") {
		t.Errorf("joined error should name the failing worktree, got: %v", err)
	}
	done := probe.completed()
	if countExecsFor(done, "good") != 2 {
		t.Errorf("sibling worktree must run to completion (both commands), got: %v", done)
	}
	if got := stateStatus(t, r, "good"); got != ports.StatusRunning {
		t.Errorf("sibling status = %q, want %q", got, ports.StatusRunning)
	}
	if got := stateStatus(t, r, "bad"); got != ports.StatusFailed {
		t.Errorf("failed worktree status = %q, want %q", got, ports.StatusFailed)
	}
}

func indexOf(list []string, want string) int {
	for i, s := range list {
		if s == want {
			return i
		}
	}
	return -1
}

func countExecsFor(list []string, slug string) int {
	n := 0
	for _, s := range list {
		if strings.HasPrefix(s, slug+":") {
			n++
		}
	}
	return n
}
