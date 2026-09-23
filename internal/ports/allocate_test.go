package ports

import (
	"net/url"
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

func TestFindFreeAllocation_SharedBaseIsAlias(t *testing.T) {
	// Names sharing one base value are aliases for a single host port:
	// every member receives the same allocation and moves in lockstep.
	a := Allocator{
		Base:   map[string]int{"app": 8000, "public_api": 8000},
		Ranges: map[string][2]int{"app": {8000, 8001}, "public_api": {8000, 8001}},
	}
	if err := a.Validate(); err != nil {
		t.Fatalf("Validate() alias base = %v, want nil", err)
	}
	got, err := a.FindFreeAllocation(nil, nil)
	if err != nil {
		t.Fatalf("FindFreeAllocation = %v", err)
	}
	if got["app"] != 8000 || got["public_api"] != 8000 {
		t.Errorf("got %v, want app=public_api=8000 (alias lockstep)", got)
	}
	// Taken base moves the whole alias group together.
	moved, err := a.FindFreeAllocation(map[int]struct{}{8000: {}}, nil)
	if err != nil {
		t.Fatalf("FindFreeAllocation = %v", err)
	}
	if moved["app"] != 8001 || moved["public_api"] != 8001 {
		t.Errorf("got %v, want app=public_api=8001 (alias lockstep)", moved)
	}
}

func TestFindFreeAllocation_DistinctGroupsAvoidCollision(t *testing.T) {
	// Distinct base values are distinct host ports even when ranges
	// overlap: the second group skips the port taken by the first.
	a := Allocator{
		Base:   map[string]int{"app": 8000, "web": 8001},
		Ranges: map[string][2]int{"app": {8000, 8001}, "web": {8000, 8001}},
	}
	got, err := a.FindFreeAllocation(nil, nil)
	if err != nil {
		t.Fatalf("FindFreeAllocation = %v", err)
	}
	if got["app"] != 8000 || got["web"] != 8001 {
		t.Errorf("got %v, want app=8000 web=8001", got)
	}
}

func TestFindFreeAllocation_NarrowRangeFirst(t *testing.T) {
	// Heterogeneous overlap with distinct bases: web's only feasible port
	// is 8001 and 8000 is taken, so web must go first; plain base order
	// (app first) would take 8001 for app and falsely exhaust web.
	a := Allocator{
		Base:   map[string]int{"app": 8000, "web": 8001},
		Ranges: map[string][2]int{"app": {8000, 8002}, "web": {8001, 8001}},
	}
	taken := map[int]struct{}{8000: {}}
	got, err := a.FindFreeAllocation(taken, nil)
	if err != nil {
		t.Fatalf("FindFreeAllocation = %v", err)
	}
	if got["web"] != 8001 || got["app"] != 8002 {
		t.Errorf("got %v, want web=8001 app=8002", got)
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
		Base:   map[string]int{"app": 8000, "public_api": 8000},
		Ranges: map[string][2]int{"app": {8000, 8099}, "public_api": {8000, 8099}},
	}
	if err := dupBase.Validate(); err != nil {
		t.Errorf("Validate() with shared base values (aliases) = %v, want nil", err)
	}
	envCollision := Allocator{
		Base:   map[string]int{"app": 8000, "api-v2": 8001, "api_v2": 8002},
		Ranges: map[string][2]int{"app": {8000, 8099}, "api-v2": {8001, 8099}, "api_v2": {8002, 8099}},
	}
	if err := envCollision.Validate(); err == nil {
		t.Errorf("Validate() with names mapping to the same .env variable = nil, want error")
	} else if !strings.Contains(err.Error(), "same .env variable") {
		t.Errorf("env-collision error %q should name the shared variable", err.Error())
	}
}

func TestFindFreeAllocation_AliasRangeIntersection(t *testing.T) {
	// Alias members with different ranges share the intersection: the
	// group ceiling is the smallest member max.
	a := Allocator{
		Base:   map[string]int{"app": 8000, "public_api": 8000},
		Ranges: map[string][2]int{"app": {8000, 8005}, "public_api": {8000, 8010}},
	}
	got, err := a.FindFreeAllocation(map[int]struct{}{8000: {}, 8001: {}}, nil)
	if err != nil {
		t.Fatalf("FindFreeAllocation = %v", err)
	}
	if got["app"] != 8002 || got["public_api"] != 8002 {
		t.Errorf("got %v, want app=public_api=8002", got)
	}
	// Exhausting the tighter member range exhausts the group.
	alias := Allocator{
		Base:   map[string]int{"app": 8000, "public_api": 8000},
		Ranges: map[string][2]int{"app": {8000, 8000}, "public_api": {8000, 8010}},
	}
	taken := map[int]struct{}{8000: {}}
	if _, err := alias.FindFreeAllocation(taken, nil); err == nil {
		t.Error("FindFreeAllocation with exhausted alias range = nil, want error")
	} else if !strings.Contains(err.Error(), "app") || !strings.Contains(err.Error(), "public_api") {
		t.Errorf("alias exhaustion error %q should name the group", err.Error())
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

func ptrURL(u string) *url.URL {
	parsed, _ := url.Parse(u)
	return parsed
}

func TestAllocateURLs_EmptySpecs(t *testing.T) {
	got, err := AllocateURLs(nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("AllocateURLs(nil) = %v, want nil", err)
	}
	if got != nil {
		t.Errorf("AllocateURLs(nil) = %v, want nil", got)
	}
}

func TestAllocateURLs_TakesBasePort(t *testing.T) {
	specs := []URLSpec{
		{Var: "APP_URL", BaseURL: ptrURL("http://localhost:8000"), Range: [2]int{8000, 8099}},
	}
	got, err := AllocateURLs(specs, nil, nil, nil)
	if err != nil {
		t.Fatalf("AllocateURLs = %v", err)
	}
	if got["APP_URL"] != 8000 {
		t.Errorf("APP_URL = %d, want 8000", got["APP_URL"])
	}
}

func TestAllocateURLs_SkipsTaken(t *testing.T) {
	specs := []URLSpec{
		{Var: "APP_URL", BaseURL: ptrURL("http://localhost:8000"), Range: [2]int{8000, 8099}},
	}
	taken := map[int]struct{}{8000: {}}
	got, err := AllocateURLs(specs, nil, taken, nil)
	if err != nil {
		t.Fatalf("AllocateURLs = %v", err)
	}
	if got["APP_URL"] != 8001 {
		t.Errorf("APP_URL = %d, want 8001 (8000 taken)", got["APP_URL"])
	}
}

func TestAllocateURLs_GapReuse(t *testing.T) {
	specs := []URLSpec{
		{Var: "APP_URL", BaseURL: ptrURL("http://localhost:8000"), Range: [2]int{8000, 8099}},
	}
	taken := map[int]struct{}{8000: {}, 8002: {}}
	got, err := AllocateURLs(specs, nil, taken, nil)
	if err != nil {
		t.Fatalf("AllocateURLs = %v", err)
	}
	if got["APP_URL"] != 8001 {
		t.Errorf("APP_URL = %d, want lowest free 8001", got["APP_URL"])
	}
}

func TestAllocateURLs_SkipsOSOccupied(t *testing.T) {
	specs := []URLSpec{
		{Var: "APP_URL", BaseURL: ptrURL("http://localhost:8000"), Range: [2]int{8000, 8099}},
	}
	isFree := func(p int) bool { return p != 8000 }
	got, err := AllocateURLs(specs, nil, nil, isFree)
	if err != nil {
		t.Fatalf("AllocateURLs = %v", err)
	}
	if got["APP_URL"] != 8001 {
		t.Errorf("APP_URL = %d, want 8001 (8000 OS-occupied)", got["APP_URL"])
	}
}

func TestAllocateURLs_MultipleSpecs(t *testing.T) {
	specs := []URLSpec{
		{Var: "APP_URL", BaseURL: ptrURL("http://localhost:8000"), Range: [2]int{8000, 8099}},
		{Var: "BASE_URL", BaseURL: ptrURL("http://localhost:9000"), Range: [2]int{9000, 9099}},
	}
	got, err := AllocateURLs(specs, nil, nil, nil)
	if err != nil {
		t.Fatalf("AllocateURLs = %v", err)
	}
	if got["APP_URL"] != 8000 || got["BASE_URL"] != 9000 {
		t.Errorf("got %v, want APP_URL=8000 BASE_URL=9000", got)
	}
}

func TestAllocateURLs_SharedBaseIsAlias(t *testing.T) {
	// Specs sharing one base port are aliases for a single URL: every
	// member receives the same allocation and moves in lockstep.
	specs := []URLSpec{
		{Var: "BASE_URL", BaseURL: ptrURL("http://localhost:8000"), Range: [2]int{8000, 8099}},
		{Var: "ALLOWED_WS_ORIGINS", BaseURL: ptrURL("http://localhost:8000"), Range: [2]int{8000, 8099}},
	}
	got, err := AllocateURLs(specs, nil, nil, nil)
	if err != nil {
		t.Fatalf("AllocateURLs = %v", err)
	}
	if got["BASE_URL"] != 8000 || got["ALLOWED_WS_ORIGINS"] != 8000 {
		t.Errorf("got %v, want BASE_URL=ALLOWED_WS_ORIGINS=8000 (alias lockstep)", got)
	}
	// Taken base moves the whole alias group together.
	moved, err := AllocateURLs(specs, nil, map[int]struct{}{8000: {}, 8001: {}}, nil)
	if err != nil {
		t.Fatalf("AllocateURLs = %v", err)
	}
	if moved["BASE_URL"] != 8002 || moved["ALLOWED_WS_ORIGINS"] != 8002 {
		t.Errorf("got %v, want BASE_URL=ALLOWED_WS_ORIGINS=8002 (alias lockstep)", moved)
	}
}

func TestAllocateURLs_AliasRangeIntersection(t *testing.T) {
	// Alias members with different ranges share the intersection: the
	// group ceiling is the smallest member max.
	specs := []URLSpec{
		{Var: "BASE_URL", BaseURL: ptrURL("http://localhost:8000"), Range: [2]int{8000, 8005}},
		{Var: "ALLOWED_WS_ORIGINS", BaseURL: ptrURL("http://localhost:8000"), Range: [2]int{8000, 8010}},
	}
	got, err := AllocateURLs(specs, nil, map[int]struct{}{8000: {}, 8001: {}}, nil)
	if err != nil {
		t.Fatalf("AllocateURLs = %v", err)
	}
	if got["BASE_URL"] != 8002 || got["ALLOWED_WS_ORIGINS"] != 8002 {
		t.Errorf("got %v, want BASE_URL=ALLOWED_WS_ORIGINS=8002", got)
	}
	// Exhausting the tighter member range exhausts the group.
	tight := []URLSpec{
		{Var: "BASE_URL", BaseURL: ptrURL("http://localhost:8000"), Range: [2]int{8000, 8000}},
		{Var: "ALLOWED_WS_ORIGINS", BaseURL: ptrURL("http://localhost:8000"), Range: [2]int{8000, 8010}},
	}
	if _, err := AllocateURLs(tight, nil, map[int]struct{}{8000: {}}, nil); err == nil {
		t.Error("AllocateURLs with exhausted alias range = nil, want error")
	} else if !strings.Contains(err.Error(), "BASE_URL") || !strings.Contains(err.Error(), "ALLOWED_WS_ORIGINS") {
		t.Errorf("alias exhaustion error %q should name the group", err.Error())
	}
}

func TestAllocateURLs_DistinctBasesAvoidCollision(t *testing.T) {
	// Distinct base ports are distinct URLs even when ranges overlap:
	// the second group skips the port taken by the first.
	specs := []URLSpec{
		{Var: "APP_URL", BaseURL: ptrURL("http://localhost:8000"), Range: [2]int{8000, 8001}},
		{Var: "BASE_URL", BaseURL: ptrURL("http://localhost:8001"), Range: [2]int{8000, 8001}},
	}
	got, err := AllocateURLs(specs, nil, nil, nil)
	if err != nil {
		t.Fatalf("AllocateURLs = %v", err)
	}
	if got["APP_URL"] == got["BASE_URL"] {
		t.Errorf("cross-collision: APP_URL=BASE_URL=%d", got["APP_URL"])
	}
	if got["APP_URL"] != 8000 || got["BASE_URL"] != 8001 {
		t.Errorf("got %v, want APP_URL=8000 BASE_URL=8001", got)
	}
}

func TestAllocateURLs_HostPortCollisionAvoided(t *testing.T) {
	specs := []URLSpec{
		{Var: "APP_URL", BaseURL: ptrURL("http://localhost:8000"), Range: [2]int{8000, 8099}},
	}
	taken := map[int]struct{}{8000: {}, 8001: {}, 8002: {}}
	got, err := AllocateURLs(specs, nil, taken, nil)
	if err != nil {
		t.Fatalf("AllocateURLs = %v", err)
	}
	if got["APP_URL"] != 8003 {
		t.Errorf("APP_URL = %d, want 8003 (8000-8002 taken)", got["APP_URL"])
	}
}

func TestAllocateURLs_ExhaustionNamesVar(t *testing.T) {
	specs := []URLSpec{
		{Var: "APP_URL", BaseURL: ptrURL("http://localhost:8000"), Range: [2]int{8000, 8001}},
	}
	taken := map[int]struct{}{8000: {}, 8001: {}}
	_, err := AllocateURLs(specs, nil, taken, nil)
	if err == nil {
		t.Fatal("AllocateURLs = nil, want exhaustion error")
	}
	if !strings.Contains(err.Error(), `"APP_URL"`) || !strings.Contains(err.Error(), "[8000,8001]") {
		t.Errorf("error %q should name var and range", err.Error())
	}
}

func TestHostPortsByBase_InvertsAllocation(t *testing.T) {
	a := Allocator{
		Base:   map[string]int{"app": 8000, "public_api": 8000, "web": 3000},
		Ranges: map[string][2]int{"app": {8000, 8099}, "public_api": {8000, 8099}, "web": {3000, 3099}},
	}
	got := a.HostPortsByBase(map[string]int{"app": 8001, "public_api": 8001, "web": 3001})
	if got[8000] != 8001 || got[3000] != 3001 {
		t.Errorf("got %v, want map[8000:8001 3000:3001]", got)
	}
}

func TestAllocateURLs_TracksHostPort(t *testing.T) {
	// A URL whose base port matches a host base tracks that group's
	// allocation instead of taking its own port — even when the tracked
	// port is in taken (it is the worktree's own host port).
	specs := []URLSpec{
		{Var: "BASE_URL", BaseURL: ptrURL("http://localhost:8000"), Range: [2]int{8000, 8099}},
	}
	tracked := map[int]int{8000: 8001}
	taken := map[int]struct{}{8000: {}, 8001: {}}
	got, err := AllocateURLs(specs, tracked, taken, nil)
	if err != nil {
		t.Fatalf("AllocateURLs = %v", err)
	}
	if got["BASE_URL"] != 8001 {
		t.Errorf("BASE_URL = %d, want tracked host port 8001", got["BASE_URL"])
	}
}

func TestAllocateURLs_TrackedAliasLockstep(t *testing.T) {
	// Alias URL vars sharing a tracked base all follow the host port.
	specs := []URLSpec{
		{Var: "BASE_URL", BaseURL: ptrURL("http://localhost:8000"), Range: [2]int{8000, 8099}},
		{Var: "ALLOWED_WS_ORIGINS", BaseURL: ptrURL("http://localhost:8000"), Range: [2]int{8000, 8099}},
	}
	tracked := map[int]int{8000: 8001}
	taken := map[int]struct{}{8000: {}, 8001: {}}
	got, err := AllocateURLs(specs, tracked, taken, nil)
	if err != nil {
		t.Fatalf("AllocateURLs = %v", err)
	}
	if got["BASE_URL"] != 8001 || got["ALLOWED_WS_ORIGINS"] != 8001 {
		t.Errorf("got %v, want BASE_URL=ALLOWED_WS_ORIGINS=8001 (tracked)", got)
	}
}

func TestAllocateURLs_TrackedOutsideRangeErrors(t *testing.T) {
	specs := []URLSpec{
		{Var: "BASE_URL", BaseURL: ptrURL("http://localhost:8000"), Range: [2]int{8000, 8099}},
	}
	_, err := AllocateURLs(specs, map[int]int{8000: 8100}, nil, nil)
	if err == nil {
		t.Fatal("AllocateURLs = nil, want range error for tracked port outside URL range")
	}
	if !strings.Contains(err.Error(), `"BASE_URL"`) || !strings.Contains(err.Error(), "[8000,8099]") {
		t.Errorf("error %q should name var and range", err.Error())
	}
}

func TestAllocateURLs_MixedTrackedAndIndependent(t *testing.T) {
	// Tracked specs follow the host port while standalone specs still
	// allocate their own lowest free port.
	specs := []URLSpec{
		{Var: "BASE_URL", BaseURL: ptrURL("http://localhost:8000"), Range: [2]int{8000, 8099}},
		{Var: "DOCS_URL", BaseURL: ptrURL("http://localhost:9000"), Range: [2]int{9000, 9099}},
	}
	tracked := map[int]int{8000: 8001}
	taken := map[int]struct{}{8000: {}, 8001: {}, 9000: {}}
	got, err := AllocateURLs(specs, tracked, taken, nil)
	if err != nil {
		t.Fatalf("AllocateURLs = %v", err)
	}
	if got["BASE_URL"] != 8001 || got["DOCS_URL"] != 9001 {
		t.Errorf("got %v, want BASE_URL=8001 (tracked) DOCS_URL=9001 (allocated)", got)
	}
}

func TestTakenFromURLRecords_Union(t *testing.T) {
	recs := []WorktreeRecord{
		{Branch: "a", Urls: map[string]int{"APP_URL": 8000}},
		{Branch: "b", Urls: map[string]int{"APP_URL": 8001, "BASE_URL": 9000}},
		{Branch: "c"}, // no URLs
	}
	taken := TakenFromURLRecords(recs)
	for _, p := range []int{8000, 8001, 9000} {
		if _, ok := taken[p]; !ok {
			t.Errorf("taken missing %d", p)
		}
	}
	if _, ok := taken[8002]; ok {
		t.Errorf("taken should not contain 8002")
	}
}
