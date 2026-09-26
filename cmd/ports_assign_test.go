package cmd

import (
	"os"
	"testing"

	"github.com/mytmlt/wrk3/internal/ports"
)

// TestMain stubs the OS bind probe for hermetic unit tests: no network
// in cmd tests. Individual tests override osPortFree temporarily to
// simulate an occupied port.
func TestMain(m *testing.M) {
	osPortFree = func(int) bool { return true }
	os.Exit(m.Run())
}

func testAllocator() ports.Allocator {
	return ports.Allocator{
		Base:   map[string]int{"app": 8000},
		Ranges: map[string][2]int{"app": {8000, 8099}},
	}
}

func TestAssignPorts_GapReuse(t *testing.T) {
	alloc := testAllocator()
	recs := []ports.WorktreeRecord{
		{Branch: "a", Slug: "a", Index: 1, Ports: map[string]int{"app": 8001}},
		{Branch: "b", Slug: "b", Index: 2, Ports: map[string]int{"app": 8003}},
	}
	got, err := assignPorts(alloc, recs)
	if err != nil {
		t.Fatalf("assignPorts = %v", err)
	}
	// main holds 8000, a holds 8001, b holds 8003 -> lowest free is 8002.
	if got["app"] != 8002 {
		t.Errorf("app = %d, want gap reuse 8002", got["app"])
	}
}

func TestAssignPorts_SkipsOSOccupied(t *testing.T) {
	alloc := testAllocator()
	old := osPortFree
	osPortFree = func(p int) bool { return p != 8001 }
	defer func() { osPortFree = old }()
	got, err := assignPorts(alloc, nil)
	if err != nil {
		t.Fatalf("assignPorts = %v", err)
	}
	// 8000 taken by main reservation, 8001 OS-occupied -> 8002.
	if got["app"] != 8002 {
		t.Errorf("app = %d, want 8002 (main 8000, OS 8001)", got["app"])
	}
}

func TestAssignPorts_Exhaustion(t *testing.T) {
	alloc := ports.Allocator{
		Base:   map[string]int{"app": 8000},
		Ranges: map[string][2]int{"app": {8000, 8001}},
	}
	recs := []ports.WorktreeRecord{
		{Branch: "a", Slug: "a", Index: 1, Ports: map[string]int{"app": 8001}},
	}
	_, err := assignPorts(alloc, recs)
	if err == nil {
		t.Fatal("assignPorts = nil, want exhaustion (8000 main, 8001 taken)")
	}
}

func TestPreviewPorts_StateOnly(t *testing.T) {
	alloc := testAllocator()
	old := osPortFree
	osPortFree = func(p int) bool { return false }
	defer func() { osPortFree = old }()
	// preview ignores osPortFree (nil isFree): still finds 8001.
	got, err := previewPorts(alloc, nil)
	if err != nil {
		t.Fatalf("previewPorts = %v", err)
	}
	if got["app"] != 8001 {
		t.Errorf("app = %d, want 8001 state-only", got["app"])
	}
}
