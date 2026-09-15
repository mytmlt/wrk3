package cmd

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/mytmlt/wrk3/internal/config"
	"github.com/mytmlt/wrk3/internal/ports"
	"github.com/mytmlt/wrk3/internal/runner"
)

// syncProbeTimeout bounds each live probe inside a sync pass. liveStatus
// applies its own 30s timeout per worktree; the pass runs probes in
// parallel like the dashboard so ls/status stay usable with many worktrees.
const syncProbeTimeout = 30 * time.Second

// applyLiveStatuses folds live probe results into records, returning the
// updated records plus the branches whose persisted status changes. It is
// pure (no docker, no disk) so any Runner backend — docker today, other
// orchestrators tomorrow — syncs through the same rule. Unknown probes
// never change state; live running always wins (even over setting
// up/stopping/failed); live stopped only clears running/failed.
func applyLiveStatuses(recs []ports.WorktreeRecord, live map[string]runner.Status) ([]ports.WorktreeRecord, []string) {
	updated := append([]ports.WorktreeRecord(nil), recs...)
	var changed []string
	for i := range updated {
		st, ok := live[updated[i].Branch]
		if !ok || st.State == runner.StateUnknown || st.State == "" {
			continue
		}
		if ports.ShouldPersistLive(updated[i].Status, string(st.State)) {
			updated[i].Status = string(st.State)
			changed = append(changed, updated[i].Branch)
		}
	}
	sort.Strings(changed)
	return updated, changed
}

// probeLiveStatuses queries the configured Runner backend for every record
// with an existing directory. Missing dirs (stale) and probe errors are
// skipped — errors surface as unknown in display and must not clobber
// stored state. Main records have no state entry; callers sync only managed
// records and synthesize main afterwards.
func probeLiveStatuses(cfg *config.Config, recs []ports.WorktreeRecord) map[string]runner.Status {
	out := make(map[string]runner.Status, len(recs))
	type result struct {
		branch string
		status runner.Status
		ok     bool
	}
	results := make([]result, len(recs))
	g, _ := errgroup.WithContext(context.Background())
	for i, rec := range recs {
		i, rec := i, rec
		g.Go(func() error {
			if _, err := os.Stat(rec.AbsPath); err != nil {
				return nil
			}
			rn, err := newRunner(cfg, rec.Slug)
			if err != nil {
				return nil
			}
			ctx, cancel := context.WithTimeout(context.Background(), syncProbeTimeout)
			defer cancel()
			st, err := rn.Status(ctx, rec.AbsPath)
			if err != nil {
				return nil
			}
			results[i] = result{branch: rec.Branch, status: st, ok: true}
			return nil
		})
	}
	_ = g.Wait()
	for _, res := range results {
		if res.ok {
			out[res.branch] = res.status
		}
	}
	return out
}

// syncRuntimeAndSave probes the live runtime via the Runner interface and
// persists running/stopped drift back to the state file. It saves only when
// something changed and skips the save for display-only (empty) state paths.
// Returned changed lists affected branches for CLI reporting.
func syncRuntimeAndSave(r *resolved, recs []ports.WorktreeRecord) ([]ports.WorktreeRecord, []string, error) {
	if len(recs) == 0 {
		return recs, nil, nil
	}
	updated, changed := applyLiveStatuses(recs, probeLiveStatuses(r.cfg, recs))
	if len(changed) == 0 {
		return updated, nil, nil
	}
	if r.stateP == "" {
		return updated, changed, nil
	}
	if err := ports.Save(r.stateP, updated); err != nil {
		return nil, nil, fmt.Errorf("save synced runtime state: %w", err)
	}
	return updated, changed, nil
}

// logSyncedCLI reports runtime syncs on stderr, mirroring the reconcile log
// so `ls`/`status` stay honest about state-file writes.
func logSyncedCLI(changed []string) {
	if len(changed) > 0 {
		_, _ = fmt.Fprintf(os.Stderr, "synced runtime state: %s\n", strings.Join(changed, ", "))
	}
}
