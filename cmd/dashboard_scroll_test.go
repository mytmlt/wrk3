package cmd

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
	"github.com/mytmlt/wrk3/internal/ports"
)

// The worktree/branch tables are rebuilt from scratch on every View().
// A fresh bubbles/table starts with viewport YOffset 0, which used to
// leave the cursor one row below the visible window once it scrolled
// past the first page (cursor 22 rendered rows 13..21). These tests pin
// the cursor row visible for deep cursors in both panes.
func TestDashboardTables_KeepCursorVisible(t *testing.T) {
	m := testDashboardModel()
	m.branches = nil
	for i := 0; i < 30; i++ {
		m.branches = append(m.branches, branchEntry{Name: fmt.Sprintf("branch-%02d", i)})
	}
	for _, cursor := range []int{0, 5, 9, 22, 29} {
		m.brCursor = cursor
		out := m.buildBranchTable(60, 10, true).View()
		want := fmt.Sprintf("branch-%02d", cursor)
		if !strings.Contains(out, want) {
			t.Errorf("branch table cursor %d: row %q not visible:\n%s", cursor, want, out)
		}
	}
}

func TestDashboardWorkTable_KeepCursorVisible(t *testing.T) {
	m := testDashboardModel()
	m.rows = nil
	for i := 0; i < 30; i++ {
		name := fmt.Sprintf("w-%02d", i)
		m.rows = append(m.rows, dashboardRow{
			Rec:    ports.WorktreeRecord{Branch: name, Slug: name},
			Status: "stopped",
		})
	}
	for _, cursor := range []int{0, 5, 9, 22, 29} {
		m.workCursor = cursor
		out := m.buildWorkTable(60, 10, true).View()
		want := fmt.Sprintf("w-%02d", cursor)
		if !strings.Contains(out, want) {
			t.Errorf("work table cursor %d: row %q not visible:\n%s", cursor, want, out)
		}
	}
}

// Rendered table rows (including bubbles/table cell padding) must fit
// the pane inner width, or the table text overflows the box border and
// the focused branches pane looks wider than its neighbors.
func TestDashboardTables_FitPaneWidth(t *testing.T) {
	m := testDashboardModel()
	m.branches = nil
	for i := 0; i < 30; i++ {
		m.branches = append(m.branches, branchEntry{Name: fmt.Sprintf("branch-%02d-with-a-long-name-%02d", i, i)})
	}
	m.rows = nil
	for i := 0; i < 30; i++ {
		name := fmt.Sprintf("slug-%02d-with-a-long-name-%02d", i, i)
		m.rows = append(m.rows, dashboardRow{
			Rec:    ports.WorktreeRecord{Branch: name, Slug: name},
			Status: "running (degraded 1/2)",
		})
	}
	for _, w := range []int{40, 66, 100} {
		if got := dashboardVisibleWidth(m.buildBranchTable(w, 10, true).View()); got > w {
			t.Errorf("branch table width %d renders %d, overflows pane", w, got)
		}
		if got := dashboardVisibleWidth(m.buildWorkTable(w, 10, false).View()); got > w {
			t.Errorf("work table width %d renders %d, overflows pane", w, got)
		}
	}
}

// Every pane must fill its grid cell: short content (few projects, 8
// detail lines) used to leave its column shorter, so the logs box ended
// above the bottom with an empty gap and the columns misaligned.
func TestDashboardPanes_FillGridCells(t *testing.T) {
	m := testDashboardModel()
	m.width, m.height = 200, 50
	grid := computeDashboardGrid(m.width, m.height)
	workH := dashboardBoxHeight(m.worktreePane(grid.leftW, grid.workTableH))
	branchH := dashboardBoxHeight(m.branchPane(grid.leftW, grid.branchTableH))
	eventLogH := dashboardBoxHeight(m.eventLogPane(grid.leftW, grid.eventLogInnerH))
	detH := dashboardBoxHeight(m.detailPane(grid.rightW, grid.detInnerH))
	logH := dashboardBoxHeight(m.logPane(grid.rightW, grid.logH))
	if workH != grid.workTableH+3 {
		t.Errorf("worktrees box height = %d, want %d", workH, grid.workTableH+3)
	}
	if branchH != grid.branchTableH+3 {
		t.Errorf("branches box height = %d, want %d", branchH, grid.branchTableH+3)
	}
	if eventLogH != grid.eventLogInnerH+3 {
		t.Errorf("event log box height = %d, want %d", eventLogH, grid.eventLogInnerH+3)
	}
	if detH != grid.detInnerH+3 {
		t.Errorf("details box height = %d, want %d", detH, grid.detInnerH+3)
	}
	if logH != grid.logH {
		t.Errorf("logs box height = %d, want %d", logH, grid.logH)
	}
	if workH+branchH+eventLogH != detH+logH {
		t.Errorf("columns differ: left %d vs right %d", workH+branchH+eventLogH, detH+logH)
	}
	// Long detail lines (ports/health lists, abs paths) must truncate to
	// the pane instead of stretching its border wider than the logs box.
	m.rows = []dashboardRow{{
		Rec:    ports.WorktreeRecord{Branch: "b", Slug: "b", Ports: map[string]int{"app": 8000}},
		Status: "running",
		Health: strings.Repeat("container:x: pass, ", 20),
		Ports:  strings.Repeat("svc=8000,", 20),
	}}
	if dw, lw := dashboardVisibleWidth(m.detailPane(grid.rightW, grid.detInnerH)), dashboardVisibleWidth(m.logPane(grid.rightW, grid.logH)); dw != lw {
		t.Errorf("details width %d != logs width %d", dw, lw)
	}
}

func dashboardBoxHeight(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

func dashboardVisibleWidth(s string) int {
	maxW := 0
	for _, ln := range strings.Split(s, "\n") {
		if w := runewidth.StringWidth(stripDashboardANSI(ln)); w > maxW {
			maxW = w
		}
	}
	return maxW
}

func stripDashboardANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for i := 0; i < len(s); i++ {
		if !inEsc && s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			inEsc = true
			i++
			continue
		}
		if inEsc {
			if (s[i] >= 'A' && s[i] <= 'Z') || (s[i] >= 'a' && s[i] <= 'z') {
				inEsc = false
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
