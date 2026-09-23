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

// reconcileState adopts on-disk worktrees missing from state (orphans from
// a deleted state file or out-of-band `git worktree add`). Orphans are
// processed in sorted branch order for deterministic ports. Ports are
// recovered from the worktree .env when the recovered set matches the
// configured base keys, sits inside ranges, and collides with neither
// state nor the OS; otherwise the lowest free range allocation is
// assigned (gap reuse, OS-aware). URL ports are similarly recovered from
// the .env when the recovered values match configured URL specs; otherwise
// a fresh URL allocation is made sharing the taken set with host ports.
// The worktree .env is ensured via ensureWorktreeEnv (managed port keys
// overwritten to the allocation). Records are appended with Status stopped
// — display overlays the live runner probe. Slug, port, and
// compose-project collisions are hard errors: state is never half-written
// (callers save only on success).
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
	mainPorts := alloc.BaseAllocation()
	taken := takenWithMain(alloc, recs)
	// Seed URL ports like host ports: existing records hold theirs, and
	// the implicit main checkout owns exactly the base-URL allocation.
	for p := range ports.TakenFromURLRecords(recs) {
		taken[p] = struct{}{}
	}
	for _, p := range r.cfg.URLBasePorts() {
		taken[p] = struct{}{}
	}

	all := append([]ports.WorktreeRecord(nil), recs...)

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

		urlSpecs := r.cfg.URLSpecs()

		idx := nextIndex(all)
		var allocation map[string]int
		var urlAlloc map[string]int
		if recovered, ok := ports.ReadPorts(path, base); ok {
			if recoveredReusable(alloc, base, recovered, taken, mainPorts) {
				allocation = recovered
			} else {
				warns = append(warns, fmt.Sprintf("worktree %q .env ports cannot be adopted; allocating a free set", branch))
			}
		}
		if allocation == nil {
			fresh, err := alloc.FindFreeAllocation(taken, osPortFree)
			if err != nil {
				return recs, nil, false, fmt.Errorf("reconcile worktree %q: %w", branch, err)
			}
			allocation = fresh
		}
		for _, p := range allocation {
			taken[p] = struct{}{}
		}
		if len(urlSpecs) > 0 {
			tracked := alloc.HostPortsByBase(allocation)
			if recovered, ok := ports.ReadURLs(path, urlSpecs); ok {
				if urlRecoveredReusable(recovered, taken, urlSpecs, tracked) {
					urlAlloc = recovered
				} else {
					warns = append(warns, fmt.Sprintf("worktree %q .env URL ports cannot be adopted; allocating a free set", branch))
				}
			}
			if urlAlloc == nil {
				fresh, err := ports.AllocateURLs(urlSpecs, tracked, taken, osPortFree)
				if err != nil {
					return recs, nil, false, fmt.Errorf("reconcile worktree %q urls: %w", branch, err)
				}
				urlAlloc = fresh
			}
			for _, p := range urlAlloc {
				taken[p] = struct{}{}
			}
		}

		rec := ports.WorktreeRecord{
			Branch:         branch,
			Slug:           slug,
			AbsPath:        path,
			Index:          idx,
			Ports:          allocation,
			Urls:           urlAlloc,
			ComposeProject: composeProject,
			Status:         ports.StatusStopped,
		}
		if err := ensureWorktreeEnv(r, rec, urlSpecs); err != nil {
			return recs, nil, false, err
		}
		all = append(all, rec)
		for _, p := range allocation {
			taken[p] = struct{}{}
		}
	}
	if len(all) == len(recs) {
		return recs, nil, false, nil
	}
	return all, warns, true, nil
}

// collidesTaken reports whether p shares any host port with taken.
func collidesTaken(taken map[int]struct{}, p map[string]int) bool {
	for _, v := range p {
		if _, ok := taken[v]; ok {
			return true
		}
	}
	return false
}

