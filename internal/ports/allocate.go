package ports

import "fmt"

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

// stepOrDefault returns the allocator step, defaulting to DefaultStep
// when Step is zero.
func (a Allocator) stepOrDefault() int {
	if a.Step != 0 {
		return a.Step
	}
	return DefaultStep
}

// Validate checks that base is non-empty, holds the required `app` port,
// and has valid values. Extra port names are allowed for stacks that bind
// additional host ports.
func (a Allocator) Validate() error {
	base := a.Base
	if base == nil {
		base = DefaultBase()
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
	if a.Step < 0 {
		return fmt.Errorf("ports step must not be negative: %d", a.Step)
	}
	return nil
}

// Allocate returns base + index*step per port name.
func (a Allocator) Allocate(index int) Allocation {
	base := a.baseOrDefault()
	step := a.stepOrDefault()
	ports := make(map[string]int, len(base))
	for name, b := range base {
		ports[name] = b + index*step
	}
	return Allocation{Index: index, Ports: ports}
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

// IndexesCollide reports whether Allocate(i) and Allocate(j) share any
// host port value.
func (a Allocator) IndexesCollide(i, j int) bool {
	return AllocationsCollide(a.Allocate(i).Ports, a.Allocate(j).Ports)
}
