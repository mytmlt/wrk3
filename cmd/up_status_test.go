package cmd

import (
	"path/filepath"
	"testing"

	"github.com/mytmlt/wrk3/internal/ports"
)

func TestStoredStatusOverridesLive(t *testing.T) {
	for status, want := range map[string]bool{
		ports.StatusSettingUp: true,
		ports.StatusFailed:    true,
		ports.StatusRunning:   false,
		ports.StatusStopped:   false,
		"":                    false,
		"unknown":             false,
		"stale":               false,
	} {
		if got := ports.StoredStatusOverridesLive(status); got != want {
			t.Errorf("StoredStatusOverridesLive(%q) = %v, want %v", status, got, want)
		}
	}
}

func TestRowFor_TransitionalOverridesLive(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	dir := t.TempDir()
	for _, status := range []string{ports.StatusSettingUp, ports.StatusFailed} {
		rec := ports.WorktreeRecord{
			Branch: "feature-a", Slug: "feature-a",
			AbsPath: dir, Ports: map[string]int{"app": 8000}, Status: status,
		}
		got, portText := rowFor(cfg, rec)
		if got != status {
			t.Errorf("rowFor status %q = %q, want stored %q (must win over live probe)", status, got, status)
		}
		if portText != "app=8000" {
			t.Errorf("rowFor ports = %q, want app=8000", portText)
		}
	}
}

func TestRowFor_StaleWinsOverSettingUp(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	rec := ports.WorktreeRecord{
		Branch: "gone", Slug: "gone",
		AbsPath: filepath.Join(t.TempDir(), "missing"),
		Ports:   map[string]int{"app": 8000}, Status: ports.StatusSettingUp,
	}
	got, portText := rowFor(cfg, rec)
	if got != "stale" || portText != "?" {
		t.Errorf("missing dir must stay stale/?, got %q/%q", got, portText)
	}
}

func TestProbeDashboardRows_SettingUpSkipsDocker(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	dir := t.TempDir()
	recs := []ports.WorktreeRecord{
		{Branch: "wip", Slug: "wip", Index: 0, AbsPath: dir,
			Ports: map[string]int{"app": 8000}, Status: ports.StatusSettingUp},
		{Branch: "gone", Slug: "gone", Index: 1,
			AbsPath: filepath.Join(t.TempDir(), "missing"),
			Ports:   map[string]int{"app": 8100}, Status: ports.StatusSettingUp},
	}
	rows := probeDashboardRows(cfg, recs, "")
	byBranch := map[string]dashboardRow{}
	for _, r := range rows {
		byBranch[r.Rec.Branch] = r
	}
	if rows[0].Status == "" || byBranch["wip"].Status != ports.StatusSettingUp {
		t.Errorf("existing dir with stored setting up must show setting up: %+v", rows)
	}
	if byBranch["gone"].Status != "stale" || !byBranch["gone"].Stale {
		t.Errorf("missing dir must stay stale: %+v", byBranch["gone"])
	}
}

func TestMarkStatus_SettingUpFailedRoundtrip(t *testing.T) {
	stateP := filepath.Join(t.TempDir(), "state.json")
	r := &resolved{stateP: stateP}
	recs := []ports.WorktreeRecord{
		{Branch: "a", Slug: "a", AbsPath: "/tmp/a", Index: 0, Status: ports.StatusStopped},
		{Branch: "b", Slug: "b", AbsPath: "/tmp/b", Index: 1, Status: ports.StatusStopped},
	}
	if err := saveState(r, recs); err != nil {
		t.Fatal(err)
	}
	targets := []ports.WorktreeRecord{recs[0]}
	if err := markStatus(r, targets, ports.StatusSettingUp); err != nil {
		t.Fatal(err)
	}
	got, err := loadState(r)
	if err != nil {
		t.Fatal(err)
	}
	byBranch := map[string]string{}
	for _, rec := range got {
		byBranch[rec.Branch] = rec.Status
	}
	if byBranch["a"] != ports.StatusSettingUp || byBranch["b"] != ports.StatusStopped {
		t.Errorf("after setting up: %v", byBranch)
	}
	if err := markStatus(r, targets, ports.StatusFailed); err != nil {
		t.Fatal(err)
	}
	got, err = loadState(r)
	if err != nil {
		t.Fatal(err)
	}
	byBranch = map[string]string{}
	for _, rec := range got {
		byBranch[rec.Branch] = rec.Status
	}
	if byBranch["a"] != ports.StatusFailed || byBranch["b"] != ports.StatusStopped {
		t.Errorf("after failed: %v", byBranch)
	}
}

func TestDashboardMarkRowsSettingUp(t *testing.T) {
	m := testDashboardModel()
	m.rows = []dashboardRow{
		{Rec: ports.WorktreeRecord{Branch: "a", Slug: "a", Status: ports.StatusStopped}, Status: "stopped"},
		{Rec: ports.WorktreeRecord{Branch: "b", Slug: "b", Status: ports.StatusStopped}, Status: "stopped"},
		{Rec: ports.WorktreeRecord{Branch: "gone", Slug: "gone", Status: ports.StatusStopped}, Status: "stale", Stale: true},
	}
	targets := []ports.WorktreeRecord{
		{Branch: "a", Slug: "a"},
		{Branch: "gone", Slug: "gone"},
	}
	m = m.markRowsSettingUp(targets)
	byBranch := map[string]dashboardRow{}
	for _, row := range m.rows {
		byBranch[row.Rec.Branch] = row
	}
	if byBranch["a"].Status != ports.StatusSettingUp || byBranch["a"].Rec.Status != ports.StatusSettingUp {
		t.Errorf("selected row must flip to setting up: %+v", byBranch["a"])
	}
	if byBranch["b"].Status == ports.StatusSettingUp {
		t.Errorf("unselected row must not flip: %+v", byBranch["b"])
	}
	if byBranch["gone"].Status == ports.StatusSettingUp {
		t.Errorf("stale row must stay stale: %+v", byBranch["gone"])
	}
}

func TestDashboardUpKey_FlipsRowsImmediately(t *testing.T) {
	m := testDashboardModel()
	m.rows = []dashboardRow{
		{Rec: ports.WorktreeRecord{Branch: "feature-a", Slug: "feature-a", Status: ports.StatusStopped}, Status: "stopped"},
	}
	m.workCursor = 0
	m.pane = 0
	// Stub project so dashboardUpCmd construction does not nil-panic; the
	// returned tea.Cmd is not executed here (no docker).
	m.projects[0].cfg = nil
	next, cmd := m.handleKey(keyMsg("u"))
	dm := next.(dashboardModel)
	if !dm.busy || dm.busyLabel != "up" {
		t.Fatalf("u should start busy up: %+v", dm)
	}
	if cmd == nil {
		t.Fatal("u should return the up command")
	}
	if len(dm.rows) != 1 || dm.rows[0].Status != ports.StatusSettingUp {
		t.Errorf("u must flip rows to setting up immediately: %+v", dm.rows)
	}
}

func TestDashboardWorkColumns_StatusFitsSettingUp(t *testing.T) {
	for _, w := range []int{80, 140, 200} {
		var statusWidth int
		for _, c := range dashboardWorkColumns(w) {
			if c.Title == "STATUS" {
				statusWidth = c.Width
			}
		}
		if statusWidth < len(ports.StatusSettingUp) {
			t.Errorf("width %d: STATUS column %d truncates %q", w, statusWidth, ports.StatusSettingUp)
		}
	}
}
