package cmd

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mytmlt/wrk3/internal/ports"
	"github.com/mytmlt/wrk3/internal/source"
)

func TestMapDashboardRows_SortsAndMarksMain(t *testing.T) {
	recs := []ports.WorktreeRecord{
		{Branch: "feature-b", Slug: "feature-b", Index: 1},
		{Branch: "main", Slug: "main", Index: mainWorktreeIndex},
		{Branch: "feature-a", Slug: "feature-a", Index: 0},
	}
	rows := mapDashboardRows(recs, "main", func(r ports.WorktreeRecord) (string, string, bool) {
		return "running", "app=8000", false
	})
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}
	if rows[0].Rec.Branch != "feature-a" || rows[1].Rec.Branch != "feature-b" || rows[2].Rec.Branch != "main" {
		t.Errorf("not sorted by branch: %v", rows)
	}
	if !rows[2].IsMain {
		t.Error("main row not marked IsMain")
	}
	if rows[0].IsMain {
		t.Error("feature-a wrongly marked IsMain")
	}
	// Same branch name but managed index must not count as main.
	rows = mapDashboardRows(recs, "main", func(r ports.WorktreeRecord) (string, string, bool) {
		return "stopped", "?", true
	})
	if !rows[2].Stale || rows[2].Ports != "?" {
		t.Errorf("statusFn values not propagated: %+v", rows[2])
	}
}

func TestUnregisteredBranchEntries_SkipsRegisteredAndCheckedOut(t *testing.T) {
	recs := []ports.WorktreeRecord{
		{Branch: "feature-a", Slug: "feature-a"},
	}
	checked := map[string]bool{"main": true}
	keep := map[string]bool{"pr-1": true, "feature-a": true, "main": true}
	entries := unregisteredBranchEntries(
		[]string{"pr-2", "pr-1", "feature-a", "feature/a", "main"},
		recs, checked, keep,
	)
	if len(entries) != 5 {
		t.Fatalf("got %d entries, want 5", len(entries))
	}
	// Sorted.
	if entries[0].Name != "feature-a" || entries[1].Name != "feature/a" {
		t.Errorf("not sorted: %v", entries)
	}
	byName := map[string]branchEntry{}
	for _, e := range entries {
		byName[e.Name] = e
	}
	if !byName["feature-a"].Registered {
		t.Error("feature-a should be registered (branch match)")
	}
	if !byName["feature/a"].Registered {
		t.Error("feature/a should be registered (slug feature-a match)")
	}
	if !byName["main"].CheckedOut {
		t.Error("main should be checked out")
	}
	if !byName["pr-1"].Selected {
		t.Error("pr-1 selection should be kept")
	}
	if byName["feature-a"].Selected || byName["main"].Selected {
		t.Error("selections for registered/checked-out branches must be dropped")
	}
}

func TestCheckedOutSet_SkipsBareAndDetached(t *testing.T) {
	infos := []source.WorktreeInfo{
		{Path: "/a", Branch: "main"},
		{Path: "/b", Branch: ""},
		{Path: "/c", Branch: "x", Bare: true},
		{Path: "/d", Branch: "feature"},
	}
	got := checkedOutSet(infos)
	if len(got) != 2 || !got["main"] || !got["feature"] {
		t.Errorf("got %v, want {main feature}", got)
	}
}

func TestNextAppPort(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	if got := nextAppPort(cfg, nil); got != 8000 {
		t.Errorf("empty state: got %d, want 8000", got)
	}
	recs := []ports.WorktreeRecord{{Branch: "a", Index: 0}, {Branch: "b", Index: 2}}
	if got := nextAppPort(cfg, recs); got != 8300 {
		t.Errorf("max index 2: got %d, want 8300", got)
	}
	if got := nextAppPort(nil, nil); got != -1 {
		t.Errorf("nil cfg: got %d, want -1", got)
	}
}

func TestDashboardProjectDescs_CurrentFirstDedupe(t *testing.T) {
	descs := dashboardProjectDescs("myapp", "/r/wrk3.yaml",
		[]string{"myapp", "other"}, []string{"/r/wrk3.yaml", "/o/wrk3.yaml"})
	if len(descs) != 2 {
		t.Fatalf("got %v, want 2 entries", descs)
	}
	if !descs[0].Current || descs[0].ConfigPath != "/r/wrk3.yaml" {
		t.Errorf("current project must come first: %v", descs)
	}
	if descs[1].Name != "other" {
		t.Errorf("second must be other: %v", descs)
	}
}

func TestSelectedKeys(t *testing.T) {
	got := selectedKeys(map[string]bool{"b": true, "a": true, "c": false})
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("got %v, want [a b]", got)
	}
}

