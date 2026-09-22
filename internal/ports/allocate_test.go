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
	got, err := AllocateURLs(nil, nil, nil)
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
	got, err := AllocateURLs(specs, nil, nil)
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
	got, err := AllocateURLs(specs, taken, nil)
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
	got, err := AllocateURLs(specs, taken, nil)
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
	got, err := AllocateURLs(specs, nil, isFree)
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
	got, err := AllocateURLs(specs, nil, nil)
	if err != nil {
		t.Fatalf("AllocateURLs = %v", err)
	}
	if got["APP_URL"] != 8000 || got["BASE_URL"] != 9000 {
		t.Errorf("got %v, want APP_URL=8000 BASE_URL=9000", got)
	}
}

func TestAllocateURLs_SameBaseSharesPort(t *testing.T) {
	specs := []URLSpec{
		{Var: "BASE_URL", BaseURL: ptrURL("http://localhost:8000"), Range: [2]int{8000, 8099}},
		{Var: "ALLOWED_WS_ORIGINS", BaseURL: ptrURL("http://localhost:8000"), Range: [2]int{8000, 8099}},
	}
	got, err := AllocateURLs(specs, nil, nil)
	if err != nil {
		t.Fatalf("AllocateURLs = %v", err)
	}
	if got["BASE_URL"] != 8000 || got["ALLOWED_WS_ORIGINS"] != 8000 {
		t.Errorf("got %v, want BASE_URL=ALLOWED_WS_ORIGINS=8000 (alias)", got)
	}
}

func TestAllocateURLs_AliasSkipsTakenTogether(t *testing.T) {
	specs := []URLSpec{
		{Var: "BASE_URL", BaseURL: ptrURL("http://localhost:8000"), Range: [2]int{8000, 8099}},
		{Var: "ALLOWED_WS_ORIGINS", BaseURL: ptrURL("http://localhost:8000"), Range: [2]int{8000, 8099}},
	}
	taken := map[int]struct{}{8000: {}, 8001: {}}
	got, err := AllocateURLs(specs, taken, nil)
	if err != nil {
		t.Fatalf("AllocateURLs = %v", err)
	}
	if got["BASE_URL"] != 8002 || got["ALLOWED_WS_ORIGINS"] != 8002 {
		t.Errorf("got %v, want BASE_URL=ALLOWED_WS_ORIGINS=8002", got)
	}
}

func TestAllocateURLs_DistinctGroupsAvoidCollision(t *testing.T) {
	specs := []URLSpec{
		{Var: "APP_URL", BaseURL: ptrURL("http://localhost:8000"), Range: [2]int{8000, 8099}},
		{Var: "API_URL", BaseURL: ptrURL("http://localhost:8001"), Range: [2]int{8001, 8099}},
	}
	taken := map[int]struct{}{8000: {}}
	got, err := AllocateURLs(specs, taken, nil)
	if err != nil {
		t.Fatalf("AllocateURLs = %v", err)
	}
	if got["APP_URL"] != 8001 {
		t.Errorf("APP_URL = %d, want 8001", got["APP_URL"])
	}
	if got["API_URL"] == got["APP_URL"] {
		t.Errorf("distinct groups must not share: APP_URL=API_URL=%d", got["APP_URL"])
	}
	if got["API_URL"] != 8002 {
		t.Errorf("API_URL = %d, want 8002 (8001 taken by alias group)", got["API_URL"])
	}
}

func TestAllocateURLs_HostPortCollisionAvoided(t *testing.T) {
	specs := []URLSpec{
		{Var: "APP_URL", BaseURL: ptrURL("http://localhost:8000"), Range: [2]int{8000, 8099}},
	}
	taken := map[int]struct{}{8000: {}, 8001: {}, 8002: {}}
	got, err := AllocateURLs(specs, taken, nil)
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
	_, err := AllocateURLs(specs, taken, nil)
	if err == nil {
		t.Fatal("AllocateURLs = nil, want exhaustion error")
	}
	if !strings.Contains(err.Error(), `"APP_URL"`) || !strings.Contains(err.Error(), "[8000,8001]") {
		t.Errorf("error %q should name var and range", err.Error())
	}
}

func TestHostBaseToPort_MapsBaseValues(t *testing.T) {
	base := map[string]int{"app": 8000, "public_api": 8000, "web": 9000}
	host := map[string]int{"app": 8001, "public_api": 8001, "web": 9001}
	got := HostBaseToPort(base, host)
	if got[8000] != 8001 || got[9000] != 9001 {
		t.Errorf("got %v, want {8000:8001, 9000:9001}", got)
	}
	if len(got) != 2 {
		t.Errorf("got %v, want exactly 2 base entries (aliases share)", got)
	}
}

func TestAllocateURLsWithHost_TracksHostPort(t *testing.T) {
	specs := []URLSpec{
		{Var: "BASE_URL", BaseURL: ptrURL("http://localhost:8000"), Range: [2]int{8000, 8099}},
		{Var: "ALLOWED_WS_ORIGINS", BaseURL: ptrURL("http://localhost:8000"), Range: [2]int{8000, 8099}},
	}
	// taken includes main (8000) and the fresh host allocation (8001):
	// without tracking the alias group would land on 8002.
	taken := map[int]struct{}{8000: {}, 8001: {}}
	got, err := AllocateURLsWithHost(specs, taken, map[int]int{8000: 8001}, nil)
	if err != nil {
		t.Fatalf("AllocateURLsWithHost = %v", err)
	}
	if got["BASE_URL"] != 8001 || got["ALLOWED_WS_ORIGINS"] != 8001 {
		t.Errorf("got %v, want BASE_URL=ALLOWED_WS_ORIGINS=8001 (tracked host port)", got)
	}
}

func TestAllocateURLsWithHost_UntrackedScansTaken(t *testing.T) {
	specs := []URLSpec{
		{Var: "DOCS_URL", BaseURL: ptrURL("http://localhost:9000"), Range: [2]int{9000, 9099}},
	}
	taken := map[int]struct{}{8000: {}, 8001: {}, 9000: {}}
	got, err := AllocateURLsWithHost(specs, taken, map[int]int{8000: 8001}, nil)
	if err != nil {
		t.Fatalf("AllocateURLsWithHost = %v", err)
	}
	if got["DOCS_URL"] != 9001 {
		t.Errorf("DOCS_URL = %d, want 9001 (no host base match, skip taken)", got["DOCS_URL"])
	}
}

func TestAllocateURLsWithHost_TrackedOutOfRangeFallsBack(t *testing.T) {
	specs := []URLSpec{
		{Var: "NARROW_URL", BaseURL: ptrURL("http://localhost:8000"), Range: [2]int{8000, 8001}},
	}
	// Host tracked port 8005 sits outside the URL range: fall back to scan.
	taken := map[int]struct{}{8000: {}, 8005: {}}
	got, err := AllocateURLsWithHost(specs, taken, map[int]int{8000: 8005}, nil)
	if err != nil {
		t.Fatalf("AllocateURLsWithHost = %v", err)
	}
	if got["NARROW_URL"] != 8001 {
		t.Errorf("NARROW_URL = %d, want 8001 (tracked 8005 out of range)", got["NARROW_URL"])
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
