// Package health probes user-configured shell checks plus compose
// container health and aggregates them into a Docker-style worktree
// health state (healthy / degraded / unhealthy / unknown).
//
// Checks run display-only: failures never fail `up` and never persist
// to the state file. Callers probe only when the lifecycle is running
// and append Suffix to the STATUS cell (e.g. "running (healthy)").
package health

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
)

// maxProbeOutput caps captured check output in memory (bytes). Reports
// keep the first 500 runes; the cap only bounds what a verbose check can
// accumulate while running.
const maxProbeOutput = 64 * 1024

// cappedBuffer is an io.Writer that keeps the first maxProbeOutput bytes
// and discards the rest, so a chatty check cannot grow memory without
// bound.
type cappedBuffer struct {
	buf []byte
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if len(b.buf) < maxProbeOutput {
		if room := maxProbeOutput - len(b.buf); len(p) > room {
			p = p[:room]
		}
		b.buf = append(b.buf, p...)
	}
	return n, nil
}

func (b *cappedBuffer) String() string { return string(b.buf) }

// States for Report.State (see Aggregate).
const (
	StateHealthy   = "healthy"
	StateDegraded  = "degraded"
	StateUnhealthy = "unhealthy"
	StateUnknown   = "unknown"
)

// DefaultTimeout bounds one shell check when none is set.
const DefaultTimeout = 10 * time.Second

// Check is one user-configured shell probe. Run executes verbatim via
// `sh -c` with cwd=worktree and env=allocated ports (like entry.*).
type Check struct {
	Name string
	Run  string
	// Timeout bounds this check. Zero means DefaultTimeout.
	Timeout time.Duration
}

// Result is the outcome of one check (shell or container).
type Result struct {
	Name string
	// Healthy reports pass/fail. Probe errors set Healthy=false with Err.
	Healthy bool
	Output  string
	Err     error
}

// Report aggregates results across one worktree.
type Report struct {
	// State is healthy/degraded/unhealthy/unknown, or "" when Total==0
	// (no checks and no container health data: caller shows no suffix).
	State   string
	Passing int
	Total   int
	Results []Result
}

// Aggregate folds results into a report. Empty input yields a zero report
// (State "" so callers render a bare lifecycle). A single failing check
// is unhealthy (not degraded); mixed pass/fail is degraded; probe errors
// count as failures.
func Aggregate(results []Result) Report {
	if len(results) == 0 {
		return Report{}
	}
	sorted := append([]Result(nil), results...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	passing := 0
	for _, r := range sorted {
		if r.Healthy {
			passing++
		}
	}
	rep := Report{Passing: passing, Total: len(sorted), Results: sorted}
	switch {
	case passing == len(sorted):
		rep.State = StateHealthy
	case passing == 0:
		rep.State = StateUnhealthy
	default:
		rep.State = StateDegraded
	}
	return rep
}

// Suffix renders the STATUS suffix for a report: "" when Total==0,
// otherwise " (healthy)", " (degraded 1/2)", " (unhealthy)" or
// " (unknown)". Unknown is produced by ProbeUnknown when the probe
// itself errored (e.g. engine unreachable) as opposed to checks failing.
func (r Report) Suffix() string {
	switch r.State {
	case StateHealthy:
		return " (healthy)"
	case StateDegraded:
		return fmt.Sprintf(" (degraded %d/%d)", r.Passing, r.Total)
	case StateUnhealthy:
		return " (unhealthy)"
	case StateUnknown:
		return " (unknown)"
	default:
		return ""
	}
}

// ProbeUnknown returns a report rendering as " (unknown)" for probe-level
// errors (timeouts, engine failures) as opposed to check failures.
func ProbeUnknown() Report {
	return Report{State: StateUnknown}
}

// WithSuffix appends the health suffix to a lifecycle string. Empty
// reports (no checks) return the lifecycle unchanged.
func WithSuffix(lifecycle string, rep Report) string {
	return lifecycle + rep.Suffix()
}

// Summary renders one per-check line for the dashboard DETAILS preview
// (e.g. "api: pass", "db: fail: exit status 1").
func (r Report) Summary() string {
	if r.Total == 0 && r.State == "" {
		return "no checks"
	}
	if r.State == StateUnknown {
		return "unknown"
	}
	parts := make([]string, 0, len(r.Results))
	for _, res := range r.Results {
		if res.Healthy {
			parts = append(parts, res.Name+": pass")
			continue
		}
		if res.Err != nil {
			parts = append(parts, res.Name+": fail: "+shortErr(res.Err))
			continue
		}
		parts = append(parts, res.Name+": fail")
	}
	return strings.Join(parts, ", ")
}

func shortErr(err error) string {
	return truncateRunes(strings.TrimSpace(err.Error()), 120)
}

// truncateRunes caps s at n runes so multi-byte UTF-8 is never split.
func truncateRunes(s string, n int) string {
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// ProbeShell runs checks via `sh -c` with cwd=worktreePath and env applied,
// in parallel, each bounded by its timeout (DefaultTimeout when unset).
// An empty check list yields a zero report. Missing/blank Run strings are
// skipped (validation rejects them in config; direct callers stay safe).
func ProbeShell(ctx context.Context, worktreePath string, env map[string]string, checks []Check) Report {
	var active []Check
	for _, c := range checks {
		if strings.TrimSpace(c.Run) == "" {
			continue
		}
		active = append(active, c)
	}
	if len(active) == 0 {
		return Report{}
	}
	results := make([]Result, len(active))
	var wg sync.WaitGroup
	for i, c := range active {
		i, c := i, c
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = runOne(ctx, worktreePath, env, c)
		}()
	}
	wg.Wait()
	return Aggregate(results)
}

func runOne(ctx context.Context, dir string, env map[string]string, c Check) Result {
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.Command("sh", "-c", c.Run)
	cmd.Dir = dir
	// Children inherit the OS environment (PATH/HOME/etc.) with the
	// allocated-port vars overlaid, mirroring the runner's
	// buildEnv(os.Environ(), ...) behavior for entry.* commands.
	cmd.Env = mergeEnv(os.Environ(), env)
	// Output is capped: a chatty check must not grow memory without
	// bound while it runs (truncated to 500 runes for the report below).
	var stdout, stderr cappedBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := startKillable(tctx, cmd)
	out := truncateRunes(strings.TrimSpace(stdout.String()+stderr.String()), 500)
	if err != nil {
		return Result{Name: c.Name, Healthy: false, Output: out, Err: err}
	}
	return Result{Name: c.Name, Healthy: true, Output: out}
}

// mergeEnv overlays extra onto base, replacing existing keys in place and
// appending new keys sorted for determinism. Malformed keys are skipped.
func mergeEnv(base []string, extra map[string]string) []string {
	merged := append([]string(nil), base...)
	index := map[string]int{}
	for i, kv := range merged {
		k := kv
		if j := strings.IndexByte(kv, '='); j >= 0 {
			k = kv[:j]
		}
		if _, ok := index[k]; !ok {
			index[k] = i
		}
	}
	keys := make([]string, 0, len(extra))
	for k := range extra {
		if k == "" || strings.Contains(k, "=") {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		kv := k + "=" + extra[k]
		if j, ok := index[k]; ok {
			merged[j] = kv
			continue
		}
		index[k] = len(merged)
		merged = append(merged, kv)
	}
	return merged
}

// Merge combines shell and container results into one report.
func Merge(shell, containers []Result) Report {
	all := make([]Result, 0, len(shell)+len(containers))
	all = append(all, shell...)
	all = append(all, containers...)
	return Aggregate(all)
}
