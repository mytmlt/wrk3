package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"

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

// assignAllocation returns host and URL port allocations for a new worktree.
// Host ports allocate first, then URL ports. URL specs whose base port
// matches a host base value track that host service (same port); other
// URL groups scan for the lowest free port, sharing one taken set (main
// host + main URL reservations, existing host/URL ports, fresh host
// ports) so distinct groups never share a port. URL specs with the same
// base port are aliases and share one port. Existing URL ports and main
// URL reservations seed taken before host allocation (like
// reconcileState): host ranges may overlap URL ranges, and tracked URL
// reuse skips taken checks, so a fresh host port must never land on a
// held URL port.
func assignAllocation(r *resolved, recs []ports.WorktreeRecord) (ports.EnvAllocation, error) {
	alloc := r.cfg.Allocator()
	taken := takenWithMain(alloc, recs)
	for p := range ports.TakenFromURLRecords(recs) {
		taken[p] = struct{}{}
	}
	for _, p := range r.cfg.URLBasePorts() {
		taken[p] = struct{}{}
	}
	hostPorts, err := alloc.FindFreeAllocation(taken, osPortFree)
	if err != nil {
		return ports.EnvAllocation{}, fmt.Errorf("ports: %w", err)
	}
	for _, p := range hostPorts {
		taken[p] = struct{}{}
	}
	specs := r.cfg.URLSpecs()
	if len(specs) == 0 {
		return ports.EnvAllocation{Ports: hostPorts}, nil
	}
	base := r.cfg.Ports.Base
	if base == nil {
		base = ports.DefaultBase()
	}
	urlPorts, err := ports.AllocateURLsWithHost(specs, taken, ports.HostBaseToPort(base, hostPorts), osPortFree)
	if err != nil {
		return ports.EnvAllocation{}, fmt.Errorf("urls: %w", err)
	}
	return ports.EnvAllocation{Ports: hostPorts, URLs: urlPorts}, nil
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
// is ensured (managed port keys overwritten to the new allocation).
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
		// taken excludes the record's own current host allocation: it is
		// being replaced, so it must not block its own fresh slot scan.
		// Its URL ports stay held (like main URL reservations and other
		// records' URLs): they survive migration, so the fresh host must
		// never land on them when ranges overlap.
		taken := make(map[int]struct{}, len(out)+len(mainPorts))
		for _, p := range mainPorts {
			taken[p] = struct{}{}
		}
		for _, p := range r.cfg.URLBasePorts() {
			taken[p] = struct{}{}
		}
		for p := range ports.TakenFromURLRecords(out) {
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
			"migrated worktree %q from colliding ports to index %d (main reserves the ports.base allocation)",
			out[ci].Branch, out[ci].Index))
		// Ensure .env only when the worktree dir exists: stale records
		// (dir missing) still migrate state, but must not create dirs.
		if st, statErr := os.Stat(out[ci].AbsPath); statErr == nil && st.IsDir() {
			if err := ensureWorktreeEnv(r, out[ci], r.cfg.URLSpecs()); err != nil {
				return recs, nil, nil, err
			}
		}
	}
	return out, migrated, warns, nil
}

