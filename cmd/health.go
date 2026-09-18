package cmd

import (
	"context"
	"os"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/mytmlt/wrk3/internal/config"
	"github.com/mytmlt/wrk3/internal/health"
	"github.com/mytmlt/wrk3/internal/ports"
	"github.com/mytmlt/wrk3/internal/runner"
)

// healthProbeTimeout bounds the container-health phase per worktree.
// The shell phase budget derives from the configured checks (longest
// per-check timeout plus headroom) so a configured timeout is always
// honored instead of being cut off by a fixed phase deadline.
const healthProbeTimeout = 30 * time.Second

// shellPhaseHeadroom is added to the longest per-check timeout for the
// shell phase budget (process spawn + group-kill propagation).
const shellPhaseHeadroom = 5 * time.Second

// healthReportFor probes shell checks plus compose container health for a
// running worktree and returns the merged report. Display-only: probe
// errors contribute nothing (never an error), unsupported runners yield
// no container data, and empty results (no checks, no container health)
// yield a zero report so callers show no suffix.
func healthReportFor(cfg *config.Config, rec ports.WorktreeRecord) health.Report {
	if cfg == nil {
		return health.Report{}
	}
	var shellChecks []health.Check
	for _, c := range cfg.Health.Checks {
		shellChecks = append(shellChecks, health.Check{
			Name:    c.Name,
			Run:     c.Run,
			Timeout: c.EffectiveTimeout(),
		})
	}
	shellCtx, shellCancel := context.WithTimeout(context.Background(), shellPhaseBudget(shellChecks))
	shellRep := health.ProbeShell(shellCtx, rec.AbsPath, envForWorktree(cfg, rec), shellChecks)
	shellCancel()

	// Container health is best-effort: query errors (daemon down,
	// unsupported runner) contribute nothing rather than an unknown
	// suffix, so stacks without health data keep their bare lifecycle.
	var containerRes []health.Result
	if rn, err := newRunner(cfg, rec.Slug); err == nil {
		contCtx, contCancel := context.WithTimeout(context.Background(), healthProbeTimeout)
		if hs, err := runner.ContainerHealths(contCtx, rn, rec.AbsPath); err == nil {
			containerRes = containerResults(hs)
		}
		contCancel()
	}

	if shellRep.Total == 0 && len(containerRes) == 0 {
		return health.Report{}
	}
	return health.Merge(shellRep.Results, containerRes)
}

// shellPhaseBudget returns the longest per-check timeout plus headroom
// (defaulting to the longest default when no checks are configured, so
// the budget stays bounded and a configured timeout is never cut off by
// a fixed phase deadline).
func shellPhaseBudget(checks []health.Check) time.Duration {
	longest := health.DefaultTimeout
	for _, c := range checks {
		eff := c.Timeout
		if eff <= 0 {
			eff = health.DefaultTimeout
		}
		if eff > longest {
			longest = eff
		}
	}
	return longest + shellPhaseHeadroom
}

// statusCell is one display row for status/ls (parallel probing keeps
// `status` latency near a single worktree budget, like the dashboard).
type statusCell struct {
	slug     string
	branch   string
	status   string
	ports    string
	project  string
}

// probeStatusCells resolves display cells for recs in parallel, preserving
// input order. Missing dirs short-circuit to stale/? without runner calls.
func probeStatusCells(cfg *config.Config, recs []ports.WorktreeRecord) []statusCell {
	cells := make([]statusCell, len(recs))
	g, _ := errgroup.WithContext(context.Background())
	for i, rec := range recs {
		i, rec := i, rec
		g.Go(func() error {
			if _, err := os.Stat(rec.AbsPath); err != nil {
				cells[i] = statusCell{slug: rec.Slug, branch: rec.Branch, status: "stale", ports: "?", project: rec.ComposeProject}
				return nil
			}
			portText := portsCell(rec.Ports)
			st, err := liveStatus(cfg, rec)
			lifecycle := ports.ResolveDisplayStatus(rec.Status, st, err)
			cells[i] = statusCell{
				slug:    rec.Slug,
				branch:  rec.Branch,
				status:  withHealthSuffix(cfg, rec, lifecycle),
				ports:   portText,
				project: rec.ComposeProject,
			}
			return nil
		})
	}
	_ = g.Wait()
	return cells
}

// containerResults converts engine health into check results. Containers
// without a healthcheck ("none") contribute nothing; "starting" counts
// as not-yet-healthy (fail) so a starting stack shows degraded/unhealthy
// instead of a premature healthy.
func containerResults(hs []runner.ContainerHealth) []health.Result {
	var out []health.Result
	for _, h := range hs {
		if !h.HasHealth() {
			continue
		}
		name := h.Name
		if name == "" {
			name = h.ID
		}
		out = append(out, health.Result{
			Name:    "container:" + name,
			Healthy: h.Status == "healthy",
		})
	}
	return out
}

// withHealthSuffix appends the live health suffix to a display lifecycle.
// Non-running lifecycles pass through untouched (never probed).
func withHealthSuffix(cfg *config.Config, rec ports.WorktreeRecord, lifecycle string) string {
	if lifecycle != ports.StatusRunning {
		return lifecycle
	}
	return health.WithSuffix(lifecycle, healthReportFor(cfg, rec))
}