func keyMsg(s string) tea.KeyMsg {
	switch s {
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func testDashboardModel() dashboardModel {
	repo := ""
	_ = repo
	m := dashboardModel{
		projects: []*dashboardProject{
			{desc: dashboardProjectDesc{Name: "myapp", ConfigPath: "/r/wrk3.yaml", Current: true}},
		},
		workSel: map[string]bool{},
		brSel:   map[string]bool{},
		poll:    0,
	}
	m.rows = []dashboardRow{
		{Rec: ports.WorktreeRecord{Branch: "feature-a", Slug: "feature-a", Index: 0}, Status: "running", Ports: "app=8000"},
		{Rec: ports.WorktreeRecord{Branch: "feature-b", Slug: "feature-b", Index: 1}, Status: "stopped", Ports: "app=8100"},
	}
	m.branches = []branchEntry{
		{Name: "pr-1"},
		{Name: "pr-2"},
		{Name: "done", Registered: true},
	}
	return m
}

func applyKey(t *testing.T, m dashboardModel, key string) dashboardModel {
	t.Helper()
	next, _ := m.handleKey(keyMsg(key))
	dm, ok := next.(dashboardModel)
	if !ok {
		t.Fatalf("handleKey(%q) returned %T, want dashboardModel", key, next)
	}
	return dm
}

func TestDashboardModel_NavigationAndSelection(t *testing.T) {
	m := testDashboardModel()
	m = applyKey(t, m, "j")
	if m.workCursor != 1 {
		t.Errorf("j: cursor = %d, want 1", m.workCursor)
	}
	m = applyKey(t, m, "k")
	if m.workCursor != 0 {
		t.Errorf("k: cursor = %d, want 0", m.workCursor)
	}
	m = applyKey(t, m, " ")
	if !m.workSel["feature-a"] {
		t.Errorf("space should select feature-a: %v", m.workSel)
	}
	m = applyKey(t, m, "2")
	if m.pane != 1 {
		t.Errorf("2: pane = %d, want 1", m.pane)
	}
	m = applyKey(t, m, "j")
	if m.brCursor != 1 {
		t.Errorf("j in branch pane: cursor = %d, want 1", m.brCursor)
	}
	m = applyKey(t, m, " ")
	if !m.brSel["pr-2"] {
		t.Errorf("space should queue pr-2: %v", m.brSel)
	}
	// Registered branch cannot be queued.
	m.brCursor = 2
	m = applyKey(t, m, " ")
	if m.brSel["done"] {
		t.Error("registered branch must not be selectable")
	}
}

func TestDashboardModel_RowsMsgDropsVanishedSelections(t *testing.T) {
	m := testDashboardModel()
	m.workSel["feature-a"] = true
	m.workSel["gone"] = true
	m.workCursor = 5 // out of range after shrink
	next, _ := m.Update(dashboardRowsMsg{rows: []dashboardRow{m.rows[0]}})
	dm := next.(dashboardModel)
	if dm.workSel["gone"] {
		t.Errorf("vanished selection kept: %v", dm.workSel)
	}
	if !dm.workSel["feature-a"] {
		t.Errorf("live selection dropped: %v", dm.workSel)
	}
	if dm.workCursor != 0 {
		t.Errorf("cursor = %d, want clamped 0", dm.workCursor)
	}
}

func TestDashboardModel_ConfirmRemoveFlow(t *testing.T) {
	m := testDashboardModel()
	m.workSel["feature-a"] = true
	m = applyKey(t, m, "x")
	if m.confirm == "" || len(m.pendingX) != 1 || m.pendingX[0] != "feature-a" {
		t.Fatalf("x should stage confirm: %+v", m)
	}
	// Cancel path.
	m = applyKey(t, m, "n")
	if m.confirm != "" || m.pendingX != nil {
		t.Errorf("n should cancel confirm: %+v", m)
	}
	m = applyKey(t, m, "x")
	next, cmd := m.handleKey(keyMsg("y"))
	dm := next.(dashboardModel)
	if !dm.busy || dm.busyLabel != "remove" {
		t.Errorf("y should start busy remove: %+v", dm)
	}
	if cmd == nil {
		t.Fatal("y should return the remove command")
	}
	// Complete the op without executing docker: feed opDone directly.
	done, _ := dm.Update(dashboardOpDoneMsg{label: "remove", lines: []string{"removed feature-a"}})
	dm = done.(dashboardModel)
	if dm.busy || len(dm.workSel) != 0 {
		t.Errorf("opDone should clear busy+selections: %+v", dm)
	}
}

func TestDashboardModel_AddRequiresQueuedBranches(t *testing.T) {
	m := testDashboardModel()
	m = applyKey(t, m, "a")
	if m.busy {
		t.Error("a with empty queue must not start an op")
	}
	if m.statusMsg == "" {
		t.Error("a with empty queue should set a status message")
	}
	m.brSel["pr-1"] = true
	m = applyKey(t, m, "a")
	if !m.busy || m.busyLabel != "add" {
		t.Errorf("a with queue should start busy add: %+v", m)
	}
}

func TestDashboardModel_TabCyclesProjects(t *testing.T) {
	m := testDashboardModel()
	m.projects = append(m.projects, &dashboardProject{
		desc: dashboardProjectDesc{Name: "other", ConfigPath: "/o/wrk3.yaml"},
	})
	m = applyKey(t, m, "tab")
	if m.cur != 1 {
		t.Errorf("tab: cur = %d, want 1", m.cur)
	}
	if len(m.workSel) != 0 || len(m.brSel) != 0 {
		t.Error("project switch must clear selections")
	}
}

func TestDashboardKeys_ShortAndFullHelp(t *testing.T) {
	keys := newDashboardKeys()
	short := keys.ShortHelp()
	if len(short) == 0 {
		t.Fatal("ShortHelp must not be empty (sticky shortcut bar)")
	}
	for _, b := range short {
		if !b.Enabled() {
			t.Errorf("short-help binding %q must be enabled", b.Help().Key)
		}
	}
	full := keys.FullHelp()
	if len(full) != 4 {
		t.Errorf("FullHelp groups = %d, want 4", len(full))
	}
}

func dashboardViewModel(t *testing.T) dashboardModel {
	t.Helper()
	m := testDashboardModel()
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	m.projects[0].cfg = cfg
	m.projects[0].remote = "origin"
	m.keys = newDashboardKeys()
	m.log = []string{"dashboard started — r refresh, R fetch, ? help"}
	return m
}

func TestDashboardView_TablesAndHelp(t *testing.T) {
	m := dashboardViewModel(t)
	out := m.View()
	for _, want := range []string{
		"wrk3 dashboard", "myapp",
		"WORKTREES", "REMOTE BRANCHES", "LOG",
		"WORKTREE", "BRANCH", "STATUS", "PORTS", "PROJECT", "STATE",
		"feature-a", "feature-b", "pr-1", "pr-2",
		"running", "stopped", "registered",
		"space", "select", "quit", "dashboard started",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "truncated to 20") {
		t.Errorf("branch list must scroll via the table, not truncate:\n%s", out)
	}
}

func TestDashboardView_FullHelp(t *testing.T) {
	m := dashboardViewModel(t)
	m.showHelp = true
	m.help.ShowAll = true
	out := m.View()
	for _, want := range []string{"project", "myprs", "remove", "refresh"} {
		if !strings.Contains(out, want) {
			t.Errorf("full help missing %q:\n%s", want, out)
		}
	}
}

func TestDashboardView_WideAndNarrow(t *testing.T) {
	for _, w := range []int{80, 140, 200} {
		m := dashboardViewModel(t)
		m.width, m.height = w, 40
		out := m.View()
		if !strings.Contains(out, "feature-a") || !strings.Contains(out, "pr-1") {
			t.Errorf("width %d: view missing rows:\n%s", w, out)
		}
	}
}

func TestDashboardView_MainMarkerAndSelection(t *testing.T) {
	m := dashboardViewModel(t)
	m.rows = append(m.rows, dashboardRow{
		Rec:    ports.WorktreeRecord{Branch: "main", Slug: "main", Index: mainWorktreeIndex},
		Status: "running", Ports: "app=8000", IsMain: true,
	})
	m.workSel["feature-a"] = true
	out := m.View()
	for _, want := range []string{"(main)", "[x]"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q:\n%s", want, out)
		}
	}
}

func TestDashboardView_Smoke(t *testing.T) {
	m := testDashboardModel()
	// View needs a loaded project; stub a minimal one via temp config.
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	m.projects[0].cfg = cfg
	m.projects[0].remote = "origin"
	out := m.View()
	for _, want := range []string{"wrk3 dashboard", "myapp", "WORKTREES", "REMOTE BRANCHES", "LOG", "feature-a", "pr-1"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q:\n%s", want, out)
		}
	}
}

