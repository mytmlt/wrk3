package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/mytmlt/wrk3/internal/ports"
	"github.com/mytmlt/wrk3/internal/source"
	"github.com/mytmlt/wrk3/internal/syslog"
)

// maxReconcileTries bounds the fresh-index scan when recovered or
// next-available allocations collide with taken ports (e.g. after a
// ports.base/step change). Hitting it means the port space is exhausted.
const maxReconcileTries = 10000

// reconcileState adopts on-disk worktrees missing from state (orphans from
// a deleted state file or out-of-band `git worktree add`). Orphans are
// processed in sorted branch order for deterministic indexes. Ports are
// recovered from the worktree .env when the recovered set is a complete,
// valid allocation point with no collisions; otherwise a fresh
// next-available index is assigned. The worktree .env is gap-filled via
// ensureWorktreeEnv (existing values never overwritten; divergences come
// back as warnings). Records are appended with Status stopped — display
// overlays the live runner probe. Slug, port, and compose-project
// collisions are hard errors: state is never half-written (callers save
// only on success).
//
// A git List failure degrades to no adoption (offline-safe, like
// mainRecord) rather than failing the read.
func reconcileState(r *resolved, recs []ports.WorktreeRecord) (updated []ports.WorktreeRecord, warns []string, dirty bool, err error) {
	infos, err := r.src.List(r.cfg.RepoPath())
	if err != nil {
		return recs, nil, false, nil
	}
	byBranch := localWorktreesByBranch(r, infos)
	if len(byBranch) == 0 {
		return recs, nil, false, nil
	}
	branches := make([]string, 0, len(byBranch))
	for branch := range byBranch {
		branches = append(branches, branch)
	}
	sort.Strings(branches)

	alloc := r.cfg.Allocator()
	base := r.cfg.Ports.Base
	if base == nil {
		base = ports.DefaultBase()
	}
	mainPorts := alloc.Allocate(mainWorktreeIndex).Ports
	taken := make([]map[string]int, 0, len(recs)+1)
	for _, rec := range recs {
		taken = append(taken, rec.Ports)
	}

	all := append([]ports.WorktreeRecord(nil), recs...)
	collides := func(p map[string]int) bool {
		if ports.AllocationsCollide(mainPorts, p) {
			return true
		}
		for _, t := range taken {
			if ports.AllocationsCollide(t, p) {
				return true
			}
		}
		return false
	}

	for _, branch := range branches {
		if findRecord(all, branch) != nil {
			continue
		}
		slug := source.Slugify(branch)
		if existing := findRecord(all, slug); existing != nil && existing.Branch != branch {
			return recs, nil, false, fmt.Errorf("reconcile worktree %q: slug %q collides with branch %q", branch, slug, existing.Branch)
		}
		composeProject := r.cfg.ComposeOptions(slug).ProjectName()
		for _, rec := range all {
			if rec.ComposeProject == composeProject {
				return recs, nil, false, fmt.Errorf("reconcile worktree %q: compose project %q collides with worktree %q", branch, composeProject, rec.Branch)
			}
		}
		path := byBranch[branch]

		// -1 is the unassigned sentinel here (not a port index):
		// allocationIndex never returns mainWorktreeIndex and
		// nextIndex floors at 1, so adopted worktrees never take
		// the main slot.
		idx := -1
		var allocation ports.Allocation
		if recovered, ok := ports.ReadPorts(path, base); ok {
			if k, valid := allocationIndex(alloc, base, recovered); valid {
				candidate := alloc.Allocate(k)
				if portsEqual(candidate.Ports, recovered) && !collides(recovered) {
					idx = k
					allocation = ports.Allocation{Index: k, Ports: recovered}
				}
			}
		}
		if idx < 0 {
			idx = nextIndex(all)
			for tries := 0; tries < maxReconcileTries; tries++ {
				candidate := alloc.Allocate(idx)
				if !collides(candidate.Ports) {
					allocation = candidate
					break
				}
				idx++
			}
			if allocation.Ports == nil {
				return recs, nil, false, fmt.Errorf("reconcile worktree %q: no collision-free port index available", branch)
			}
		}

		rec := ports.WorktreeRecord{
			Branch:         branch,
			Slug:           slug,
			AbsPath:        path,
			Index:          idx,
			Ports:          allocation.Ports,
			ComposeProject: composeProject,
			Status:         ports.StatusStopped,
		}
		w, err := ensureWorktreeEnv(r, rec)
		if err != nil {
			return recs, nil, false, err
		}
		warns = append(warns, w...)
		all = append(all, rec)
		taken = append(taken, allocation.Ports)
	}
	if len(all) == len(recs) {
		return recs, nil, false, nil
	}
	return all, warns, true, nil
}

