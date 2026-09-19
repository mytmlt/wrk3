package cmd

import (
	"os"
	"strings"
	"testing"

	"github.com/mytmlt/wrk3/internal/ports"
)

func TestDashboardKeys_HasEditEnv(t *testing.T) {
	keys := newDashboardKeys()
	if !keys.OpEditEnv.Enabled() {
		t.Error("OpEditEnv binding must be enabled")
	}
	found := false
	for _, b := range keys.ActHelp() {
		if b.Help().Key == "e" {
			found = true
		}
	}
	if !found {
		t.Error("ActHelp must include the edit-env (e) binding")
	}
	items := dashboardMenuItems()
	seen := false
	for _, it := range items {
		if it.Run == "e" {
			seen = true
		}
	}
	if !seen {
		t.Error("menu must list the edit-env (e) action")
	}
}

func TestDashboardModel_EditEnvKeyEmpty(t *testing.T) {
	m := testDashboardModel()
	m.rows = nil
	next, cmd := m.handleKey(keyMsg("e"))
	dm := next.(dashboardModel)
	if cmd != nil {
		t.Error("e with no worktrees must not return a command")
	}
	if dm.statusMsg == "" {
		t.Error("e with no worktrees should set a status message")
	}
}

func TestDashboardModel_EditEnvKeyStale(t *testing.T) {
	m := testDashboardModel()
	m.rows[0].Rec.AbsPath = "/nonexistent-wrk3-worktree"
	next, cmd := m.handleKey(keyMsg("e"))
	dm := next.(dashboardModel)
	if cmd != nil {
		t.Error("e on a stale worktree must not return a command")
	}
	if !strings.Contains(dm.statusMsg, "stale") {
		t.Errorf("status = %q, want a stale hint", dm.statusMsg)
	}
}

func TestDashboardModel_EditEnvKeyReturnsCmd(t *testing.T) {
	old := editorLookPath
	editorLookPath = func(string) (string, error) { return "/bin/vim", nil }
	defer func() { editorLookPath = old }()

	m := dashboardViewModel(t)
	dir := t.TempDir()
	m.rows[0].Rec.AbsPath = dir
	m.rows[0].Rec.Ports = map[string]int{"app": 8001}
	next, cmd := m.handleKey(keyMsg("e"))
	dm := next.(dashboardModel)
	if cmd == nil {
		t.Fatalf("e should return the edit command: status=%q", dm.statusMsg)
	}
	if !strings.Contains(dm.statusMsg, "editing") {
		t.Errorf("status = %q, want an editing hint", dm.statusMsg)
	}
	// .env must have been ensured before suspending.
	if _, err := os.Stat(envFilePath(dir)); err != nil {
		t.Errorf(".env not ensured: %v", err)
	}
}

func TestDashboardEnvDoneMsg_SuccessAndFailure(t *testing.T) {
	m := testDashboardModel()
	next, _ := m.Update(dashboardEnvDoneMsg{branch: "feature-a", path: "/w/.env"})
	dm := next.(dashboardModel)
	if !strings.Contains(dm.statusMsg, "edited feature-a") {
		t.Errorf("status = %q, want edited hint", dm.statusMsg)
	}
	next, _ = m.Update(dashboardEnvDoneMsg{branch: "feature-a", path: "/w/.env", err: os.ErrNotExist})
	dm = next.(dashboardModel)
	if !strings.Contains(dm.statusMsg, "edit .env") {
		t.Errorf("status = %q, want edit error hint", dm.statusMsg)
	}
}

func TestDashboardMenuItems_MatchBindingsIncludesEditEnv(t *testing.T) {
	items := dashboardMenuItems()
	seen := map[string]bool{}
	for _, it := range items {
		seen[it.Run] = true
	}
	if !seen["e"] {
		t.Error("menu missing run \"e\" (drift from newDashboardKeys)")
	}
	if len(items) == 0 || items[0].Run == "" {
		t.Error("menu must list dashboard actions")
	}
	_ = ports.EnvFileName
}
