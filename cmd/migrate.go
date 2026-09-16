package cmd

import (
	"fmt"
	"os"
	"sort"

	"github.com/mytmlt/wrk3/internal/ports"
)

// nextFreeIndex returns the first managed index >= nextIndex(recs) whose
// allocation collides with neither main (reserved index 0, ports.base)
// nor any existing record. It bounds the scan with maxReconcileTries;
// ok=false means the port space is exhausted.
func nextFreeIndex(alloc ports.Allocator, recs []ports.WorktreeRecord) (idx int, ok bool) {
	mainPorts := alloc.Allocate(mainWorktreeIndex).Ports
	taken := make([]map[string]int, 0, len(recs)+1)
	taken = append(taken, mainPorts)
	for _, rec := range recs {
		taken = append(taken, rec.Ports)
	}
	idx = nextIndex(recs)
	for tries := 0; tries < maxReconcileTries; tries++ {
		candidate := alloc.Allocate(idx)
		collides := false
		for _, t := range taken {
			if len(t) == 0 {
				continue
			}
			if ports.AllocationsCollide(candidate.Ports, t) {
				collides = true
				break
			}
		}
		if !collides {
			return idx, true
		}
		idx++
	}
	return 0, false
}

// migrateLegacyMainCollisions reassigns managed records that collide with
// the implicit main checkout (reserved index 0, exactly the ports.base
// allocation) to the next collision-free managed index (>= 1).
//
// Pre-main-at-base state files may still hold a managed index 0
// allocation (e.g. app=8000 with defaults). Since main now owns those
// ports, every recordsWithMain call used to hard-error and block
// status/pull/dashboard. Migration instead bumps the colliding record to
// base+step onwards, preserving the user's desired layout:
//
//	main == ports.base, managed == base+step, base+2*step, ...
//
// Records are processed in sorted branch order for deterministic indexes.
// Existing non-colliding indexes are never renumbered. The worktree .env
// is gap-filled (existing values never overwritten); when the on-disk
// .env still holds the old ports a divergence warning is returned so the
// caller can report it — the state file (runner env source) is fixed
// regardless and takes effect on next up/reload.
func migrateLegacyMainCollisions(r *resolved, recs []ports.WorktreeRecord) (updated []ports.WorktreeRecord, migrated []string, warns []string, err error) {
	if r == nil || r.cfg == nil {
		return recs, nil, nil, nil
	}
	alloc := r.cfg.Allocator()
	mainPorts := alloc.Allocate(mainWorktreeIndex).Ports

	collidesWithMain := func(rec ports.WorktreeRecord) bool {
		return alloc.IndexesCollide(mainWorktreeIndex, rec.Index) ||
			ports.AllocationsCollide(mainPorts, rec.Ports)
	}

	var colliding []int
	for i, rec := range recs {
		if collidesWithMain(rec) {
			colliding = append(colliding, i)
		}
	}
	if len(colliding) == 0 {
		return recs, nil, nil, nil
	}
	sort.Slice(colliding, func(a, b int) bool {
		return recs[colliding[a]].Branch < recs[colliding[b]].Branch
	})

	out := append([]ports.WorktreeRecord(nil), recs...)
	// taken tracks all allocations that must stay unique: main plus every
	// managed record (including ones about to move, so two legacy zeros
	// can never land on the same fresh slot).
	taken := make([]map[string]int, 0, len(out)+1)
	taken = append(taken, mainPorts)
	for _, rec := range out {
		taken = append(taken, rec.Ports)
	}
	collidesTaken := func(p map[string]int, skip map[string]int) bool {
		if ports.AllocationsCollide(mainPorts, p) {
			return true
		}
		for _, t := range taken {
			if len(t) == 0 {
				continue
			}
			// Skip the record's own current allocation: it is being
			// replaced, so it must not block its own fresh slot scan
			// except via mainPorts (checked above).
			if skip != nil && portsEqual(t, skip) {
				continue
			}
			if ports.AllocationsCollide(t, p) {
				return true
			}
		}
		return false
	}

	for _, ci := range colliding {
		old := out[ci].Ports
		idx := nextIndex(out)
		var allocation ports.Allocation
		for tries := 0; tries < maxReconcileTries; tries++ {
			candidate := alloc.Allocate(idx)
			if !collidesTaken(candidate.Ports, old) {
				allocation = candidate
				break
			}
			idx++
		}
		if allocation.Ports == nil {
			return recs, nil, nil, fmt.Errorf("migrate worktree %q: no collision-free port index available", out[ci].Branch)
		}
		out[ci].Index = allocation.Index
		out[ci].Ports = allocation.Ports
		taken = append(taken, allocation.Ports)
		migrated = append(migrated, out[ci].Branch)
		warns = append(warns, fmt.Sprintf(
			"migrated worktree %q from colliding ports to index %d (main reserves index %d, the ports.base allocation); .env divergence warnings below are advisory",
			out[ci].Branch, allocation.Index, mainWorktreeIndex))
		// Gap-fill .env only when the worktree dir exists: stale records
		// (dir missing) still migrate state, but must not create dirs.
		if st, statErr := os.Stat(out[ci].AbsPath); statErr == nil && st.IsDir() {
			if w, err := ensureWorktreeEnv(r, out[ci]); err != nil {
				return recs, nil, nil, err
			} else {
				warns = append(warns, w...)
			}
		}
	}
	return out, migrated, warns, nil
}