func TestProbeDashboardRows_StaleWithoutDocker(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	recs := []ports.WorktreeRecord{
		{Branch: "gone", Slug: "gone", Index: 3, Ports: map[string]int{"app": 8300}, Status: "running"},
	}
	rows := probeDashboardRows(cfg, recs, "")
	if len(rows) != 1 {
		t.Fatalf("got %d rows", len(rows))
	}
	if !rows[0].Stale || rows[0].Status != "stale" || rows[0].Ports != "?" {
		t.Errorf("missing dir must be stale/?: %+v", rows[0])
	}
}

func TestDashboardFetchDone_ClearsBusy(t *testing.T) {
	m := testDashboardModel()
	m.busy = true
	m.busyLabel = "fetch"
	next, _ := m.Update(dashboardFetchDoneMsg{entries: []branchEntry{{Name: "pr-9"}}})
	dm := next.(dashboardModel)
	if dm.busy {
		t.Error("fetchDone must clear busy")
	}
	if len(dm.branches) != 1 || dm.fetchedAt.IsZero() {
		t.Errorf("fetchDone must store branches+timestamp: %+v", dm)
	}
}

func TestDashboardModel_MineToggle(t *testing.T) {
	m := testDashboardModel()
	m = applyKey(t, m, "m")
	if !m.mine {
		t.Error("m should enable mine filter")
	}
}