// reconcileAndSave adopts orphan worktrees into recs, migrates legacy
// managed index 0 allocations colliding with main (reserved index 0, the
// ports.base allocation), and persists when anything changed. It returns
// the updated records, the adopted branch names, and .env divergence
// warnings. A missing stateP skips the save and returns display-only
// records.
func reconcileAndSave(r *resolved, recs []ports.WorktreeRecord) (updated []ports.WorktreeRecord, adopted []string, warns []string, err error) {
	had := make(map[string]struct{}, len(recs))
	for _, rec := range recs {
		had[rec.Branch] = struct{}{}
	}
	updated, warns, dirty, err := reconcileState(r, recs)
	if err != nil {
		return nil, nil, nil, err
	}
	migratedRecs, moved, migrateWarns, err := migrateLegacyMainCollisions(r, updated)
	if err != nil {
		return nil, nil, nil, err
	}
	warns = append(warns, migrateWarns...)
	if len(moved) > 0 {
		updated = migratedRecs
		dirty = true
		warns = append(warns, "migrated legacy index 0 collides with main (ports.base): "+strings.Join(moved, ", "))
	}
	if !dirty {
		return updated, nil, warns, nil
	}
	for _, rec := range updated {
		if _, ok := had[rec.Branch]; !ok {
			adopted = append(adopted, rec.Branch)
		}
	}
	if r.stateP == "" {
		return updated, adopted, warns, nil
	}
	if err := ports.Save(r.stateP, updated); err != nil {
		return nil, nil, nil, fmt.Errorf("save reconciled state: %w", err)
	}
	if len(adopted) > 0 {
		r.oplog(syslog.Entry{Level: syslog.LevelInfo, Op: "reconcile", Msg: "reconciled state: adopted " + strings.Join(adopted, ", ")})
	}
	return updated, adopted, warns, nil
}

// logReconciledCLI reports adoptions and .env divergence warnings on
// stderr for CLI reads (status/ls/up/down) that auto-reconcile state.
func logReconciledCLI(adopted, warns []string) {
	if len(adopted) > 0 {
		_, _ = fmt.Fprintf(os.Stderr, "reconciled state: adopted %s\n", strings.Join(adopted, ", "))
	}
	warnReconciled(warns)
}

// allocationIndex resolves recovered .env ports to their allocator index:
// the app port must sit exactly on the base+index*step grid (effective step
// honors the allocator default). Callers additionally verify the full map
// equals Allocate(index) so multi-port bases match on every name.
func allocationIndex(alloc ports.Allocator, base map[string]int, recovered map[string]int) (int, bool) {
	baseApp, ok := base[ports.PortApp]
	if !ok {
		return 0, false
	}
	appValue, ok := recovered[ports.PortApp]
	if !ok {
		return 0, false
	}
	step := alloc.Step
	if step == 0 {
		step = ports.DefaultStep
	}
	if step <= 0 {
		return 0, false
	}
	delta := appValue - baseApp
	if delta < 0 || delta%step != 0 {
		return 0, false
	}
	idx := delta / step
	if idx == mainWorktreeIndex {
		return 0, false
	}
	return idx, true
}

// portsEqual reports whether two allocations hold identical name=value pairs.
func portsEqual(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
}

// warnReconciled prints .env divergence warnings from reconciliation.
// Adopted allocations keep existing .env values intact; the warnings tell
// the user which keys differ from the adopted allocation.
func warnReconciled(warns []string) {
	for _, w := range warns {
		_, _ = fmt.Fprintf(os.Stderr, "warning: %s\n", w)
	}
}

// unionBranches merges two branch lists into a sorted, deduplicated union.
func unionBranches(a, b []string) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	var out []string
	for _, name := range a {
		if _, ok := seen[name]; !ok {
			seen[name] = struct{}{}
			out = append(out, name)
		}
	}
	for _, name := range b {
		if _, ok := seen[name]; !ok {
			seen[name] = struct{}{}
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}