// recoveredReusable reports whether .env-recovered ports can be reused:
// same keys as base, each value inside its range, alias-consistent values
// (names sharing one base value must share one recovered value; distinct
// base values must stay distinct), no collision with main
// or taken state, and OS-free (bind probe).
func recoveredReusable(alloc ports.Allocator, base, recovered map[string]int, taken map[int]struct{}, mainPorts map[string]int) bool {
	if len(recovered) != len(base) {
		return false
	}
	for k := range base {
		if _, ok := recovered[k]; !ok {
			return false
		}
	}
	ranges := alloc.Ranges
	if ranges == nil {
		ranges = ports.DefaultRanges()
	}
	for name, v := range recovered {
		r, ok := ranges[name]
		if !ok {
			return false
		}
		if v < r[0] || v > r[1] {
			return false
		}
		if _, ok := base[name]; !ok {
			return false
		}
	}
	if ports.AllocationsCollide(mainPorts, recovered) || collidesTaken(taken, recovered) {
		return false
	}
	// Alias consistency: names sharing one base value share one host port,
	// so their recovered values must match; distinct base values must map
	// to distinct host ports.
	aliasOf := make(map[string]int, len(recovered))
	if base != nil {
		for name := range recovered {
			if b, ok := base[name]; ok {
				aliasOf[name] = b
			} else {
				aliasOf[name] = -1
			}
		}
	}
	seen := make(map[int]string, len(recovered))
	for name, v := range recovered {
		if other, dup := seen[v]; dup {
			if base == nil || aliasOf[name] != aliasOf[other] {
				return false
			}
			continue
		}
		seen[v] = name
	}
	if base != nil {
		byBase := make(map[int]int, len(recovered))
		for name, v := range recovered {
			b := aliasOf[name]
			if prev, ok := byBase[b]; ok {
				if prev != v {
					return false
				}
				continue
			}
			byBase[b] = v
		}
	}
	for _, v := range recovered {
		if !osPortFree(v) {
			return false
		}
	}
	return true
}

// urlRecoveredReusable reports whether .env-recovered URL ports can be
// reused: no collision with taken ports, tracked values equal the host
// allocation they follow (vars whose base port matches a host base must
// name that tracked port), alias-consistent values otherwise (vars
// sharing one base port must share one recovered value; distinct base
// ports must stay distinct), and OS-free.
func urlRecoveredReusable(recovered map[string]int, taken map[int]struct{}, specs []ports.URLSpec, tracked map[int]int) bool {
	if len(recovered) == 0 {
		return false
	}
	baseOf := make(map[string]int, len(specs))
	for _, s := range specs {
		baseOf[s.Var] = s.BasePort()
	}
	// Tracked values name the worktree's own host port (already validated
	// against taken when the host allocation was recovered or assigned),
	// so only untracked values collide-check against taken.
	untracked := make(map[string]int, len(recovered))
	for v, p := range recovered {
		b, ok := baseOf[v]
		if !ok {
			return false
		}
		if tp, ok := tracked[b]; ok {
			if p != tp {
				return false
			}
			continue
		}
		untracked[v] = p
	}
	if collidesTaken(taken, untracked) {
		return false
	}
	seen := make(map[int]string, len(recovered))
	for v, p := range recovered {
		if other, dup := seen[p]; dup {
			bv, okV := baseOf[v]
			bo, okO := baseOf[other]
			if !okV || !okO || bv != bo {
				return false
			}
			continue
		}
		seen[p] = v
	}
	byBase := make(map[int]int, len(recovered))
	for v, p := range recovered {
		b, ok := baseOf[v]
		if !ok {
			return false
		}
		if prev, dup := byBase[b]; dup {
			if prev != p {
				return false
			}
			continue
		}
		byBase[b] = p
	}
	for _, v := range recovered {
		if !osPortFree(v) {
			return false
		}
	}
	return true
}

// reconcileAndSave adopts orphan worktrees into recs, migrates legacy
// managed allocations colliding with main (the ports.base allocation),
// and persists when anything changed. It returns the updated records,
// the adopted branch names, and warnings (including recovered .env
// allocations that cannot be adopted). A missing
// stateP skips the save and returns display-only records.
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
		warns = append(warns, "migrated legacy collides with main (ports.base): "+strings.Join(moved, ", "))
	}
	urlRecs, urlMoved, urlWarns, err := migrateTrackedURLs(r, updated)
	if err != nil {
		return nil, nil, nil, err
	}
	warns = append(warns, urlWarns...)
	if len(urlMoved) > 0 {
		updated = urlRecs
		dirty = true
		warns = append(warns, "migrated diverged URL ports to tracked host ports: "+strings.Join(urlMoved, ", "))
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

// logReconciledCLI reports adoptions and reconcile warnings on
// stderr for CLI reads (status/ls/up/down) that auto-reconcile state.
func logReconciledCLI(adopted, warns []string) {
	if len(adopted) > 0 {
		_, _ = fmt.Fprintf(os.Stderr, "reconciled state: adopted %s\n", strings.Join(adopted, ", "))
	}
	warnReconciled(warns)
}

// warnReconciled prints reconcile warnings (including recovered .env
// allocations that cannot be adopted). After a fresh allocation, ensure
// overwrites managed port keys to match.
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
