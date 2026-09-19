package ports

import "testing"

func TestAllocator_NilBaseAndRangesDefaults(t *testing.T) {
	a := Allocator{}
	if got := a.BaseAllocation()[PortApp]; got != DefaultBase()[PortApp] {
		t.Errorf("nil base BaseAllocation() app = %d, want %d", got, DefaultBase()[PortApp])
	}
	if err := a.Validate(); err != nil {
		t.Errorf("Validate() nil base = %v, want nil (defaults)", err)
	}
	// baseOrDefault must copy, not alias the result.
	src := map[string]int{"app": 8000}
	b := Allocator{Base: src, Ranges: DefaultRanges()}
	got := b.BaseAllocation()
	got["app"] = 1
	if src["app"] != 8000 {
		t.Errorf("BaseAllocation result aliases input, src mutated to %d", src["app"])
	}
}

func TestAllocator_PortRangeRejects(t *testing.T) {
	for _, base := range []map[string]int{
		{"app": 0},
		{"app": -1},
		{"app": 99999},
		{"app": 70000},
	} {
		if err := (Allocator{Base: base, Ranges: DefaultRanges()}).Validate(); err == nil {
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
