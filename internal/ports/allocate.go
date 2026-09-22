package ports

import (
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
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
// additional host ports. Two names may share the same base value: they are
// aliases for one host port (e.g. `app` and `public_api` both 8000 for a
// stack that serves both on one listener) and move together at allocation
// time. Distinct .env variables are required, so names mapping to the same
// <NAME>_PORT (e.g. `api-v2` and `api_v2`) are rejected.
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
	seenVar := make(map[string]string, len(base))
	names := make([]string, 0, len(base))
	for name := range base {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		env := EnvVarForPort(name)
		if prev, dup := seenVar[env]; dup {
			return fmt.Errorf("ports base: port names %q and %q map to the same .env variable %q", prev, name, env)
		}
		seenVar[env] = name
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
// per service. Names sharing one base value are aliases for a single host
// port (e.g. `app` and `public_api` both 8000): the group is allocated
// once and every member receives the same port, so aliases stay in
// lockstep across worktrees. A group port must sit inside every member's
// range (scan floor is the shared base, ceiling is the smallest member
// max); disjoint alias ranges exhaust with a group-naming error.
// taken holds already-used host ports (union of state records
// plus the main reservation); isFree probes OS availability (nil means
// state-only, always free). Ports assigned to earlier groups count
// as taken so collisions between distinct groups in one candidate are
// avoided.
// Groups scan earliest-deadline-first (effective upper bound, then base,
// then first name) so heterogeneously overlapping ranges reduce false
// exhaustion instead of greedily exhausting under plain sorted order; the
// order is still deterministic. Exhaustion errors name the service (or
// alias group) and its configured range [min,max] per the acceptance
// contract (the scan floor is base, which always sits inside [min,max]).
func (a Allocator) FindFreeAllocation(taken map[int]struct{}, isFree func(int) bool) (map[string]int, error) {
	base := a.baseOrDefault()
	ranges := a.rangesOrDefault()
	// Group names by shared base value: one host port per group.
	byBase := make(map[int][]string, len(base))
	for name := range base {
		b := base[name]
		byBase[b] = append(byBase[b], name)
	}
	type group struct {
		names []string // sorted member names
		base  int      // shared base value
		floor int      // scan floor: base (always inside every range)
		ceil  int      // smallest member max
	}
	groups := make([]group, 0, len(byBase))
	for b, members := range byBase {
		sort.Strings(members)
		ceil := -1
		for _, name := range members {
			r, ok := ranges[name]
			if !ok {
				return nil, fmt.Errorf("ports ranges: missing range for %q (every ports.base entry needs a range)", name)
			}
			if ceil < 0 || r[1] < ceil {
				ceil = r[1]
			}
		}
		groups = append(groups, group{names: members, base: b, floor: b, ceil: ceil})
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].ceil != groups[j].ceil {
			return groups[i].ceil < groups[j].ceil
		}
		if groups[i].base != groups[j].base {
			return groups[i].base < groups[j].base
		}
		return groups[i].names[0] < groups[j].names[0]
	})
	used := make(map[int]struct{}, len(taken)+len(base))
	for p := range taken {
		used[p] = struct{}{}
	}
	out := make(map[string]int, len(base))
	for _, g := range groups {
		found := -1
		for p := g.floor; p <= g.ceil; p++ {
			if _, ok := used[p]; ok {
				continue
			}
			if isFree != nil && !isFree(p) {
				continue
			}
			okAll := true
			for _, name := range g.names {
				r := ranges[name]
				if p < r[0] || p > r[1] {
					okAll = false
					break
				}
			}
			if !okAll {
				continue
			}
			found = p
			break
		}
		if found < 0 {
			if len(g.names) == 1 {
				name := g.names[0]
				r := ranges[name]
				return nil, fmt.Errorf("no free port for %q in [%d,%d]", name, r[0], r[1])
			}
			return nil, fmt.Errorf("no free port for %q in [%d,%d]", strings.Join(g.names, ","), g.floor, g.ceil)
		}
		for _, name := range g.names {
			out[name] = found
		}
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
