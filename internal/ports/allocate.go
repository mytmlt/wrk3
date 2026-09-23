package ports

import (
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
)

// BasePort returns the explicit port from the URLSpec's BaseURL.
func (s URLSpec) BasePort() int {
	if s.BaseURL == nil {
		return 0
	}
	p := s.BaseURL.Port()
	if p == "" {
		return 0
	}
	n, _ := strconv.Atoi(p)
	return n
}

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

// HostPortsByBase inverts hostPorts (an allocation from this Allocator)
// into base value -> allocated port. URL specs whose base port matches a
// host base value track that group's port instead of allocating their
// own (see AllocateURLs): the URL always names the port its service
// actually listens on.
func (a Allocator) HostPortsByBase(hostPorts map[string]int) map[int]int {
	base := a.baseOrDefault()
	out := make(map[int]int, len(base))
	for name, b := range base {
		if p, ok := hostPorts[name]; ok {
			out[b] = p
		}
	}
	return out
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

// AllocateURLs resolves each URLSpec to a port for one worktree and
// returns var -> port. A spec whose base port matches a host base value
// tracks that group's allocated port (via tracked, see HostPortsByBase),
// so the URL always names the port its service actually listens on —
// e.g. BASE_URL on http://localhost:8000 follows the `app` allocation
// instead of taking its own port. The tracked port must sit inside every
// group member's range, else allocation fails naming the var. Specs with
// no tracked base (standalone URLs) allocate the lowest free port in
// their range, like host ports: specs sharing one base port are aliases
// for a single URL and receive the same port, so they stay in lockstep
// across worktrees. taken holds already-used ports (host allocations +
// previously allocated URL ports); isFree probes OS availability.
// Results are added to used so cross-collisions between distinct groups
// in one call are avoided. Exhaustion errors name the var (or alias
// group) and its range.
func AllocateURLs(specs []URLSpec, tracked map[int]int, taken map[int]struct{}, isFree func(int) bool) (map[string]int, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	// Group specs by shared base port: one URL port per group.
	byBase := make(map[int][]int, len(specs))
	for i, spec := range specs {
		bp := spec.BasePort()
		byBase[bp] = append(byBase[bp], i)
	}
	type group struct {
		members []URLSpec // sorted by var
		base    int       // shared base port
		ceil    int       // smallest member max
	}
	groups := make([]group, 0, len(byBase))
	for b, idxs := range byBase {
		if b <= 0 {
			first := specs[idxs[0]]
			return nil, fmt.Errorf("no free port for %q in [%d,%d]: no explicit port in base URL", first.Var, first.Range[0], first.Range[1])
		}
		ceil := -1
		members := make([]URLSpec, 0, len(idxs))
		for _, i := range idxs {
			members = append(members, specs[i])
			if ceil < 0 || specs[i].Range[1] < ceil {
				ceil = specs[i].Range[1]
			}
		}
		sort.Slice(members, func(a, c int) bool { return members[a].Var < members[c].Var })
		groups = append(groups, group{members: members, base: b, ceil: ceil})
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].ceil != groups[j].ceil {
			return groups[i].ceil < groups[j].ceil
		}
		if groups[i].base != groups[j].base {
			return groups[i].base < groups[j].base
		}
		return groups[i].members[0].Var < groups[j].members[0].Var
	})
	used := make(map[int]struct{}, len(taken))
	for p := range taken {
		used[p] = struct{}{}
	}
	out := make(map[string]int, len(specs))
	for _, g := range groups {
		if tp, ok := tracked[g.base]; ok {
			for _, m := range g.members {
				if tp < m.Range[0] || tp > m.Range[1] {
					return nil, fmt.Errorf("no free port for %q in [%d,%d]: tracked host port %d sits outside its range", m.Var, m.Range[0], m.Range[1], tp)
				}
			}
			for _, m := range g.members {
				out[m.Var] = tp
			}
			used[tp] = struct{}{}
			continue
		}
		found := -1
		for p := g.base; p <= g.ceil; p++ {
			if _, ok := used[p]; ok {
				continue
			}
			if isFree != nil && !isFree(p) {
				continue
			}
			okAll := true
			for _, m := range g.members {
				if p < m.Range[0] || p > m.Range[1] {
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
			if len(g.members) == 1 {
				m := g.members[0]
				return nil, fmt.Errorf("no free port for %q in [%d,%d]", m.Var, m.Range[0], m.Range[1])
			}
			vars := make([]string, 0, len(g.members))
			for _, m := range g.members {
				vars = append(vars, m.Var)
			}
			return nil, fmt.Errorf("no free port for %q in [%d,%d]", strings.Join(vars, ","), g.base, g.ceil)
		}
		for _, m := range g.members {
			out[m.Var] = found
		}
		used[found] = struct{}{}
	}
	return out, nil
}

// TakenFromURLRecords returns the union of all URL port values in recs.
func TakenFromURLRecords(recs []WorktreeRecord) map[int]struct{} {
	out := make(map[int]struct{})
	for _, rec := range recs {
		if rec.Urls == nil {
			continue
		}
		for _, p := range rec.Urls {
			out[p] = struct{}{}
		}
	}
	return out
}
