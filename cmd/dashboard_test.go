package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mytmlt/wrk3/internal/ports"
	"github.com/mytmlt/wrk3/internal/project"
	"github.com/mytmlt/wrk3/internal/source"
	"github.com/mytmlt/wrk3/internal/telemetry"
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
	// Input order is the display order (displayBranches sorts: priority
	// first, then newest-first) — entries preserve it verbatim.
	entries := unregisteredBranchEntries(
		[]string{"pr-1", "pr-2", "feature-a", "feature/a", "main"},
		recs, checked, keep,
	)
	if len(entries) != 5 {
		t.Fatalf("got %d entries, want 5", len(entries))
	}
	// Order preserved.
	if entries[0].Name != "pr-1" || entries[1].Name != "pr-2" || entries[4].Name != "main" {
		t.Errorf("order not preserved: %v", entries)
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
	if byName["feature-a"].Selected {
		t.Error("selections for registered branches must be dropped")
	}
	if !byName["main"].Adoptable {
		t.Error("checked-out unregistered branch should be adoptable")
	}
	if !byName["main"].Selected {
		t.Error("selections for adoptable orphans should be kept")
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
	if got := nextAppPort(cfg, nil); got != 8001 {
		t.Errorf("empty state: got %d, want 8001 (8000 is the main checkout)", got)
	}
	// Index is decoupled: only taken ports block, not indexes.
	recs := []ports.WorktreeRecord{
		{Branch: "a", Index: 5, Ports: map[string]int{"app": 8001}},
		{Branch: "b", Index: 2, Ports: map[string]int{"app": 8002}},
	}
	if got := nextAppPort(cfg, recs); got != 8003 {
		t.Errorf("taken 8001-8002: got %d, want gap reuse 8003", got)
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

func TestDashboardProjectDescs_EmptyCurrentUsesRegistry(t *testing.T) {
	descs := dashboardProjectDescs("", "",
		[]string{"other", "myapp"}, []string{"/o/wrk3.yaml", "/r/wrk3.yaml"})
	if len(descs) != 2 {
		t.Fatalf("got %v, want 2 registry entries", descs)
	}
	if descs[0].Current || descs[1].Current {
		t.Errorf("no current path: none should be marked current: %v", descs)
	}
	if descs[0].Name != "myapp" || descs[1].Name != "other" {
		t.Errorf("sorted by name: %v", descs)
	}
}

type dashboardStubStore struct {
	projects []project.Project
}

func (s *dashboardStubStore) Touch(string, string) error { return nil }

func (s *dashboardStubStore) Get(name string) (*project.Project, error) {
	for i := range s.projects {
		if s.projects[i].Name == name {
			return &s.projects[i], nil
		}
	}
	return nil, fmt.Errorf("get project %q: %w", name, project.ErrNotFound)
}

func (s *dashboardStubStore) List() ([]project.Project, error) {
	return s.projects, nil
}

func stubProjectStore(t *testing.T, projects []project.Project) {
	t.Helper()
	old := newProjectStore
	newProjectStore = func() (project.Store, error) {
		return &dashboardStubStore{projects: projects}, nil
	}
	t.Cleanup(func() { newProjectStore = old })
}

func resetDashboardPathFlags(t *testing.T) {
	t.Helper()
	oldFile, oldProj := fileFlag, dashboardProjectArg
	fileFlag, dashboardProjectArg = "", ""
	t.Cleanup(func() {
		fileFlag, dashboardProjectArg = oldFile, oldProj
	})
}

func TestResolveDashboardInitialPath_FallsBackToRegistry(t *testing.T) {
	resetDashboardPathFlags(t)
	dir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	want := "/reg/wrk3.yaml"
	stubProjectStore(t, []project.Project{{Name: "myapp", ConfigPath: want}})
	got, err := resolveDashboardInitialPath(nil)
	if err != nil {
		t.Fatalf("resolveDashboardInitialPath: %v", err)
	}
	if got != want {
		t.Errorf("path = %q, want registry %q", got, want)
	}
}

func TestResolveDashboardInitialPath_NoYamlNoRegistry(t *testing.T) {
	resetDashboardPathFlags(t)
	dir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	stubProjectStore(t, nil)
	_, err = resolveDashboardInitialPath(nil)
	if err == nil {
		t.Fatal("expected error with no yaml and empty registry")
	}
	if !errors.Is(err, errNoConfig) {
		t.Errorf("err = %v, want errNoConfig", err)
	}
	if !telemetry.IsQuiet(err) {
		t.Error("missing-config usage error must be telemetry-quiet")
	}
}

func TestResolveDashboardInitialPath_LocalYamlWins(t *testing.T) {
	resetDashboardPathFlags(t)
	dir := t.TempDir()
	local := filepath.Join(dir, "wrk3.yaml")
	if err := os.WriteFile(local, []byte("project:\n  worktreeBase: .worktrees\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	stubProjectStore(t, []project.Project{{Name: "other", ConfigPath: "/other/wrk3.yaml"}})
	got, err := resolveDashboardInitialPath(nil)
	if err != nil {
		t.Fatalf("resolveDashboardInitialPath: %v", err)
	}
	if got != local {
		t.Errorf("path = %q, want local %q", got, local)
	}
}

func TestResolveDashboardInitialPath_ProjectFlag(t *testing.T) {
	resetDashboardPathFlags(t)
	dashboardProjectArg = "other"
	stubProjectStore(t, []project.Project{
		{Name: "myapp", ConfigPath: "/a/wrk3.yaml"},
		{Name: "other", ConfigPath: "/o/wrk3.yaml"},
	})
	got, err := resolveDashboardInitialPath(nil)
	if err != nil {
		t.Fatalf("resolveDashboardInitialPath: %v", err)
	}
	if got != "/o/wrk3.yaml" {
		t.Errorf("path = %q, want --project other", got)
	}
}

func TestResolveConfigPath_QuietUsageError(t *testing.T) {
	resetDashboardPathFlags(t)
	dir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	_, err = ResolveConfigPath()
	if !errors.Is(err, errNoConfig) {
		t.Errorf("ResolveConfigPath = %v, want errNoConfig", err)
	}
	if !telemetry.IsQuiet(err) {
		t.Error("ResolveConfigPath missing-config must be telemetry-quiet")
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
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
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
		{Rec: ports.WorktreeRecord{Branch: "feature-b", Slug: "feature-b", Index: 1}, Status: "stopped", Ports: "app=8001"},
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

func TestDashboardModel_OrphanAdoptable(t *testing.T) {
	m := testDashboardModel()
	m.branches = append(m.branches, branchEntry{Name: "orphan", CheckedOut: true, Adoptable: true})
	m.pane = 1
	m.brCursor = len(m.branches) - 1
	m = applyKey(t, m, " ")
	if !m.brSel["orphan"] {
		t.Error("orphan checked-out branch should be queueable for adopt")
	}
	out := m.buildBranchTable(60, 10, true).View()
	if !strings.Contains(out, "orphan") {
		t.Errorf("branch table should label adoptable orphans:\n%s", out)
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
	if !dm.isBusy() || dm.busyTitle() != "remove feature-a" {
		t.Errorf("y should start busy remove: %+v", dm)
	}
	if cmd == nil {
		t.Fatal("y should return the remove command")
	}
	// Complete the op without executing docker: feed opDone directly.
	done, _ := dm.Update(dashboardOpDoneMsg{opID: dm.ops[0].id, label: "remove", lines: []string{"removed feature-a"}})
	dm = done.(dashboardModel)
	if dm.isBusy() || len(dm.workSel) != 0 {
		t.Errorf("opDone should clear busy+selections: %+v", dm)
	}
}

func TestDashboardModel_ForceRemoveFlow(t *testing.T) {
	m := testDashboardModel()
	m.workSel["feature-a"] = true
	m = applyKey(t, m, "X")
	if m.confirm != "remove --force" || !m.pendingForce {
		t.Fatalf("X should stage force confirm: %+v", m)
	}
	if len(m.pendingX) != 1 || m.pendingX[0] != "feature-a" {
		t.Fatalf("X should stage feature-a: %+v", m)
	}
	next, cmd := m.handleKey(keyMsg("y"))
	dm := next.(dashboardModel)
	if !dm.isBusy() || dm.busyTitle() != "remove --force feature-a" {
		t.Errorf("X+y should start busy force remove: %+v", dm)
	}
	if cmd == nil {
		t.Fatal("X+y should return the remove command")
	}
	// Force label must clear selections like the clean remove.
	done, _ := dm.Update(dashboardOpDoneMsg{opID: dm.ops[0].id, label: "remove --force", lines: []string{"removed feature-a"}})
	dm = done.(dashboardModel)
	if dm.isBusy() || len(dm.workSel) != 0 {
		t.Errorf("X opDone should clear busy+selections: %+v", dm)
	}
	if dm.pendingForce {
		t.Error("X opDone should clear pendingForce")
	}
	// D is not a force-remove alias: it must stage nothing.
	d := testDashboardModel()
	d.workSel["feature-a"] = true
	d = applyKey(t, d, "D")
	if d.confirm != "" || d.pendingForce || len(d.pendingX) != 0 {
		t.Errorf("D should not stage a remove confirm: %+v", d)
	}
	// Lowercase x stays a clean remove.
	m = testDashboardModel()
	m.workSel["feature-a"] = true
	m = applyKey(t, m, "x")
	if m.confirm != "remove" || m.pendingForce {
		t.Errorf("x should stage clean confirm: %+v", m)
	}
	m = applyKey(t, m, "n")
	if m.confirm != "" || m.pendingForce {
		t.Errorf("n should cancel and clear force: %+v", m)
	}
}

func TestDashboardModel_AddRequiresQueuedBranches(t *testing.T) {
	m := testDashboardModel()
	m = applyKey(t, m, "a")
	if m.isBusy() {
		t.Error("a with empty queue must not start an op")
	}
	if m.statusMsg == "" {
		t.Error("a with empty queue should set a status message")
	}
	m.brSel["pr-1"] = true
	m = applyKey(t, m, "a")
	if !m.isBusy() || m.busyTitle() != "add pr-1" {
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
	m.log = []string{"dashboard started — r refresh, R fetch, ? menu"}
	return m
}

func TestDashboardView_TablesAndHelp(t *testing.T) {
	m := dashboardViewModel(t)
	out := m.View()
	for _, want := range []string{
		"wrk3 dashboard", "myapp",
		"Worktrees", "Branches", "Logs", "Details", "Projects",
		"SLUG", "STATUS", "STATE",
		"1 of ", "of 2",
		"feature-a", "feature-b", "pr-1", "pr-2",
		"running", "stopped", "registered",
		"space", "select", "quit", "menu", "dashboard started",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q:\n%s", want, out)
		}
	}
	for _, gone := range []string{"PORTS", "PROJECT", "REMOTE BRANCHES"} {
		if strings.Contains(out, gone) {
			t.Errorf("slim worktree list must not show %q:\n%s", gone, out)
		}
	}
	if strings.Contains(out, "truncated to 20") {
		t.Errorf("branch list must scroll via the table, not truncate:\n%s", out)
	}
	if strings.Contains(out, "enter run") {
		t.Errorf("menu popup must stay closed until ?: \n%s", out)
	}
}

func TestDashboardView_MenuPopup(t *testing.T) {
	m := dashboardViewModel(t)
	m.showMenu = true
	m.width, m.height = 100, 40
	out := m.View()
	for _, want := range []string{
		"Menu",
		"up selected worktrees", "down selected worktrees",
		"add queued branches", "force remove",
		"toggle mine filter", "toggle myprs filter",
		"switch project", "quit",
		"enter run", "esc close",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("menu popup missing %q:\n%s", want, out)
		}
	}
	// Panes hide behind the popup.
	for _, gone := range []string{"Worktrees", "Branches"} {
		if strings.Contains(out, gone) {
			t.Errorf("menu popup should replace panes, found %q:\n%s", gone, out)
		}
	}
}

func TestDashboardMenuItems_MatchBindings(t *testing.T) {
	items := dashboardMenuItems()
	if len(items) == 0 {
		t.Fatal("menu must list dashboard actions")
	}
	seen := map[string]bool{}
	for _, it := range items {
		if it.Key == "" || it.Desc == "" || it.Run == "" {
			t.Errorf("menu item must have key/desc/run: %+v", it)
		}
		if seen[it.Run] {
			t.Errorf("duplicate menu run %q", it.Run)
		}
		seen[it.Run] = true
	}
	// Every Run value must dispatch through the normal key handler.
	for _, run := range []string{"u", "d", "l", "p", "a", "o", "O", "e", "x", "X", "r", "R", "m", "P", "1", "2", "3", "tab", "q"} {
		if !seen[run] {
			t.Errorf("menu missing run %q (drift from newDashboardKeys)", run)
		}
	}
}

func TestDashboardModel_MenuOpenClose(t *testing.T) {
	m := testDashboardModel()
	m = applyKey(t, m, "?")
	if !m.showMenu {
		t.Fatalf("? should open the menu: %+v", m)
	}
	if out := m.menuPane(60); !strings.Contains(out, "Menu") {
		t.Errorf("menu pane should title Menu:\n%s", out)
	}
	// Raw op keys are swallowed while the menu is open.
	opened := m
	opened = applyKey(t, opened, "u")
	if opened.isBusy() {
		t.Error("u with menu open must not start an op")
	}
	if !opened.showMenu {
		t.Error("u with menu open must keep the menu open")
	}
	m = applyKey(t, m, "esc")
	if m.showMenu {
		t.Error("esc should close the menu")
	}
	m = applyKey(t, m, "?")
	m = applyKey(t, m, "?")
	if m.showMenu {
		t.Error("second ? should close the menu")
	}
	m = applyKey(t, m, "?")
	m = applyKey(t, m, "q")
	if m.showMenu {
		t.Error("q should close the menu, not quit")
	}
}

func TestDashboardModel_MenuNavigateWrap(t *testing.T) {
	m := testDashboardModel()
	m = applyKey(t, m, "?")
	n := len(dashboardMenuItems())
	m.menuCursor = 0
	m = applyKey(t, m, "k")
	if m.menuCursor != n-1 {
		t.Errorf("k at top should wrap to %d, got %d", n-1, m.menuCursor)
	}
	m = applyKey(t, m, "j")
	if m.menuCursor != 0 {
		t.Errorf("j at bottom should wrap to 0, got %d", m.menuCursor)
	}
	m = applyKey(t, m, "j")
	if m.menuCursor != 1 {
		t.Errorf("j should advance to 1, got %d", m.menuCursor)
	}
}

func menuCursorForRun(t *testing.T, run string) int {
	t.Helper()
	for i, it := range dashboardMenuItems() {
		if it.Run == run {
			return i
		}
	}
	t.Fatalf("menu has no run %q", run)
	return 0
}

func TestDashboardModel_MenuEnterRunsAction(t *testing.T) {
	// Pull via the menu: same busy op as pressing p directly.
	m := testDashboardModel()
	m = applyKey(t, m, "?")
	m.menuCursor = menuCursorForRun(t, "p")
	next, cmd := m.handleKey(keyMsg("enter"))
	dm := next.(dashboardModel)
	if dm.showMenu {
		t.Error("enter should close the menu")
	}
	if !dm.isBusy() || dm.busyTitle() != "pull feature-a" {
		t.Errorf("menu pull should start busy pull: %+v", dm)
	}
	if cmd == nil {
		t.Error("menu pull should return the pull command")
	}
	// Remove via the menu lands in the y/n confirm flow.
	m = testDashboardModel()
	m.workSel["feature-a"] = true
	m = applyKey(t, m, "?")
	m.menuCursor = menuCursorForRun(t, "x")
	next, cmd = m.handleKey(keyMsg("enter"))
	dm = next.(dashboardModel)
	if dm.showMenu {
		t.Error("enter should close the menu")
	}
	if dm.confirm != "remove" || cmd != nil {
		t.Errorf("menu remove should stage confirm (no cmd yet): %+v", dm)
	}
}

func TestDashboardModel_MenuBlockedByConfirm(t *testing.T) {
	m := testDashboardModel()
	m.workSel["feature-a"] = true
	m = applyKey(t, m, "x")
	if m.confirm == "" {
		t.Fatal("x should stage a remove confirm")
	}
	m = applyKey(t, m, "?")
	if m.showMenu {
		t.Error("? must not open the menu while a confirm is pending")
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
	for _, want := range []string{"wrk3 dashboard", "myapp", "Worktrees", "Branches", "Logs", "feature-a", "pr-1"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q:\n%s", want, out)
		}
	}
}

func TestProbeDashboardRows_StaleWithoutDocker(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	recs := []ports.WorktreeRecord{
		{Branch: "gone", Slug: "gone", Index: 3, Ports: map[string]int{"app": 8003}, Status: "running"},
	}
	rows := probeDashboardRows(&resolved{cfg: cfg}, recs, "")
	if len(rows) != 1 {
		t.Fatalf("got %d rows", len(rows))
	}
	if !rows[0].Stale || rows[0].Status != "stale" || rows[0].Ports != "?" {
		t.Errorf("missing dir must be stale/?: %+v", rows[0])
	}
}

func TestDashboardFetchDone_ClearsBusy(t *testing.T) {
	m := testDashboardModel()
	m.startOp("fetch", nil)
	fetchID := m.ops[0].id
	next, _ := m.Update(dashboardFetchDoneMsg{opID: fetchID, entries: []branchEntry{{Name: "pr-9"}}})
	dm := next.(dashboardModel)
	if dm.isBusy() {
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

func TestDashboardModel_PullKeyStartsOp(t *testing.T) {
	// Empty worktrees: p must not start an op.
	empty := testDashboardModel()
	empty.rows = nil
	empty = applyKey(t, empty, "p")
	if empty.isBusy() {
		t.Error("p with no worktrees must not start an op")
	}
	if empty.statusMsg == "" {
		t.Error("p with no worktrees should set a status message")
	}
	// Cursor worktree (no explicit selection): p starts a busy pull.
	m := testDashboardModel()
	next, cmd := m.handleKey(keyMsg("p"))
	dm := next.(dashboardModel)
	if !dm.isBusy() || dm.busyTitle() != "pull feature-a" {
		t.Fatalf("p should start busy pull: %+v", dm)
	}
	if cmd == nil {
		t.Fatal("p should return the pull command")
	}
	if dm.myprs {
		t.Error("p must not toggle the myprs filter (that is P)")
	}
	found := false
	for _, l := range dm.log {
		if strings.Contains(l, "pull feature-a") {
			found = true
		}
	}
	if !found {
		t.Errorf("pull target missing from log: %v", dm.log)
	}
	// Complete the op: feed opDone directly (no git here).
	done, _ := dm.Update(dashboardOpDoneMsg{opID: dm.ops[0].id, label: "pull", lines: []string{"[feature-a] pulled"}})
	dm = done.(dashboardModel)
	if dm.isBusy() {
		t.Error("pull opDone must clear busy")
	}
}

func TestDashboardModel_PullKeyWhileBusy(t *testing.T) {
	m := testDashboardModel()
	m.startOp("up", []string{"feature-a"})
	m = applyKey(t, m, "p")
	if len(m.ops) != 1 || m.busyTitle() != "up feature-a" {
		t.Errorf("p on the same branch while up runs must not start a second op: %+v", m.ops)
	}
	if !strings.Contains(m.statusMsg, "already running") {
		t.Errorf("same-branch conflict should explain the block: %q", m.statusMsg)
	}
}

func TestDashboardModel_MyPRSToggleUnaffectedByPull(t *testing.T) {
	m := testDashboardModel()
	m = applyKey(t, m, "P")
	if !m.myprs {
		t.Error("P should enable myprs filter")
	}
	m = applyKey(t, m, "P")
	if m.myprs {
		t.Error("P should toggle myprs filter back off")
	}
}

func TestDashboardPullCmd_RunsPullTargets(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	src := &pullTestSource{}
	p := &dashboardProject{
		desc:   dashboardProjectDesc{Name: "myapp", ConfigPath: "/r/wrk3.yaml", Current: true},
		cfg:    cfg,
		src:    src,
		base:   cfg.AbsWorktreeBase(),
		stateP: cfg.StatePath(),
	}
	dir := t.TempDir()
	targets := []ports.WorktreeRecord{{Branch: "feature-a", Slug: "feature-a", AbsPath: dir}}
	msg := dashboardPullCmd(p, 0, targets)()
	done, ok := msg.(dashboardOpDoneMsg)
	if !ok {
		t.Fatalf("pull cmd returned %T, want dashboardOpDoneMsg", msg)
	}
	if done.label != "pull" {
		t.Errorf("label = %q, want pull", done.label)
	}
	if done.err != nil {
		t.Fatalf("pull cmd err = %v", done.err)
	}
	if got := src.calls(); len(got) != 1 || got[0] != dir {
		t.Errorf("pulled = %v, want [%s]", got, dir)
	}
	src.mu.Lock()
	defer src.mu.Unlock()
	if len(src.pullOpts) != 1 || src.pullOpts[0] != (source.PullOptions{}) {
		t.Errorf("opts = %+v, want plain pull (no --rebase/--ff-only)", src.pullOpts)
	}
}

func TestDashboardCmd_DbAlias(t *testing.T) {
	found := false
	for _, a := range dashboardCmd.Aliases {
		if a == "db" {
			found = true
		}
	}
	if !found {
		t.Errorf("dashboard aliases = %v, want db shorthand", dashboardCmd.Aliases)
	}
}

func TestDashboardURLFor(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	rec := ports.WorktreeRecord{Branch: "feature-a", Slug: "feature-a", Index: 0, Ports: map[string]int{"app": 8000}}
	// Proxy disabled (test config default): plain localhost link.
	if got := dashboardURLFor(cfg, rec); got != "http://localhost:8000" {
		t.Errorf("disabled proxy: got %q, want localhost link", got)
	}
	// Proxy enabled: gateway URL wins over localhost.
	cfg.Proxy.Enabled = true
	cfg.Proxy.Domain = "localhost"
	cfg.Proxy.Addr = "127.0.0.1:8080"
	if got := dashboardURLFor(cfg, rec); got != "http://feature-a.localhost:8080" {
		t.Errorf("enabled proxy: got %q", got)
	}
	// Nil config falls back to localhost when an app port exists.
	if got := dashboardURLFor(nil, rec); got != "http://localhost:8000" {
		t.Errorf("nil cfg: got %q", got)
	}
	// No app port anywhere: unknown marker, never an empty link.
	bare := ports.WorktreeRecord{Branch: "x", Slug: "x"}
	if got := dashboardURLFor(cfg, bare); got != "http://x.localhost:8080" {
		t.Errorf("enabled proxy without ports: got %q", got)
	}
	cfg.Proxy.Enabled = false
	if got := dashboardURLFor(cfg, bare); got != "?" {
		t.Errorf("disabled proxy without ports: got %q, want ?", got)
	}
}

func TestDashboardProxyEnsure_DisabledNoop(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	if info, note := dashboardProxyEnsure(&resolved{cfg: cfg}); info != "" || note != "" {
		t.Errorf("disabled ensure = (%q,%q), want empty", info, note)
	}
	if info, note := dashboardProxyEnsure(nil); info != "" || note != "" {
		t.Errorf("nil ensure = (%q,%q), want empty", info, note)
	}
}

func TestDashboardModel_OpenKeyStartsOp(t *testing.T) {
	m := dashboardViewModel(t)
	m.rows[0].Rec.Ports = map[string]int{"app": 8000}
	next, cmd := m.handleKey(keyMsg("o"))
	dm := next.(dashboardModel)
	if !dm.isBusy() || dm.busyTitle() != "open" {
		t.Fatalf("o should start busy open: %+v", dm)
	}
	if cmd == nil {
		t.Fatal("o should return the open command (not executed here: it launches a browser)")
	}
	// Complete the op without launching a browser: feed opDone directly.
	done, _ := dm.Update(dashboardOpDoneMsg{opID: dm.ops[0].id, label: "open", lines: []string{"http://localhost:8000"}})
	dm = done.(dashboardModel)
	if dm.isBusy() {
		t.Error("open opDone must clear busy")
	}
	found := false
	for _, l := range dm.log {
		if strings.Contains(l, "http://localhost:8000") {
			found = true
		}
	}
	if !found {
		t.Errorf("open URL missing from log: %v", dm.log)
	}
}

func TestDashboardModel_OpenKeyUsesCursorOnly(t *testing.T) {
	// Space-select feature-a, move the cursor to feature-b: o must open
	// only the cursor row, never the stale selection.
	m := dashboardViewModel(t)
	m.rows[0].Rec.Ports = map[string]int{"app": 8000}
	m.rows[1].Rec.Ports = map[string]int{"app": 8001}
	m.workSel["feature-a"] = true
	m.workCursor = 1
	next, cmd := m.handleKey(keyMsg("o"))
	dm := next.(dashboardModel)
	if !dm.isBusy() || dm.busyTitle() != "open" {
		t.Fatalf("o should start busy open: %+v", dm)
	}
	if cmd == nil {
		t.Fatal("o should return the open command")
	}
	found := false
	for _, l := range dm.log {
		if strings.Contains(l, "open feature-b") {
			found = true
		}
		if strings.Contains(l, "open feature-a") {
			t.Errorf("o must ignore the space selection on feature-a: %v", dm.log)
		}
	}
	if !found {
		t.Errorf("o should log the cursor worktree feature-b: %v", dm.log)
	}
	// Empty rows: cursor-only open reports no worktree under cursor.
	empty := testDashboardModel()
	empty.rows = nil
	empty = applyKey(t, empty, "o")
	if empty.isBusy() {
		t.Error("o with no worktrees must not start an op")
	}
	if !strings.Contains(empty.statusMsg, "no worktree under cursor") {
		t.Errorf("status = %q, want a no-worktree-under-cursor hint", empty.statusMsg)
	}
}

func TestDashboardModel_CopyKeyUsesCursorOnly(t *testing.T) {
	oldFeed, oldLook := clipboardFeed, clipboardLookPath
	t.Cleanup(func() { clipboardFeed, clipboardLookPath = oldFeed, oldLook })
	clipboardLookPath = func(string) (string, error) { return "/usr/bin/pbcopy", nil }
	clipboardFeed = func(_ string, _ []string, text string) error { return nil }
	// Space-select feature-a, move the cursor to feature-b: O must copy
	// only the cursor row URL.
	m := dashboardViewModel(t)
	m.rows[0].Rec.Ports = map[string]int{"app": 8000}
	m.rows[1].Rec.Ports = map[string]int{"app": 8001}
	m.workSel["feature-a"] = true
	m.workCursor = 1
	_, cmd := m.handleKey(keyMsg("O"))
	if cmd == nil {
		t.Fatal("O should return the copy command")
	}
	msg, ok := cmd().(dashboardCopiedMsg)
	if !ok {
		t.Fatalf("copy cmd returned %T, want dashboardCopiedMsg", msg)
	}
	if msg.err != nil {
		t.Fatalf("copy cmd: %v", msg.err)
	}
	if msg.url != "http://localhost:8001" {
		t.Errorf("copy url = %q, want the cursor worktree URL http://localhost:8001", msg.url)
	}
}

func TestDashboardModel_CopyKeyCopiesURL(t *testing.T) {
	oldFeed, oldLook := clipboardFeed, clipboardLookPath
	t.Cleanup(func() { clipboardFeed, clipboardLookPath = oldFeed, oldLook })
	var copied string
	clipboardLookPath = func(string) (string, error) { return "/usr/bin/pbcopy", nil }
	clipboardFeed = func(_ string, _ []string, text string) error {
		copied = text
		return nil
	}
	m := dashboardViewModel(t)
	m.rows[0].Rec.Ports = map[string]int{"app": 8000}
	next, cmd := m.handleKey(keyMsg("O"))
	dm := next.(dashboardModel)
	if cmd == nil {
		t.Fatal("O should return the copy command")
	}
	msg, ok := cmd().(dashboardCopiedMsg)
	if !ok {
		t.Fatalf("copy cmd returned %T, want dashboardCopiedMsg", cmd())
	}
	if msg.err != nil {
		t.Fatalf("copy cmd: %v", msg.err)
	}
	if msg.url != "http://localhost:8000" {
		t.Errorf("copy url = %q, want http://localhost:8000", msg.url)
	}
	done, _ := dm.Update(msg)
	dm = done.(dashboardModel)
	found := false
	for _, l := range dm.log {
		if strings.Contains(l, "copied http://localhost:8000") {
			found = true
		}
	}
	if !found {
		t.Errorf("copied URL missing from log: %v", dm.log)
	}
	if copied != "http://localhost:8000" {
		t.Errorf("clipboard got %q, want http://localhost:8000", copied)
	}
}

func TestDashboardModel_CopyKeyEmpty(t *testing.T) {
	m := testDashboardModel()
	m.rows = nil
	next, cmd := m.handleKey(keyMsg("O"))
	dm := next.(dashboardModel)
	if cmd != nil {
		t.Error("O with no worktrees must not return a command")
	}
	if dm.statusMsg == "" {
		t.Error("O with no worktrees should set a status message")
	}
}

func TestDashboardModel_CopyKeyNoURL(t *testing.T) {
	m := dashboardViewModel(t)
	// Proxy disabled (test config default) and no app port: no copyable URL.
	m.rows[0].Rec.Ports = nil
	next, cmd := m.handleKey(keyMsg("O"))
	dm := next.(dashboardModel)
	if cmd != nil {
		t.Error("O with no URL must not return a command")
	}
	if !strings.Contains(dm.statusMsg, "no URL to copy") {
		t.Errorf("status = %q, want a no-URL hint", dm.statusMsg)
	}
}

func TestDashboardCopiedMsg_Failure(t *testing.T) {
	m := testDashboardModel()
	next, _ := m.Update(dashboardCopiedMsg{url: "http://localhost:8000", err: errors.New("no clipboard tool")})
	dm := next.(dashboardModel)
	if !strings.Contains(dm.statusMsg, "copy failed") {
		t.Errorf("status = %q, want a copy-failed hint", dm.statusMsg)
	}
	found := false
	for _, l := range dm.log {
		if strings.Contains(l, "http://localhost:8000") {
			found = true
		}
	}
	if !found {
		t.Errorf("failed URL missing from log (must stay manually copyable): %v", dm.log)
	}
}

func TestDashboardModel_OpenKeyEmpty(t *testing.T) {
	m := testDashboardModel()
	m.rows = nil
	m = applyKey(t, m, "o")
	if m.isBusy() {
		t.Error("o with no worktrees must not start an op")
	}
	if m.statusMsg == "" {
		t.Error("o with no worktrees should set a status message")
	}
}

func TestDashboardView_URLColumnAndProxyMeta(t *testing.T) {
	m := dashboardViewModel(t)
	m.rows[0].Rec.Ports = map[string]int{"app": 8000}
	m.rows[1].Rec.Ports = map[string]int{"app": 8001}
	m.width, m.height = 140, 40
	// Slim list shows slug+status; full URLs live in the DETAILS pane for
	// the cursor worktree.
	out := m.View()
	for _, want := range []string{"url:", "http://localhost:8000", "proxy off", "o opens URL"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q:\n%s", want, out)
		}
	}
	// Cursor on the second row previews its URL instead.
	m.workCursor = 1
	out = m.View()
	if !strings.Contains(out, "http://localhost:8001") {
		t.Errorf("cursor row URL missing:\n%s", out)
	}
	// Proxy enabled: gateway URLs + configured addr in the meta line.
	m.projects[0].cfg.Proxy.Enabled = true
	m.workCursor = 0
	out = m.View()
	for _, want := range []string{"http://feature-a.localhost:8080", "proxy 127.0.0.1:8080"} {
		if !strings.Contains(out, want) {
			t.Errorf("proxy view missing %q:\n%s", want, out)
		}
	}
}

func TestDashboardWorkColumns_FitsTableWidth(t *testing.T) {
	for _, w := range []int{76, 80, 115, 140, 200} {
		sum := 0
		for _, c := range dashboardWorkColumns(w) {
			sum += c.Width
		}
		// Rendered rows add 1 cell padding on each side per column
		// (bubbles/table), so the column widths must leave 6 spare.
		if sum+6 > w {
			t.Errorf("width %d: columns sum %d (+6 padding) overflows the table", w, sum)
		}
	}
}

func TestDashboardWorkColumnsFor_FitsContent(t *testing.T) {
	// Slim slug+status list: long slugs fit when wide, STATUS stays 11
	// for plain statuses.
	cells := []dashboardWorkCells{{
		slug:   "feat-sentry-intake-enrichment",
		status: "running",
	}}
	cols := dashboardWorkColumnsFor(200, cells)
	byTitle := map[string]int{}
	sum := 0
	for _, c := range cols {
		byTitle[c.Title] = c.Width
		sum += c.Width
	}
	if sum+6 > 200 {
		t.Errorf("columns sum %d (+6 padding) overflows width 200", sum)
	}
	if len(byTitle) != 3 {
		t.Errorf("slim list must have 3 columns (✓/SLUG/STATUS), got %v", byTitle)
	}
	if byTitle["SLUG"] < len("feat-sentry-intake-enrichment") {
		t.Errorf("SLUG width %d truncates content", byTitle["SLUG"])
	}
	if byTitle["STATUS"] != 11 {
		t.Errorf("STATUS width = %d, want fixed 11", byTitle["STATUS"])
	}
}

func TestDashboardWorkColumnsFor_ShrinksSlugFirst(t *testing.T) {
	long := strings.Repeat("x", 40)
	status := "running (degraded 1/2)"
	cells := []dashboardWorkCells{{
		slug: long, status: status,
	}}
	cols := dashboardWorkColumnsFor(30, cells)
	byTitle := map[string]int{}
	sum := 0
	for _, c := range cols {
		byTitle[c.Title] = c.Width
		sum += c.Width
	}
	if sum+6 > 30 {
		t.Fatalf("columns sum %d (+6 padding) overflows width 30", sum)
	}
	// Total need 3+40+22=65 in avail 30-6=24 (over 41): SLUG 40->8,
	// STATUS 22->13 (slug shrinks first, remainder comes off STATUS).
	if byTitle["✓"] != 3 {
		t.Errorf("✓ width = %d, want 3 (all: %v)", byTitle["✓"], byTitle)
	}
	if byTitle["SLUG"] != 8 {
		t.Errorf("SLUG width = %d, want 8 (all: %v)", byTitle["SLUG"], byTitle)
	}
	if byTitle["STATUS"] != 13 {
		t.Errorf("STATUS width = %d, want %d (all: %v)", byTitle["STATUS"], 13, byTitle)
	}
}

func TestDashboardLogViewportHeight_FortyPercent(t *testing.T) {
	for _, tc := range []struct {
		total, want int
	}{
		{40, 13}, // 40*40/100-3
		{24, 6},  // 24*40/100-3
		{0, 5},   // unset size falls back
		{-1, 5},
		{10, 5}, // clamped to the fallback minimum
	} {
		if got := dashboardLogViewportHeight(tc.total); got != tc.want {
			t.Errorf("height(%d) = %d, want %d", tc.total, got, tc.want)
		}
	}
}

func seedLogModel(t *testing.T, m dashboardModel) dashboardModel {
	t.Helper()
	lines := make([]string, 0, 30)
	for i := 0; i < 30; i++ {
		lines = append(lines, strings.Repeat("log line ", 20)+strings.Repeat("x", i))
	}
	m.log = lines
	m.logView.SetContent(strings.Join(wrapLogLines(lines, 60), "\n"))
	m.logView.GotoBottom()
	return m
}

func TestDashboardModel_LogPaneFocusAndScroll(t *testing.T) {
	m := seedLogModel(t, testDashboardModel())
	m = applyKey(t, m, "3")
	if m.pane != 2 {
		t.Fatalf("3: pane = %d, want 2 (logs)", m.pane)
	}
	top := m.logView.YOffset
	m = applyKey(t, m, "k")
	if m.logView.YOffset >= top {
		t.Errorf("k in log pane should scroll up: %d -> %d", top, m.logView.YOffset)
	}
	if m.workCursor != 0 || m.brCursor != 0 {
		t.Errorf("j/k in log pane must not move cursors: work=%d br=%d", m.workCursor, m.brCursor)
	}
	m = applyKey(t, m, "j")
	if m.logView.YOffset != top {
		t.Errorf("j in log pane should scroll back down to %d, got %d", top, m.logView.YOffset)
	}
	// Space is a no-op in the log pane.
	m = applyKey(t, m, " ")
	if len(m.workSel) != 0 || len(m.brSel) != 0 {
		t.Errorf("space in log pane must not select: %v %v", m.workSel, m.brSel)
	}
	// left/right cycles worktrees -> branches -> logs.
	m.pane = 0
	m = applyKey(t, m, "right")
	if m.pane != 1 {
		t.Errorf("right from 0: pane = %d, want 1", m.pane)
	}
	m = applyKey(t, m, "right")
	if m.pane != 2 {
		t.Errorf("right from 1: pane = %d, want 2", m.pane)
	}
	m = applyKey(t, m, "right")
	if m.pane != 0 {
		t.Errorf("right from 2: pane = %d, want 0 (wrap)", m.pane)
	}
	m = applyKey(t, m, "left")
	if m.pane != 2 {
		t.Errorf("left from 0: pane = %d, want 2 (wrap)", m.pane)
	}
}

func TestDashboardModel_DetailPane(t *testing.T) {
	m := testDashboardModel()
	out := m.detailPane(60, 10)
	for _, want := range []string{"Details", "feature-a", "slug:", "status:", "ports:", "url:", "path:", "project:"} {
		if !strings.Contains(out, want) {
			t.Errorf("detail pane missing %q:\n%s", want, out)
		}
	}
	// Selection wins over cursor.
	m.workCursor = 1
	m.workSel["feature-a"] = true
	if got := m.detailRecord(); got == nil || got.Rec.Branch != "feature-a" {
		t.Errorf("detail should follow selection, got %+v", got)
	}
	// Empty state placeholder.
	m.rows = nil
	if got := m.detailRecord(); got != nil {
		t.Errorf("empty rows: detailRecord = %+v, want nil", got)
	}
	if out := m.detailPane(60, 10); !strings.Contains(out, "no worktree") {
		t.Errorf("empty detail pane should placeholder:\n%s", out)
	}
}

func TestDashboardView_RedesignedLayout(t *testing.T) {
	for _, w := range []int{80, 140, 200} {
		m := dashboardViewModel(t)
		m.width, m.height = w, 40
		out := m.View()
		for _, want := range []string{
			"Worktrees", "Branches", "Details", "Logs", "Projects",
			"[1]-Worktrees", "[2]-Branches", "Details", "[3]-Logs", "Projects",
			"1 of ", "of 2", "of 3",
			"feature-a", "pr-1", "1/2/3",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("width %d: view missing %q:\n%s", w, want, out)
			}
		}
	}
	// Focused log pane is visibly marked.
	m := dashboardViewModel(t)
	m.width, m.height = 140, 40
	m.pane = 2
	if out := m.View(); !strings.Contains(out, "[3]-Logs (3") || !strings.Contains(out, "●") {
		t.Errorf("focused log pane should mark [3]-Logs (3) ●:\n%s", out)
	}
}

func TestDashboardCountLabel(t *testing.T) {
	for _, tc := range []struct {
		cursor, total int
		want          string
	}{
		{0, 0, "0 of 0"},
		{0, 1, "1 of 1"},
		{0, 3, "1 of 3"},
		{2, 3, "3 of 3"},
		{9, 3, "3 of 3"},
		{-1, 3, "1 of 3"},
	} {
		if got := dashboardCountLabel(tc.cursor, tc.total); got != tc.want {
			t.Errorf("count(%d,%d) = %q, want %q", tc.cursor, tc.total, got, tc.want)
		}
	}
}

func TestDashboardWorkStatusText_SlugHealthOnly(t *testing.T) {
	// Status already carries the health suffix ("running (healthy)");
	// Health is the per-check breakdown for DETAILS and must not be
	// appended again.
	row := dashboardRow{Status: "running (healthy)", Health: "api: pass, db: pass", Ports: "app=8000"}
	if got := dashboardWorkStatusText(row); got != "running (healthy)" {
		t.Errorf("status = %q, want passthrough without duplicated health", got)
	}
	if got := dashboardWorkStatusText(dashboardRow{Status: "stopped", Stale: true}); got != "stale" {
		t.Errorf("stale = %q, want stale", got)
	}
	if got := dashboardWorkStatusText(dashboardRow{}); got != "?" {
		t.Errorf("empty = %q, want ?", got)
	}
}

func TestDashboardView_ProjectPane(t *testing.T) {
	m := dashboardViewModel(t)
	m.width, m.height = 140, 40
	out := m.View()
	for _, want := range []string{"Projects", "myapp", "remote origin", "proxy off"} {
		if !strings.Contains(out, want) {
			t.Errorf("project pane missing %q:\n%s", want, out)
		}
	}
}
