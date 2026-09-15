package ports

import "testing"

func TestAllocate_Offsets(t *testing.T) {
	a := Allocator{Base: DefaultBase(), Step: DefaultStep}
	base := DefaultBase()
	for _, idx := range []int{0, 1, 2, 5} {
		got := a.Allocate(idx)
		if got.Index != idx {
			t.Fatalf("Allocate(%d).Index = %d", idx, got.Index)
		}
		for name, b := range base {
			want := b + idx*DefaultStep
			if got.Ports[name] != want {
				t.Errorf("Allocate(%d)[%q] = %d, want %d", idx, name, got.Ports[name], want)
			}
		}
	}
}

func TestAllocate_ZeroIndexEqualsBase(t *testing.T) {
	a := Allocator{Base: DefaultBase(), Step: DefaultStep}
	got := a.Allocate(0).Ports
	base := DefaultBase()
	for name, b := range base {
		if got[name] != b {
			t.Errorf("Allocate(0)[%q] = %d, want base %d", name, got[name], b)
		}
	}
}

func TestAllocate_CustomStep(t *testing.T) {
	a := Allocator{Base: DefaultBase(), Step: 10}
	got := a.Allocate(3).Ports
	base := DefaultBase()
	for name, b := range base {
		if want := b + 30; got[name] != want {
			t.Errorf("custom step Allocate(3)[%q] = %d, want %d", name, got[name], want)
		}
	}
}

func TestAllocate_CustomPorts(t *testing.T) {
	a := Allocator{Base: map[string]int{"app": 8000, "web": 3000}, Step: 100}
	got := a.Allocate(2).Ports
	if got["app"] != 8200 || got["web"] != 3200 {
		t.Errorf("Allocate(2) = %v, want app=8200 web=3200", got)
	}
	if err := a.Validate(); err != nil {
		t.Errorf("Validate() custom ports = %v, want nil", err)
	}
}

func TestIndexesCollide_AdjacentNoOverlap(t *testing.T) {
	a := Allocator{Base: DefaultBase(), Step: DefaultStep}
	if a.IndexesCollide(0, 1) {
		t.Errorf("IndexesCollide(0,1) = true, want false with step %d", DefaultStep)
	}
	if a.IndexesCollide(1, 2) {
		t.Errorf("IndexesCollide(1,2) = true, want false")
	}
}

func TestIndexesCollide_CrossNameOverlap(t *testing.T) {
	// app base 8000 and web base 8100 differ by 100 = 1*step,
	// so index 0 and index 1 share port value 8100.
	a := Allocator{Base: map[string]int{"app": 8000, "web": 8100}, Step: 100}
	if !a.IndexesCollide(0, 1) {
		t.Errorf("IndexesCollide(0,1) = false, want true (app1 == web0 == 8100)")
	}
}

func TestAllocationsCollide_Basic(t *testing.T) {
	a := map[string]int{"app": 8000, "web": 3000}
	b := map[string]int{"app": 8100, "web": 3100}
	if AllocationsCollide(a, b) {
		t.Errorf("disjoint allocations reported as colliding")
	}
	c := map[string]int{"app": 8000}
	d := map[string]int{"web": 8000}
	if !AllocationsCollide(c, d) {
		t.Errorf("overlapping value across names not detected")
	}
	if !AllocationsCollide(a, a) {
		t.Errorf("identical allocation must collide with itself")
	}
}

func TestAllocator_Validate(t *testing.T) {
	ok := Allocator{Base: DefaultBase(), Step: DefaultStep}
	if err := ok.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
	missing := DefaultBase()
	delete(missing, PortApp)
	if err := (Allocator{Base: missing, Step: 100}).Validate(); err == nil {
		t.Errorf("Validate() with missing port = nil, want error")
	}
	empty := Allocator{Base: map[string]int{}, Step: 100}
	if err := empty.Validate(); err == nil {
		t.Errorf("Validate() with empty base = nil, want error")
	}
	if err := (Allocator{Base: DefaultBase(), Step: -1}).Validate(); err == nil {
		t.Errorf("Validate() with negative step = nil, want error")
	}
}
