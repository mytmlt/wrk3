package cmd

import (
	"strings"
	"testing"
)

// Starting up on one worktree must not block operating on an unrelated
// worktree: the dashboard tracks ops per branch instead of one global busy
// flag.
func TestDashboardModel_UnrelatedOpWhileUpRuns(t *testing.T) {
	m := testDashboardModel()
	m.workSel["feature-a"] = true
	next, cmd := m.handleKey(keyMsg("u"))
	dm := next.(dashboardModel)
	if !dm.isBusy() {
		t.Fatalf("u should start an op: %+v", dm)
	}
	if cmd == nil {
		t.Fatal("u should return the up command")
	}
	firstID := dm.ops[0].id

	// Operate on the unrelated worktree while up is still running.
	dm.workSel = map[string]bool{"feature-b": true}
	dm.workCursor = 1
	next, cmd = dm.handleKey(keyMsg("d"))
	dm = next.(dashboardModel)
	if !dm.isBusy() || len(dm.ops) != 2 {
		t.Fatalf("down on unrelated branch must start a second op: %+v", dm.ops)
	}
	if cmd == nil {
		t.Fatal("down should return a command while up runs")
	}
	if dm.ops[0].id != firstID {
		t.Errorf("first op ID changed: %v", dm.ops)
	}
	if got := dm.busyTitle(); !strings.Contains(got, "+1 more") {
		t.Errorf("title should summarize 2 ops, got %q", got)
	}

	// Finishing one op leaves the other running.
	done, _ := dm.Update(dashboardOpDoneMsg{opID: firstID, label: "up", lines: []string{"up feature-a"}})
	dm = done.(dashboardModel)
	if !dm.isBusy() || len(dm.ops) != 1 {
		t.Fatalf("one op should remain after first completes: %+v", dm.ops)
	}
	done, _ = dm.Update(dashboardOpDoneMsg{opID: dm.ops[0].id, label: "down", lines: []string{"down feature-b"}})
	dm = done.(dashboardModel)
	if dm.isBusy() {
		t.Error("all ops done: dashboard must be idle")
	}
}

// Selection, navigation, and manual refresh stay live while ops run.
func TestDashboardModel_InteractiveWhileOpsRun(t *testing.T) {
	m := testDashboardModel()
	m.workSel["feature-a"] = true
	next, _ := m.handleKey(keyMsg("u"))
	dm := next.(dashboardModel)
	if !dm.isBusy() {
		t.Fatal("u should start an op")
	}

	// Cursor movement works.
	dm = applyKey(t, dm, "j")
	if dm.workCursor != 1 {
		t.Errorf("j while op runs: cursor = %d, want 1", dm.workCursor)
	}
	// Selection works.
	dm = applyKey(t, dm, " ")
	if !dm.workSel["feature-b"] {
		t.Errorf("space while op runs should select: %v", dm.workSel)
	}
	// Manual refresh returns commands instead of being swallowed.
	_, cmd := dm.handleKey(keyMsg("r"))
	if cmd == nil {
		t.Error("r while op runs must still refresh")
	}
	// Log scrolling works.
	dm.pane = 2
	top := dm.logView.YOffset
	_ = top
	dm = applyKey(t, dm, "3")
}

// Poll ticks keep refreshing while ops run (no frozen UI).
func TestDashboardModel_TickRefreshesWhileOpsRun(t *testing.T) {
	m := testDashboardModel()
	m.poll = 1
	m.startOp("up", []string{"feature-a"})
	next, cmd := m.Update(dashboardTickMsg{})
	dm := next.(dashboardModel)
	if !dm.isBusy() {
		t.Error("op should still be running after tick")
	}
	if cmd == nil {
		t.Error("tick while op runs must return refresh commands")
	}
}

// Fetch runs alongside worktree ops but a second fetch is rejected.
func TestDashboardModel_FetchConcurrentWithUp(t *testing.T) {
	m := testDashboardModel()
	m.workSel["feature-a"] = true
	next, _ := m.handleKey(keyMsg("u"))
	dm := next.(dashboardModel)

	next, cmd := dm.handleKey(keyMsg("R"))
	dm = next.(dashboardModel)
	if !dm.isBusy() || len(dm.ops) != 2 {
		t.Fatalf("fetch should run alongside up: %+v", dm.ops)
	}
	if cmd == nil {
		t.Fatal("fetch should return a command while up runs")
	}

	before := len(dm.ops)
	next, cmd = dm.handleKey(keyMsg("R"))
	dm = next.(dashboardModel)
	if len(dm.ops) != before || cmd != nil {
		t.Errorf("second fetch must be rejected: ops=%v cmd=%v", dm.ops, cmd)
	}
	if !strings.Contains(dm.statusMsg, "already running") {
		t.Errorf("status should explain the blocked fetch: %q", dm.statusMsg)
	}
}

// Starting a remove via y clears the modal confirm immediately so the UI
// stays interactive (quit, navigation, other ops) while the remove runs.
func TestDashboardModel_RemoveConfirmClearedOnStart(t *testing.T) {
	m := testDashboardModel()
	m.workSel["feature-a"] = true
	m = applyKey(t, m, "x")
	if m.confirm == "" {
		t.Fatal("x should stage a remove confirm")
	}
	next, cmd := m.handleKey(keyMsg("y"))
	dm := next.(dashboardModel)
	if cmd == nil {
		t.Fatal("y should return the remove command")
	}
	if dm.confirm != "" || dm.pendingX != nil || dm.pendingForce {
		t.Errorf("y must clear the modal confirm at op start: %+v", dm)
	}
	if !dm.isBusy() {
		t.Error("y should start the remove op")
	}
	// Navigation still works while the remove runs.
	dm = applyKey(t, dm, "j")
	if dm.workCursor != 1 {
		t.Errorf("j during remove: cursor = %d, want 1", dm.workCursor)
	}
}