// migrateURLTracking repoints managed URL vars that track a host service
// to the host's allocated port. A URL group whose base port equals a host
// base value renders that service (e.g. BASE_URL http://localhost:8000
// renders the app listener), so every member must equal the host's port.
// Records allocated before host tracking scanned past the host port
// (e.g. APP_PORT 8001 with BASE_URL ...:8002) are healed to the tracked
// port; alias members move together. URL groups with no matching host
// base are never touched, and records without URL allocations are
// skipped. A tracked port colliding with another record is left alone
// with a warning: state is never half-written.
//
// Records are processed in sorted branch order for determinism. Existing
// non-tracking URLs are never renumbered. The worktree .env is ensured
// (managed keys overwritten to the new allocation) only when the dir
// exists: stale records still migrate state, but must not create dirs.
func migrateURLTracking(r *resolved, recs []ports.WorktreeRecord) (updated []ports.WorktreeRecord, migrated []string, warns []string, err error) {
	if r == nil || r.cfg == nil {
		return recs, nil, nil, nil
	}
	specs := r.cfg.URLSpecs()
	if len(specs) == 0 {
		return recs, nil, nil, nil
	}
	base := r.cfg.Ports.Base
	if base == nil {
		base = ports.DefaultBase()
	}
	// Group spec indexes by shared base port (aliases).
	byBase := make(map[int][]int, len(specs))
	for i, s := range specs {
		bp := s.BasePort()
		byBase[bp] = append(byBase[bp], i)
	}
	// Main reservations (host + URL) for collision checks.
	alloc := r.cfg.Allocator()
	mainTaken := make(map[int]struct{})
	for _, p := range alloc.BaseAllocation() {
		mainTaken[p] = struct{}{}
	}
	for _, p := range r.cfg.URLBasePorts() {
		mainTaken[p] = struct{}{}
	}
	out := append([]ports.WorktreeRecord(nil), recs...)
	order := make([]int, 0, len(out))
	for i := range out {
		if len(out[i].Urls) > 0 {
			order = append(order, i)
		}
	}
	sort.Slice(order, func(a, b int) bool { return out[order[a]].Branch < out[order[b]].Branch })
	for _, idx := range order {
		hostMap := ports.HostBaseToPort(base, out[idx].Ports)
		want := make(map[string]int, len(out[idx].Urls))
		for k, v := range out[idx].Urls {
			want[k] = v
		}
		var changed []string
		for bp, members := range byBase {
			hp, ok := hostMap[bp]
			if !ok {
				continue
			}
			inRange := true
			for _, mi := range members {
				rng := specs[mi].Range
				if hp < rng[0] || hp > rng[1] {
					inRange = false
					break
				}
			}
			if !inRange {
				continue
			}
			for _, mi := range members {
				v := specs[mi].Var
				cur, present := want[v]
				if !present || cur == hp {
					continue
				}
				want[v] = hp
				changed = append(changed, fmt.Sprintf("%s=%d", v, hp))
			}
		}
		if len(changed) == 0 {
			continue
		}
		// changed is built by ranging over the byBase map, so sort for
		// deterministic warnings on both the blocked and migrated paths.
		sort.Strings(changed)
		// Collision check: tracked ports must not belong to main or any
		// other record (host or URL). There is no exemption for own
		// host ports: a tracked port always equals our own host port,
		// and fresh host allocations hold main URL reservations before
		// allocating (see assignAllocation), so a main hit means a
		// stale host — leave the record alone with a warning instead
		// of migrating into the collision.
		blocked := false
		for v, p := range want {
			if out[idx].Urls[v] == p {
				continue // unchanged var cannot newly collide
			}
			if _, ok := mainTaken[p]; ok {
				blocked = true
				break
			}
			for j := range out {
				if j == idx {
					continue
				}
				for _, op := range out[j].Ports {
					if op == p {
						blocked = true
						break
					}
				}
				if blocked {
					break
				}
				for _, op := range out[j].Urls {
					if op == p {
						blocked = true
						break
					}
				}
				if blocked {
					break
				}
			}
			if blocked {
				break
			}
		}
		if blocked {
			warns = append(warns, fmt.Sprintf(
				"worktree %q URLs not migrated to tracked host ports (collision): %s",
				out[idx].Branch, strings.Join(changed, ", ")))
			continue
		}
		prev := out[idx].Urls
		out[idx].Urls = want
		if st, statErr := os.Stat(out[idx].AbsPath); statErr == nil && st.IsDir() {
			if err := ensureWorktreeEnv(r, out[idx], specs); err != nil {
				// Revert: this branch's .env was not updated, so state
				// must keep the old URLs. Earlier branches are fully
				// migrated (state + .env); return them with the error
				// instead of discarding all progress.
				out[idx].Urls = prev
				return out, migrated, warns, err
			}
		}
		migrated = append(migrated, out[idx].Branch)
		warns = append(warns, fmt.Sprintf(
			"migrated worktree %q URLs to tracked host ports: %s",
			out[idx].Branch, strings.Join(changed, ", ")))
	}
	return out, migrated, warns, nil
}
