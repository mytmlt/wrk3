package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mytmlt/wrk3/internal/ports"
)

func TestEffectiveReload_SkipsBlanks(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	cfg.Entry.Reload = []string{"", "  ", "echo hi"}
	r := &resolved{cfg: cfg}
	got := effectiveReload(r)
	if len(got) != 1 || got[0] != "echo hi" {
		t.Errorf("effectiveReload = %v, want [echo hi]", got)
	}
	if got := effectiveReload(nil); len(got) != 0 {
		t.Errorf("effectiveReload(nil) = %v, want empty", got)
	}
}

func TestRunReloadTargets_EmptyErrors(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	cfg.Entry.Reload = nil
	r := &resolved{cfg: cfg, stateP: filepath.Join(t.TempDir(), "state.json")}
	var logs []string
	err := runReloadTargets(context.Background(), r, nil, func(f string, a ...any) {
		logs = append(logs, f)
	})
	if err == nil || !strings.Contains(err.Error(), "entry.reload is not set") {
		t.Fatalf("err = %v, want entry.reload complaint", err)
	}
}

func TestReloadOne_EmptyErrors(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	cfg.Entry.Reload = []string{}
	r := &resolved{cfg: cfg, stateP: filepath.Join(t.TempDir(), "state.json")}
	rec := ports.WorktreeRecord{Branch: "a", Slug: "a", AbsPath: t.TempDir(), Ports: map[string]int{"app": 8000}}
	if err := reloadOne(context.Background(), r, rec, func(string, ...any) {}); err == nil ||
		!strings.Contains(err.Error(), "entry.reload is not set") {
		t.Fatalf("err = %v, want entry.reload complaint", err)
	}
}

func TestRunReloadTargets_EchoMarksRunning(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	cfg.Entry.Reload = []string{"echo reloaded"}
	dir := t.TempDir()
	rec := ports.WorktreeRecord{
		Branch: "feature-a", Slug: "feature-a", AbsPath: dir,
		Index: 0, Ports: map[string]int{"app": 8000}, Status: ports.StatusStopped,
	}
	stateP := filepath.Join(t.TempDir(), "state.json")
	r := &resolved{cfg: cfg, base: cfg.AbsWorktreeBase(), stateP: stateP}
	if err := saveState(r, []ports.WorktreeRecord{rec}); err != nil {
		t.Fatal(err)
	}
	var logs []string
	logf := func(f string, a ...any) {
		logs = append(logs, fmt.Sprintf(f, a...))
	}
	targets, err := loadState(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := runReloadTargets(context.Background(), r, targets, logf); err != nil {
		t.Fatalf("runReloadTargets = %v", err)
	}
	joined := strings.Join(logs, "\n")
	if !strings.Contains(joined, "reload: echo reloaded") || !strings.Contains(joined, "reloaded") {
		t.Errorf("logs missing reload lines: %v", logs)
	}
	got, err := loadState(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Status != ports.StatusRunning {
		t.Errorf("status = %+v, want running", got)
	}
	// .env must have been ensured for the worktree.
	if _, err := os.Stat(filepath.Join(dir, ".env")); err != nil {
		t.Errorf(".env not ensured: %v", err)
	}
}

func TestDashboardReloadKey_FlipsRowsImmediately(t *testing.T) {
	m := testDashboardModel()
	m.rows = []dashboardRow{
		{Rec: ports.WorktreeRecord{Branch: "feature-a", Slug: "feature-a", Status: ports.StatusStopped}, Status: "stopped"},
	}
	m.workCursor = 0
	m.pane = 0
	// Stub project so dashboardReloadCmd construction does not nil-panic; the
	// returned tea.Cmd is not executed here (no docker).
	m.projects[0].cfg = nil
	next, cmd := m.handleKey(keyMsg("l"))
	dm := next.(dashboardModel)
	if !dm.busy || dm.busyLabel != "reload" {
		t.Fatalf("l should start busy reload: %+v", dm)
	}
	if cmd == nil {
		t.Fatal("l should return the reload command")
	}
	if len(dm.rows) != 1 || dm.rows[0].Status != ports.StatusSettingUp {
		t.Errorf("l must flip rows to setting up immediately: %+v", dm.rows)
	}
}

func TestDashboardKeys_HasReload(t *testing.T) {
	keys := newDashboardKeys()
	if !keys.OpReload.Enabled() {
		t.Error("OpReload binding must be enabled")
	}
	found := false
	for _, b := range keys.ActHelp() {
		if b.Help().Key == "l" {
			found = true
		}
	}
	if !found {
		t.Error("ActHelp must include the reload (l) binding")
	}
}

func TestRunReloadTargets_FailureMarksFailed(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	cfg.Entry.Reload = []string{"exit 3"}
	dir := t.TempDir()
	rec := ports.WorktreeRecord{
		Branch: "feature-a", Slug: "feature-a", AbsPath: dir,
		Index: 0, Ports: map[string]int{"app": 8000}, Status: ports.StatusStopped,
	}
	stateP := filepath.Join(t.TempDir(), "state.json")
	r := &resolved{cfg: cfg, base: cfg.AbsWorktreeBase(), stateP: stateP}
	if err := saveState(r, []ports.WorktreeRecord{rec}); err != nil {
		t.Fatal(err)
	}
	targets, err := loadState(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := runReloadTargets(context.Background(), r, targets, func(string, ...any) {}); err == nil {
		t.Fatal("expected reload failure, got nil")
	}
	got, err := loadState(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Status != ports.StatusFailed {
		t.Errorf("status = %+v, want failed", got)
	}
}
