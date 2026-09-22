package cmd

import (
	"strings"
	"testing"

	"github.com/mytmlt/wrk3/internal/ports"
)

func TestDashboardLogTabs_OpDoneRoutesConsoleAndSwitches(t *testing.T) {
	m := seedLogModel(t, testDashboardModel())
	m.startOp("up", []string{"feature-a"})
	done, _ := m.Update(dashboardOpDoneMsg{
		opID:    m.ops[0].id,
		label:   "up",
		lines:   []string{"[feature-a] up"},
		console: []string{"[up] setup output", "[up] compose output"},
	})
	dm := done.(dashboardModel)
	for _, want := range []string{"[up] setup output", "[up] compose output"} {
		found := false
		for _, l := range dm.console["feature-a"] {
			if l == want {
				found = true
			}
		}
		if !found {
			t.Errorf("console for feature-a missing %q: %v", want, dm.console["feature-a"])
		}
	}
	found := false
	for _, l := range dm.log {
		if strings.Contains(l, "[up] [feature-a] up") {
			found = true
		}
	}
	if !found {
		t.Errorf("event line missing from event log: %v", dm.log)
	}
}

func TestDashboardLogTabs_OpDoneWithoutConsole(t *testing.T) {
	m := seedLogModel(t, testDashboardModel())
	m.startOp("pull", []string{"feature-a"})
	done, _ := m.Update(dashboardOpDoneMsg{
		opID:  m.ops[0].id,
		label: "pull",
		lines: []string{"[feature-a] pulled"},
	})
	dm := done.(dashboardModel)
	// Console for feature-a must not contain pull output (no console lines sent).
	if len(dm.console["feature-a"]) != 30 {
		t.Errorf("event-only op must not touch console: %d lines", len(dm.console["feature-a"]))
	}
}

func TestDashboardLogTabs_ScrollEventLogAndConsole(t *testing.T) {
	m := seedLogModel(t, testDashboardModel())
	// Focus event log (pane 2).
	m = applyKey(t, m, "3")
	if m.pane != 2 {
		t.Fatalf("3: pane = %d, want 2", m.pane)
	}
	top := m.eventLogView.YOffset
	m = applyKey(t, m, "k")
	if m.eventLogView.YOffset >= top {
		t.Fatalf("k should scroll event log: %d -> %d", top, m.eventLogView.YOffset)
	}
	// Focus console (pane 3).
	m = applyKey(t, m, "4")
	if m.pane != 3 {
		t.Fatalf("4: pane = %d, want 3", m.pane)
	}
	cTop := m.consoleView.YOffset
	m = applyKey(t, m, "k")
	if m.consoleView.YOffset >= cTop {
		t.Errorf("k should scroll console: %d -> %d", cTop, m.consoleView.YOffset)
	}
}

func TestDashboardLogTabs_PaneShowsConsoleAndEventLog(t *testing.T) {
	m := dashboardViewModel(t)
	m.width, m.height = 140, 40
	out := m.View()
	for _, want := range []string{"Console", "Event Log", "feature-a", "dashboard started"} {
		if !strings.Contains(out, want) {
			t.Errorf("dashboard missing %q:\n%s", want, out)
		}
	}
}

func TestDashboardOpCmdStream_CapturesConsole(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	cfg.Entry.Reload = []string{"echo hello-console"}
	p := &dashboardProject{
		desc:   dashboardProjectDesc{Name: "myapp", ConfigPath: "/r/wrk3.yaml", Current: true},
		cfg:    cfg,
		src:    &pullTestSource{},
		base:   cfg.AbsWorktreeBase(),
		stateP: cfg.StatePath(),
	}
	dir := t.TempDir()
	rec := ports.WorktreeRecord{Branch: "feature-a", Slug: "feature-a", AbsPath: dir, Ports: map[string]int{"app": 8000}}
	msg := dashboardReloadCmd(p, 0, []ports.WorktreeRecord{rec})()
	done, ok := msg.(dashboardOpDoneMsg)
	if !ok {
		t.Fatalf("reload cmd returned %T, want dashboardOpDoneMsg", msg)
	}
	if done.err != nil {
		t.Fatalf("reload cmd err = %v", done.err)
	}
	joined := strings.Join(done.console, "\n")
	if !strings.Contains(joined, "hello-console") {
		t.Errorf("console missing entry output: %q", joined)
	}
}

func TestDashboardLogTabs_ConsolePerWorktreeFollowsCursor(t *testing.T) {
	m := testDashboardModel()
	m.console = map[string][]string{
		"feature-a": {"[up] output for feature-a"},
		"feature-b": {"[reload] output for feature-b"},
	}
	m.width, m.height = 140, 40
	m.workCursor = 0
	m.syncConsoleView()
	outA := m.logPane(60, 15)
	if !strings.Contains(outA, "feature-a") {
		t.Errorf("console for worktree 0 should show feature-a content:\n%s", outA)
	}
	if strings.Contains(outA, "feature-b") {
		t.Errorf("console for worktree 0 must not show feature-b content:\n%s", outA)
	}
	m.workCursor = 1
	m.syncConsoleView()
	outB := m.logPane(60, 15)
	if !strings.Contains(outB, "feature-b") {
		t.Errorf("console for worktree 1 should show feature-b content:\n%s", outB)
	}
	if strings.Contains(outB, "feature-a") {
		t.Errorf("console for worktree 1 must not show feature-a content:\n%s", outB)
	}
}

func TestDashboardLogTabs_MultiBranchConsoleRoutesToBoth(t *testing.T) {
	m := seedLogModel(t, testDashboardModel())
	m.startOp("up", []string{"feature-a", "feature-b"})
	done, _ := m.Update(dashboardOpDoneMsg{
		opID:    m.ops[0].id,
		label:   "up",
		console: []string{"[up] shared output"},
	})
	dm := done.(dashboardModel)
	for _, slug := range []string{"feature-a", "feature-b"} {
		found := false
		for _, l := range dm.console[slug] {
			if l == "[up] shared output" {
				found = true
			}
		}
		if !found {
			t.Errorf("slug %q missing shared console output: %v", slug, dm.console[slug])
		}
	}
}
