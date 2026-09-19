package ports

import (
	"strings"
	"testing"
)

func TestBaseAllocation_EqualsBase(t *testing.T) {
	a := Allocator{Base: DefaultBase(), Ranges: DefaultRanges()}
	got := a.BaseAllocation()
	base := DefaultBase()
	for name, b := range base {
		if got[name] != b {
			t.Errorf("BaseAllocation()[%q] = %d, want base %d", name, got[name], b)
		}
	}
}

func TestFindFreeAllocation_EmptyTakenTakesBase(t *testing.T) {
	a := Allocator{Base: DefaultBase(), Ranges: DefaultRanges()}
	got, err := a.FindFreeAllocation(nil, nil)
	if err != nil {
		t.Fatalf("FindFreeAllocation = %v", err)
	}
	if got[PortApp] != 8000 {
		t.Errorf("app = %d, want 8000", got[PortApp])
	}
}

func TestFindFreeAllocation_GapReuse(t *testing.T) {
	a := Allocator{Base: DefaultBase(), Ranges: DefaultRanges()}
	taken := map[int]struct{}{8000: {}, 8002: {}}
	got, err := a.FindFreeAllocation(taken, nil)
	if err != nil {
		t.Fatalf("FindFreeAllocation = %v", err)
	}
	if got[PortApp] != 8001 {
		t.Errorf("app = %d, want lowest free 8001", got[PortApp])
	}
}

func TestFindFreeAllocation_SkipsOSOccupied(t *testing.T) {
	a := Allocator{Base: DefaultBase(), Ranges: DefaultRanges()}
	isFree := func(p int) bool { return p != 8000 && p != 8001 }
	got, err := a.FindFreeAllocation(nil, isFree)
	if err != nil {
		t.Fatalf("FindFreeAllocation = %v", err)
	}
	if got[PortApp] != 8002 {
		t.Errorf("app = %d, want 8002 (8000-8001 OS-occupied)", got[PortApp])
	}
}

func TestFindFreeAllocation_CrossServiceCollisionAvoided(t *testing.T) {
	a := Allocator{
		Base:   map[string]int{"app": 8000, "web": 8000},
		Ranges: map[string][2]int{"app": {8000, 8001}, "web": {8000, 8001}},
	}
	got, err := a.FindFreeAllocation(nil, nil)
	if err != nil {
		t.Fatalf("FindFreeAllocation = %v", err)
	}
	if got["app"] == got["web"] {
		t.Errorf("cross-service collision: app=web=%d", got["app"])
	}
	if got["app"] != 8000 || got["web"] != 8001 {
		t.Errorf("got %v, want app=8000 web=8001 (name tiebreak)", got)
	}
}

func TestFindFreeAllocation_NarrowRangeFirst(t *testing.T) {
	// Heterogeneous overlap: web has the earlier deadline, so it goes
	// first; plain sorted order (app first) would falsely exhaust web.
	a := Allocator{
		Base:   map[string]int{"app": 8000, "web": 8000},
		Ranges: map[string][2]int{"app": {8000, 8001}, "web": {8000, 8000}},
	}
	got, err := a.FindFreeAllocation(nil, nil)
	if err != nil {
		t.Fatalf("FindFreeAllocation = %v", err)
	}
	if got["web"] != 8000 || got["app"] != 8001 {
		t.Errorf("got %v, want web=8000 app=8001", got)
	}
}

func TestFindFreeAllocation_EarliestDeadlineFirst(t *testing.T) {
	// app has fewer candidates but the later deadline; db must go first.
	// taken blocks db's low ports, leaving only 8000 for db and 8001 for app.
	a := Allocator{
		Base:   map[string]int{"app": 8000, "db": 7990},
		Ranges: map[string][2]int{"app": {8000, 8001}, "db": {7990, 8000}},
	}
	taken := map[int]struct{}{}
	for p := 7990; p <= 7999; p++ {
		taken[p] = struct{}{}
	}
	got, err := a.FindFreeAllocation(taken, nil)
	if err != nil {
		t.Fatalf("FindFreeAllocation = %v", err)
	}
	if got["db"] != 8000 || got["app"] != 8001 {
		t.Errorf("got %v, want db=8000 app=8001", got)
	}
}

