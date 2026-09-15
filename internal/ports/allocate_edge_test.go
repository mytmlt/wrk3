package ports

import "testing"

func TestAllocator_NilBaseAndZeroStepDefaults(t *testing.T) {
	a := Allocator{}
	if got := a.Allocate(0).Ports[PortApp]; got != DefaultBase()[PortApp] {
		t.Errorf("nil base Allocate(0) app = %d, want %d", got, DefaultBase()[PortApp])
	}
	if got := a.Allocate(2).Ports[PortApp]; got != DefaultBase()[PortApp]+2*DefaultStep {
		t.Errorf("zero step Allocate(2) app = %d, want default step", got)
	}
	if err := a.Validate(); err != nil {
		t.Errorf("Validate() nil base = %v, want nil (defaults)", err)
	}
	// baseOrDefault must copy, not alias the result.
	src := map[string]int{"app": 8000}
	b := Allocator{Base: src}
	got := b.Allocate(0).Ports
	got["app"] = 1
	if src["app"] != 8000 {
		t.Errorf("Allocate result aliases input, src mutated to %d", src["app"])
	}
}

func TestAllocator_PortRangeRejects(t *testing.T) {
	for _, base := range []map[string]int{
		{"app": 0},
		{"app": -1},
		{"app": 99999},
		{"app": 70000},
	} {
		if err := (Allocator{Base: base, Step: 100}).Validate(); err == nil {
			t.Errorf("Validate(%v) = nil, want range error", base)
		}
	}
}

func TestAllocationsCollide_Empty(t *testing.T) {
	if AllocationsCollide(nil, nil) {
		t.Error("empty allocations must not collide")
	}
	if AllocationsCollide(map[string]int{}, map[string]int{"app": 8000}) {
		t.Error("empty vs non-empty must not collide")
	}
}
