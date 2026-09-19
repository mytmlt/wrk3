package cmd

import (
	"fmt"
	"os"
	"sort"

	"github.com/mytmlt/wrk3/internal/ports"
)

// osPortFree probes OS availability at assign time (add/adopt/reconcile).
// It is a variable so unit tests stay hermetic (no bind in tests).
var osPortFree = ports.IsPortFree

// takenWithMain returns the union of all host ports in recs plus the
// main (ports.base) reservation. Stale rows intentionally hold ports.
func takenWithMain(alloc ports.Allocator, recs []ports.WorktreeRecord) map[int]struct{} {
	taken := ports.TakenFromRecords(recs)
	for _, p := range alloc.BaseAllocation() {
		taken[p] = struct{}{}
	}
	return taken
}

// assignPorts returns the lowest free allocation for a new worktree:
// gap reuse within ranges, skipping state-taken ports and OS-occupied
// ports (bind probe). Exhaustion errors name the service and its range.
func assignPorts(alloc ports.Allocator, recs []ports.WorktreeRecord) (map[string]int, error) {
	return alloc.FindFreeAllocation(takenWithMain(alloc, recs), osPortFree)
}

// previewPorts is the state-only counterpart of assignPorts for display
// paths (status/dashboard): no blocking bind checks in renders.
func previewPorts(alloc ports.Allocator, recs []ports.WorktreeRecord) (map[string]int, error) {
	return alloc.FindFreeAllocation(takenWithMain(alloc, recs), nil)
}

// migrateLegacyMainCollisions reassigns managed records that collide with
// the implicit main checkout (exactly the ports.base allocation) to the
// lowest free range allocation.
//
// Pre-range state files may still hold a managed allocation overlapping
// main's ports (e.g. app=8000 with defaults). Since main owns those
// ports, every recordsWithMain call used to hard-error and block
// status/pull/dashboard. Migration instead reassigns the colliding record
// via FindFreeAllocation (gap reuse, OS-aware), preserving other records:
//
//	main == ports.base, managed == lowest free in ranges
//
// Records are processed in sorted branch order for determinism.
// Existing non-colliding ports are never renumbered. The worktree .env
// is gap-filled (existing values never overwritten); when the on-disk
// .env still holds the old ports a divergence warning is returned so the
// caller can report it — the state file (runner env source) is fixed
// regardless and takes effect on next up/reload.
func migrateLegacyMainCollisions(r *resolved, recs []ports.WorktreeRecord) (updated []ports.WorktreeRecord, migrated []string, warns []string, err error) {
	if r == nil || r.cfg == nil {
		return recs, nil, nil, nil
	}
	alloc := r.cfg.Allocator()
	mainPorts := alloc.BaseAllocation()

	var colliding []int
	for i, rec := range recs {
		if ports.AllocationsCollide(mainPorts, rec.Ports) {
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

	for _, ci := range colliding {
		old := out[ci].Ports
		// taken excludes the record's own current allocation: it is being
		// replaced, so it must not block its own fresh slot scan.
		taken := make(map[int]struct{}, len(out)+len(mainPorts))
		for _, p := range mainPorts {
			taken[p] = struct{}{}
		}
		for i, rec := range out {
			if i == ci {
				continue
			}
			for _, p := range rec.Ports {
				taken[p] = struct{}{}
			}
		}
		fresh, err := alloc.FindFreeAllocation(taken, osPortFree)
		if err != nil {
			return recs, nil, nil, fmt.Errorf("migrate worktree %q: %w", out[ci].Branch, err)
		}
		out[ci].Index = nextIndex(out)
		out[ci].Ports = fresh
		migrated = append(migrated, out[ci].Branch)
		warns = append(warns, fmt.Sprintf(
			"migrated worktree %q from colliding ports to index %d (main reserves the ports.base allocation); .env divergence warnings below are advisory",
			out[ci].Branch, out[ci].Index))
		_ = old
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
