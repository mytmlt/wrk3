package ports

import (
	"fmt"
	"net"
	"sort"
	"strconv"
)

// baseOrDefault returns a copy of the allocator base, defaulting to
// DefaultBase when Base is nil.
func (a Allocator) baseOrDefault() map[string]int {
	if a.Base != nil {
		out := make(map[string]int, len(a.Base))
		for k, v := range a.Base {
			out[k] = v
		}
		return out
	}
	return DefaultBase()
}

// rangesOrDefault returns a copy of the allocator ranges, defaulting to
// DefaultRanges when Ranges is nil.
func (a Allocator) rangesOrDefault() map[string][2]int {
	if a.Ranges != nil {
		out := make(map[string][2]int, len(a.Ranges))
		for k, v := range a.Ranges {
			out[k] = v
		}
		return out
	}
	return DefaultRanges()
}

// Validate checks that base is non-empty, holds the required `app` port,
// and has valid values, and that every base entry has a valid range
// containing its base. Extra port names are allowed for stacks that bind
// additional host ports.
func (a Allocator) Validate() error {
	base := a.Base
	if base == nil {
		base = DefaultBase()
	}
	ranges := a.Ranges
	if ranges == nil {
		ranges = DefaultRanges()
	}
	if len(base) == 0 {
		return fmt.Errorf("ports base: must list at least one port")
	}
	if _, ok := base[PortApp]; !ok {
		return fmt.Errorf("ports base: missing port %q", PortApp)
	}
	for name, v := range base {
		if v <= 0 || v > 65535 {
			return fmt.Errorf("ports base: port %q out of range: %d", name, v)
		}
	}
	seenBase := make(map[int]string, len(base))
	for name, v := range base {
		if prev, dup := seenBase[v]; dup {
			return fmt.Errorf("ports base: ports %q and %q share value %d", prev, name, v)
		}
		seenBase[v] = name
	}
	if _, ok := ranges[PortApp]; !ok {
		return fmt.Errorf("ports ranges: missing range for %q", PortApp)
	}
	for name, r := range ranges {
		if r[0] <= 0 || r[0] > 65535 || r[1] <= 0 || r[1] > 65535 {
			return fmt.Errorf("ports ranges: range for %q out of range: [%d,%d]", name, r[0], r[1])
		}
		if r[0] > r[1] {
			return fmt.Errorf("ports ranges: range for %q is inverted: [%d,%d]", name, r[0], r[1])
		}
		if _, ok := base[name]; !ok {
			return fmt.Errorf("ports ranges: range for unknown port %q (no ports.base entry)", name)
		}
	}
	for name, b := range base {
		r, ok := ranges[name]
		if !ok {
			return fmt.Errorf("ports ranges: missing range for %q (every ports.base entry needs a range)", name)
		}
		if b < r[0] || b > r[1] {
			return fmt.Errorf("ports base: port %q=%d outside its range [%d,%d]", name, b, r[0], r[1])
		}
	}
	return nil
}

// BaseAllocation returns a copy of the base ports (the implicit main
// checkout allocation, reserved index 0).
func (a Allocator) BaseAllocation() map[string]int {
	return a.baseOrDefault()
}

// FindFreeAllocation scans each service range from base upward by 1 (per
// issue #7: scan base, base+1, … ≤ max) and returns the lowest free port
// per service. taken holds already-used host ports (union of state records
// plus the main reservation); isFree probes OS availability (nil means
// state-only, always free). Ports assigned earlier in the same call count
// as taken so cross-service collisions within one candidate are avoided.
// Services scan fewest-candidates-first (r[1]-base, name tiebreak) so
// heterogeneously overlapping ranges resolve when feasible instead of
// falsely exhausting under plain sorted order; the order is still
// deterministic. Exhaustion errors name the service and its configured
// range [min,max] per the acceptance contract (the scan floor is base,
// which always sits inside [min,max]).
func (a Allocator) FindFreeAllocation(taken map[int]struct{}, isFree func(int) bool) (map[string]int, error) {
	base := a.baseOrDefault()
	ranges := a.rangesOrDefault()
	names := make([]string, 0, len(base))
	for name := range base {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		bi, bj := base[names[i]], base[names[j]]
		ci, cj := ranges[names[i]][1]-bi, ranges[names[j]][1]-bj
		if ci != cj {
			return ci < cj
		}
		return names[i] < names[j]
	})
	used := make(map[int]struct{}, len(taken)+len(base))
	for p := range taken {
		used[p] = struct{}{}
	}
	out := make(map[string]int, len(base))
	for _, name := range names {
		b := base[name]
		r, ok := ranges[name]
		if !ok {
			return nil, fmt.Errorf("ports ranges: missing range for %q (every ports.base entry needs a range)", name)
		}
		found := -1
		for p := b; p <= r[1]; p++ {
			if _, ok := used[p]; ok {
				continue
			}
			if isFree != nil && !isFree(p) {
				continue
			}
			found = p
			break
		}
		if found < 0 {
			return nil, fmt.Errorf("no free port for %q in [%d,%d]", name, r[0], r[1])
		}
		out[name] = found
		used[found] = struct{}{}
	}
	return out, nil
}

// TakenFromRecords returns the union of all host port values in recs,
// including stale rows (dir missing, still in state) which intentionally
// hold their ports until remove --force.
func TakenFromRecords(recs []WorktreeRecord) map[int]struct{} {
	out := make(map[int]struct{})
	for _, rec := range recs {
		for _, p := range rec.Ports {
			out[p] = struct{}{}
		}
	}
	return out
}

// IsPortFree reports whether a TCP bind to 127.0.0.1:port succeeds.
// Used as the isFree probe at assign time (add/adopt); status display
// paths use state-only previews (nil isFree) and never call this.
// The 127.0.0.1 probe is the conflict domain pinned by issue #7.
func IsPortFree(port int) bool {
	if port <= 0 || port > 65535 {
		return false
	}
	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

// AllocationsCollide reports whether two allocations share any host
// port value (across any port names).
func AllocationsCollide(a, b map[string]int) bool {
	seen := make(map[int]struct{}, len(a))
	for _, v := range a {
		seen[v] = struct{}{}
	}
	for _, v := range b {
		if _, ok := seen[v]; ok {
			return true
		}
	}
	return false
}
