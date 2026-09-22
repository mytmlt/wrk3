package cmd

import (
	"strings"
	"testing"

	"github.com/mytmlt/wrk3/internal/ports"
)

func TestDashboardLogTabs_Toggle(t *testing.T) {
	m := seedLogModel(t, testDashboardModel())
	if m.logTab != 0 {
		t.Fatalf("default tab = %d, want 0 (console)", m.logTab)
	}
	m = applyKey(t, m, "t")
	if m.logTab != 1 {
		t.Fatalf("t: tab = %d, want 1 (dashboard)", m.logTab)
	}
	m = applyKey(t, m, "t")
	if m.logTab != 0 {
		t.Fatalf("t again: tab = %d, want 0 (console)", m.logTab)
	}
	m = applyKey(t, m, "T")
	if m.logTab != 1 {
		t.Fatalf("T: tab = %d, want 1 (dashboard)", m.logTab)
	}
}

func TestDashboardLogTabs_OpDoneRoutesConsoleAndSwitches(t *testing.T) {
	m := seedLogModel(t, testDashboardModel())
	m.logTab = 1
	m.startOp("up", []string{"feature-a"})
	done, _ := m.Update(dashboardOpDoneMsg{
		opID:    m.ops[0].id,
		label:   "up",
		lines:   []string{"[feature-a] up"},
		console: []string{"[up] setup output", "[up] compose output"},
	})
	dm := done.(dashboardModel)
	if dm.logTab != 0 {
		t.Errorf("console output should flip to console tab, got %d", dm.logTab)
	}
	for _, want := range []string{"[up] setup output", "[up] compose output"} {
		found := false
		for _, l := range dm.console {
			if l == want {
				found = true
			}
		}
		if !found {
			t.Errorf("console missing %q: %v", want, dm.console)
		}
	}
	found := false
	for _, l := range dm.log {
		if strings.Contains(l, "[up] [feature-a] up") {
			found = true
		}
	}
	if !found {
		t.Errorf("event line missing from dashboard tab: %v", dm.log)
	}
}

func TestDashboardLogTabs_OpDoneWithoutConsoleKeepsTab(t *testing.T) {
	m := seedLogModel(t, testDashboardModel())
	m.logTab = 1
	m.startOp("pull", []string{"feature-a"})
	done, _ := m.Update(dashboardOpDoneMsg{
		opID:  m.ops[0].id,
		label: "pull",
		lines: []string{"[feature-a] pulled"},
	})
	dm := done.(dashboardModel)
	if dm.logTab != 1 {
		t.Errorf("event-only op must not flip tab, got %d", dm.logTab)
	}
	if len(dm.console) != len(m.console) {
		t.Errorf("event-only op must not touch console: %v", dm.console)
	}
}

func TestDashboardLogTabs_ScrollFollowsActiveTab(t *testing.T) {
	m := seedLogModel(t, testDashboardModel())
	m = applyKey(t, m, "3")
	if m.pane != 2 {
		t.Fatalf("3: pane = %d, want 2", m.pane)
	}
	m.logTab = 0
	top := m.consoleView.YOffset
	m = applyKey(t, m, "k")
	if m.consoleView.YOffset >= top {
		t.Fatalf("k should scroll console: %d -> %d", top, m.consoleView.YOffset)
	}
	if m.logView.YOffset == 0 && top > 0 {
		t.Errorf("dashboard view must not move on console scroll")
	}
	m.logTab = 1
	topDash := m.logView.YOffset
	m = applyKey(t, m, "k")
	if m.logView.YOffset >= topDash && topDash > 0 {
		t.Errorf("k should scroll dashboard tab: %d -> %d", topDash, m.logView.YOffset)
	}
}

func TestDashboardLogTabs_PaneShowsTabs(t *testing.T) {
	m := dashboardViewModel(t)
	m.width, m.height = 140, 40
	out := m.View()
	for _, want := range []string{"console (", "dashboard (", "t tab"} {
		if !strings.Contains(out, want) {
			t.Errorf("log pane missing %q:\n%s", want, out)
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
