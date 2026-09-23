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

func TestProbeDashboardRows_TransitionalStaysWithoutLiveContainers(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	dir := t.TempDir()
	recs := []ports.WorktreeRecord{
		{Branch: "wip", Slug: "wip", Index: 0, AbsPath: dir,
			Ports: map[string]int{"app": 8000}, Status: ports.StatusSettingUp},
		{Branch: "gone", Slug: "gone", Index: 1,
			AbsPath: filepath.Join(t.TempDir(), "missing"),
			Ports:   map[string]int{"app": 8001}, Status: ports.StatusSettingUp},
	}
	rows := probeDashboardRows(&resolved{cfg: cfg}, recs, "")
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
	base := t.TempDir()
	recs := []ports.WorktreeRecord{
		{Branch: "a", Slug: "a", AbsPath: filepath.Join(base, "a"), Index: 0, Status: ports.StatusStopped},
		{Branch: "b", Slug: "b", AbsPath: filepath.Join(base, "b"), Index: 1, Status: ports.StatusStopped},
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
	if !dm.isBusy() || dm.busyTitle() != "up feature-a" {
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

func TestDashboardRowsRefresh_KeepsInFlightMainSettingUp(t *testing.T) {
	// Regression: `u` on the implicit main worktree (no state-file entry,
	// synthesized Status "") flipped rows to "setting up", but the next
	// poll refresh overwrote it with the live "stopped" probe while the op
	// was still running (spinner on, status stopped).
	m := testDashboardModel()
	m.rows = []dashboardRow{
		{Rec: ports.WorktreeRecord{Branch: "main", Slug: "main", Index: mainWorktreeIndex}, Status: ports.StatusStopped},
	}
	m.workCursor = 0
	m.pane = 0
	m.projects[0].cfg = nil
	next, _ := m.handleKey(keyMsg("u"))
	dm := next.(dashboardModel)
	if dm.rows[0].Status != ports.StatusSettingUp {
		t.Fatalf("u must flip main to setting up immediately: %+v", dm.rows)
	}
	// Simulate a poll refresh arriving mid-up: fresh probe says stopped
	// (main has no stored status to win over the probe).
	refreshed := []dashboardRow{
		{Rec: ports.WorktreeRecord{Branch: "main", Slug: "main", Index: mainWorktreeIndex}, Status: ports.StatusStopped},
	}
	updated, _ := dm.Update(dashboardRowsMsg{rows: refreshed})
	dm = updated.(dashboardModel)
	if dm.rows[0].Status != ports.StatusSettingUp {
		t.Errorf("refresh during up main must keep setting up, got %q", dm.rows[0].Status)
	}
	// Completing the op clears the override: the next refresh shows live.
	done, _ := dm.Update(dashboardOpDoneMsg{opID: dm.ops[0].id, label: "up"})
	dm = done.(dashboardModel)
	if dm.isBusy() {
		t.Fatal("opDone must clear busy")
	}
	refreshedAfter := []dashboardRow{
		{Rec: ports.WorktreeRecord{Branch: "main", Slug: "main", Index: mainWorktreeIndex}, Status: ports.StatusStopped},
	}
	updated, _ = dm.Update(dashboardRowsMsg{rows: refreshedAfter})
	dm = updated.(dashboardModel)
	if dm.rows[0].Status != ports.StatusStopped {
		t.Errorf("refresh after up done must show live stopped, got %q", dm.rows[0].Status)
	}
}

func TestDashboardReapplyInFlightStatuses(t *testing.T) {
	m := testDashboardModel()
	m.rows = []dashboardRow{
		{Rec: ports.WorktreeRecord{Branch: "a", Slug: "a"}, Status: ports.StatusStopped},
		{Rec: ports.WorktreeRecord{Branch: "b", Slug: "b"}, Status: ports.StatusStopped},
		{Rec: ports.WorktreeRecord{Branch: "gone", Slug: "gone"}, Status: "stale", Stale: true},
	}
	m.startOp("up", []string{"a", "gone"})
	m.startOp("down", []string{"b"})
	m.startOp("pull", []string{"a"})
	m = m.reapplyInFlightStatuses()
	byBranch := map[string]dashboardRow{}
	for _, row := range m.rows {
		byBranch[row.Rec.Branch] = row
	}
	// up wins first for "a" (op order); pull carries no status.
	if byBranch["a"].Status != ports.StatusSettingUp {
		t.Errorf("up must reapply setting up to a, got %q", byBranch["a"].Status)
	}
	if byBranch["b"].Status != ports.StatusStopping {
		t.Errorf("down must reapply stopping to b, got %q", byBranch["b"].Status)
	}
	if byBranch["gone"].Status == ports.StatusSettingUp {
		t.Errorf("stale row must stay stale: %+v", byBranch["gone"])
	}
	// Ops from another project must not leak into the current view.
	m.ops[0].proj = m.cur + 1
	m.rows[0].Status = ports.StatusStopped
	m = m.reapplyInFlightStatuses()
	if m.rows[0].Status != ports.StatusStopped {
		t.Errorf("other-project op must not touch rows, got %q", m.rows[0].Status)
	}
}