func TestFindFreeAllocation_ExhaustionNamesService(t *testing.T) {
	a := Allocator{
		Base:   map[string]int{"app": 8000},
		Ranges: map[string][2]int{"app": {8000, 8001}},
	}
	taken := map[int]struct{}{8000: {}, 8001: {}}
	_, err := a.FindFreeAllocation(taken, nil)
	if err == nil {
		t.Fatal("FindFreeAllocation = nil, want exhaustion error")
	}
	if !strings.Contains(err.Error(), `"app"`) || !strings.Contains(err.Error(), "[8000,8001]") {
		t.Errorf("error %q should name service and range", err.Error())
	}
}

func TestFindFreeAllocation_CustomPorts(t *testing.T) {
	a := Allocator{
		Base:   map[string]int{"app": 8000, "web": 3000},
		Ranges: map[string][2]int{"app": {8000, 8099}, "web": {3000, 3099}},
	}
	got, err := a.FindFreeAllocation(map[int]struct{}{8000: {}, 3000: {}}, nil)
	if err != nil {
		t.Fatalf("FindFreeAllocation = %v", err)
	}
	if got["app"] != 8001 || got["web"] != 3001 {
		t.Errorf("got %v, want app=8001 web=3001", got)
	}
	if err := a.Validate(); err != nil {
		t.Errorf("Validate() custom ports = %v, want nil", err)
	}
}

func TestAllocationsCollide_Basic(t *testing.T) {
	a := map[string]int{"app": 8000, "web": 3000}
	b := map[string]int{"app": 8001, "web": 3001}
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
	ok := Allocator{Base: DefaultBase(), Ranges: DefaultRanges()}
	if err := ok.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
	missing := DefaultBase()
	delete(missing, PortApp)
	if err := (Allocator{Base: missing, Ranges: DefaultRanges()}).Validate(); err == nil {
		t.Errorf("Validate() with missing port = nil, want error")
	}
	empty := Allocator{Base: map[string]int{}, Ranges: map[string][2]int{}}
	if err := empty.Validate(); err == nil {
		t.Errorf("Validate() with empty base = nil, want error")
	}
	noRange := Allocator{Base: DefaultBase(), Ranges: map[string][2]int{}}
	if err := noRange.Validate(); err == nil {
		t.Errorf("Validate() with missing range = nil, want error")
	}
	outside := Allocator{Base: map[string]int{"app": 9000}, Ranges: map[string][2]int{"app": {8000, 8099}}}
	if err := outside.Validate(); err == nil {
		t.Errorf("Validate() with base outside range = nil, want error")
	}
	inverted := Allocator{Base: map[string]int{"app": 8000}, Ranges: map[string][2]int{"app": {8099, 8000}}}
	if err := inverted.Validate(); err == nil {
		t.Errorf("Validate() with inverted range = nil, want error")
	}
	unknownRange := Allocator{Base: DefaultBase(), Ranges: map[string][2]int{"app": {8000, 8099}, "web": {3000, 3099}}}
	if err := unknownRange.Validate(); err == nil {
		t.Errorf("Validate() with unknown range = nil, want error")
	}
	dupBase := Allocator{
		Base:   map[string]int{"app": 8000, "web": 8000},
		Ranges: map[string][2]int{"app": {8000, 8099}, "web": {8000, 8099}},
	}
	if err := dupBase.Validate(); err == nil {
		t.Errorf("Validate() with duplicate base values = nil, want error")
	} else if !strings.Contains(err.Error(), "share value 8000") {
		t.Errorf("duplicate-base error %q should name the shared value", err.Error())
	}
}

func TestTakenFromRecords_Union(t *testing.T) {
	recs := []WorktreeRecord{
		{Branch: "a", Ports: map[string]int{"app": 8000}},
		{Branch: "b", Ports: map[string]int{"app": 8001, "web": 3000}},
	}
	taken := TakenFromRecords(recs)
	for _, p := range []int{8000, 8001, 3000} {
		if _, ok := taken[p]; !ok {
			t.Errorf("taken missing %d", p)
		}
	}
	if _, ok := taken[8002]; ok {
		t.Errorf("taken should not contain 8002")
	}
}

func TestIsPortFree_RejectsBadPorts(t *testing.T) {
	for _, p := range []int{0, -1, 70000} {
		if IsPortFree(p) {
			t.Errorf("IsPortFree(%d) = true, want false", p)
		}
	}
}