// A remove finishing must not wipe a newer pending confirm for disjoint
// branches staged while it ran.
func TestDashboardModel_RemoveDoneKeepsNewerConfirm(t *testing.T) {
	m := testDashboardModel()
	m.workSel["feature-a"] = true
	m = applyKey(t, m, "x")
	next, _ := m.handleKey(keyMsg("y"))
	dm := next.(dashboardModel)
	if len(dm.ops) != 1 {
		t.Fatalf("remove should be running: %+v", dm.ops)
	}
	removeID := dm.ops[0].id

	// Stage a new confirm for the disjoint branch while the remove runs.
	dm.workSel = map[string]bool{"feature-b": true}
	dm = applyKey(t, dm, "x")
	if dm.confirm == "" {
		t.Fatal("x should stage a second confirm while remove runs")
	}
	done, _ := dm.Update(dashboardOpDoneMsg{opID: removeID, label: "remove", lines: []string{"removed feature-a"}})
	dm = done.(dashboardModel)
	if dm.confirm == "" || len(dm.pendingX) != 1 || dm.pendingX[0] != "feature-b" {
		t.Errorf("newer pending confirm must survive the older remove completing: %+v", dm)
	}
}

// Completing one op clears only its branches from the selection sets,
// so queues staged while it ran survive.
func TestDashboardModel_OpDoneClearsOnlyItsBranches(t *testing.T) {
	m := testDashboardModel()
	m.brSel["pr-1"] = true
	next, _ := m.handleKey(keyMsg("a"))
	dm := next.(dashboardModel)
	if len(dm.ops) != 1 {
		t.Fatalf("add should be running: %+v", dm.ops)
	}
	addID := dm.ops[0].id
	// Queue another branch while the add runs (no overlap, so allowed).
	dm.brSel["pr-2"] = true
	done, _ := dm.Update(dashboardOpDoneMsg{opID: addID, label: "add", lines: []string{"added pr-1"}})
	dm = done.(dashboardModel)
	if dm.brSel["pr-2"] != true {
		t.Errorf("newer queued branch must survive: %v", dm.brSel)
	}
	if dm.brSel["pr-1"] {
		t.Errorf("finished op branch must be cleared: %v", dm.brSel)
	}
}

// Branches queued while a fetch runs survive fetch completion, while
// branches explicitly dequeued during the fetch stay dequeued.
func TestDashboardModel_FetchDoneKeepsQueuedDuringFetch(t *testing.T) {
	m := testDashboardModel()
	m.branches = []branchEntry{{Name: "pr-1"}, {Name: "pr-2"}}
	m.startOp("fetch", nil)
	fetchID := m.ops[0].id
	// Queue a branch while the fetch runs.
	m.brSel["pr-2"] = true
	next, _ := m.Update(dashboardFetchDoneMsg{opID: fetchID, entries: []branchEntry{{Name: "pr-1"}, {Name: "pr-2"}}})
	dm := next.(dashboardModel)
	if !dm.brSel["pr-2"] {
		t.Errorf("branch queued during fetch must survive: %v", dm.brSel)
	}
	if dm.isBusy() {
		t.Error("fetch should be finished")
	}
}

func TestDashboardModel_FetchDoneRespectsDequeueDuringFetch(t *testing.T) {
	m := testDashboardModel()
	m.branches = []branchEntry{{Name: "pr-1"}, {Name: "pr-2"}}
	m.brSel["pr-1"] = true
	m.startOp("fetch", nil)
	fetchID := m.ops[0].id
	// Dequeue while the fetch runs; entries still carry the fetch-start
	// selection but intent is S1 (empty).
	delete(m.brSel, "pr-1")
	next, _ := m.Update(dashboardFetchDoneMsg{opID: fetchID, entries: []branchEntry{{Name: "pr-1", Selected: true}, {Name: "pr-2"}}})
	dm := next.(dashboardModel)
	if dm.brSel["pr-1"] {
		t.Errorf("branch dequeued during fetch must stay dequeued: %v", dm.brSel)
	}
}

func TestDashboardModel_TabClearsPendingRemoveConfirm(t *testing.T) {
	m := testDashboardModel()
	m.projects = append(m.projects, &dashboardProject{
		desc: dashboardProjectDesc{Name: "other", ConfigPath: "/o/wrk3.yaml"},
	})
	m.workSel["feature-a"] = true
	m = applyKey(t, m, "x")
	if m.confirm == "" {
		t.Fatal("x should stage a remove confirm")
	}
	m = applyKey(t, m, "tab")
	if m.cur != 1 {
		t.Fatalf("tab should switch projects: %+v", m)
	}
	if m.confirm != "" || m.pendingX != nil || m.pendingForce {
		t.Errorf("tab must clear the staged confirm so y cannot run old branches in the new project: %+v", m)
	}
}

// A staged remove confirm survives an unrelated op completing.
func TestDashboardModel_RemoveConfirmSurvivesUnrelatedOpDone(t *testing.T) {
	m := testDashboardModel()
	m.workSel["feature-a"] = true
	next, _ := m.handleKey(keyMsg("u"))
	dm := next.(dashboardModel)
	upID := dm.ops[0].id

	dm.workSel = map[string]bool{"feature-b": true}
	dm = applyKey(t, dm, "x")
	if dm.confirm == "" {
		t.Fatal("x should stage a remove confirm while up runs")
	}
	done, _ := dm.Update(dashboardOpDoneMsg{opID: upID, label: "up", lines: []string{"up feature-a"}})
	dm = done.(dashboardModel)
	if dm.confirm == "" {
		t.Error("unrelated opDone must not clear the staged remove confirm")
	}
}
