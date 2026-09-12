package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/mytmlt/wrk3/internal/config"
	"github.com/mytmlt/wrk3/internal/ports"
	"github.com/mytmlt/wrk3/internal/source"
)

var (
	dashboardPoll       time.Duration
	dashboardRemote     string
	dashboardMine       bool
	dashboardAuthors    []string
	dashboardMyPRS      bool
	dashboardProjectArg string
)

var dashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Interactive TUI: worktrees, branches, ports, up/down/add/remove",
	Long: `Open an interactive dashboard for this repo (plus registered projects).

Polls worktree state and remote branches, shows the worktree table with
ports, and runs up/down/add/remove without leaving the TUI. The CLI keeps
working alongside it: ` + "`wrk3 add`" + ` in another terminal shows up on
the next poll or manual refresh (r).`,
	ValidArgsFunction: cobra.NoFileCompletions,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) != 0 {
			return fmt.Errorf("dashboard takes no positional args")
		}
		initialPath, err := resolveDashboardInitialPath(cmd)
		if err != nil {
			return err
		}
		descs := loadDashboardDescs(initialPath)
		if len(descs) == 0 {
			return fmt.Errorf("no project available (could not load %q)", initialPath)
		}
		m := newDashboardModel(descs, dashboardPoll, dashboardRemote, dashboardMine, dashboardAuthors, dashboardMyPRS)
		p := tea.NewProgram(m, tea.WithAltScreen())
		_, err = p.Run()
		return err
	},
}

// resolveDashboardInitialPath honors --project NAME (registry) over -f/cwd.
func resolveDashboardInitialPath(cmd *cobra.Command) (string, error) {
	if dashboardProjectArg != "" {
		if fileFlag != "" {
			return "", fmt.Errorf("pass either --project or -f/--file, not both")
		}
		store, err := newProjectStore()
		if err != nil {
			return "", fmt.Errorf("open project registry: %w", err)
		}
		p, err := store.Get(dashboardProjectArg)
		if err != nil {
			return "", err
		}
		return p.ConfigPath, nil
	}
	_ = cmd
	return ResolveConfigPath()
}

// loadDashboardDescs merges the initial config with the registry list.
// Registry failures degrade to single-project mode, never fatal.
func loadDashboardDescs(initialPath string) []dashboardProjectDesc {
	currentName := ""
	if cfg, err := config.Load(initialPath); err == nil {
		currentName = filepath.Base(filepath.Clean(cfg.RepoPath()))
	}
	var names, paths []string
	if store, err := newProjectStore(); err == nil {
		if projects, err := store.List(); err == nil {
			for _, p := range projects {
				names = append(names, p.Name)
				paths = append(paths, p.ConfigPath)
			}
		}
	}
	return dashboardProjectDescs(currentName, initialPath, names, paths)
}

// dashboardProject is one loaded switchable project.
type dashboardProject struct {
	desc    dashboardProjectDesc
	cfg     *config.Config
	src     source.Source
	base    string
	stateP  string
	remote  string
	loadErr error
}

// loadDashboardProject loads config + source backend for a path.
// Interface-only: never imports concrete git/docker implementations.
func loadDashboardProject(desc dashboardProjectDesc, remoteOverride string) *dashboardProject {
	p := &dashboardProject{desc: desc}
	cfg, err := config.Load(desc.ConfigPath)
	if err != nil {
		p.loadErr = err
		return p
	}
	src, err := newSource(cfg)
	if err != nil {
		p.loadErr = err
		return p
	}
	p.cfg = cfg
	p.src = src
	p.base = cfg.AbsWorktreeBase()
	p.stateP = cfg.StatePath()
	if strings.TrimSpace(remoteOverride) != "" {
		p.remote = strings.TrimSpace(remoteOverride)
	} else {
		p.remote = cfg.EffectiveRemote()
	}
	return p
}

// dashboardModel is the BubbleTea model for the whole dashboard.
type dashboardModel struct {
	projects   []*dashboardProject
	cur        int
	rows       []dashboardRow
	branches   []branchEntry
	workCursor int
	brCursor   int
	pane       int // 0 = worktrees, 1 = branches
	workSel    map[string]bool
	brSel      map[string]bool
	log        []string
	statusMsg  string
	fetchedAt  time.Time
	busy       bool
	busyLabel  string
	confirm    string // pending confirm label, "" when none
	pendingX   []string
	mine       bool
	authors    []string
	myprs      bool
	poll       time.Duration
	width      int
	height     int
	spinner    spinner.Model
	showHelp   bool
}

func newDashboardModel(descs []dashboardProjectDesc, poll time.Duration, remote string, mine bool, authors []string, myprs bool) dashboardModel {
	projects := make([]*dashboardProject, 0, len(descs))
	for _, d := range descs {
		projects = append(projects, loadDashboardProject(d, remote))
	}
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	return dashboardModel{
		projects: projects,
		workSel:  map[string]bool{},
		brSel:    map[string]bool{},
		poll:     poll,
		mine:     mine,
		authors:  append([]string(nil), authors...),
		myprs:    myprs,
		spinner:  sp,
		log:      []string{"dashboard started — r refresh, R fetch, ? help"},
	}
}

func (m dashboardModel) curProject() *dashboardProject { return m.projects[m.cur] }

// Messages.
type dashboardTickMsg time.Time
type dashboardRowsMsg struct {
	rows []dashboardRow
	err  error
}
type dashboardBranchesMsg struct {
	entries []branchEntry
	err     error
}
type dashboardOpDoneMsg struct {
	label string
	lines []string
	err   error
}

func (m dashboardModel) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		dashboardRefreshRowsCmd(m.curProject()),
		dashboardFetchBranchesCmd(m.curProject(), m.mine, m.authors, m.myprs, m.brSel),
		dashboardTickCmd(m.poll),
	)
}

func dashboardTickCmd(poll time.Duration) tea.Cmd {
	if poll <= 0 {
		return nil
	}
	return tea.Tick(poll, func(t time.Time) tea.Msg { return dashboardTickMsg(t) })
}

// dashboardRefreshRowsCmd reloads state + main + live status concurrently.
func dashboardRefreshRowsCmd(p *dashboardProject) tea.Cmd {
	return func() tea.Msg {
		if p == nil || p.loadErr != nil || p.cfg == nil {
			err := fmt.Errorf("project not loaded")
			if p != nil && p.loadErr != nil {
				err = p.loadErr
			}
			return dashboardRowsMsg{err: err}
		}
		recs, err := ports.Load(p.stateP)
		if err != nil {
			return dashboardRowsMsg{err: fmt.Errorf("load state: %w", err)}
		}
		r := &resolved{cfg: p.cfg, src: p.src, base: p.base, stateP: p.stateP}
		all, err := recordsWithMain(r, recs)
		if err != nil {
			return dashboardRowsMsg{err: err}
		}
		main, _ := mainRecord(r, recs)
		mainBranch := ""
		if main != nil {
			mainBranch = main.Branch
		}
		rows := probeDashboardRows(p.cfg, all, mainBranch)
		return dashboardRowsMsg{rows: rows}
	}
}

// probeDashboardRows maps records to rows, probing live runner status in
// parallel. Missing dirs short-circuit to stale/? without docker calls.
func probeDashboardRows(cfg *config.Config, recs []ports.WorktreeRecord, mainBranch string) []dashboardRow {
	type cell struct {
		status string
		app    string
		stale  bool
	}
	cells := make([]cell, len(recs))
	g, _ := errgroup.WithContext(context.Background())
	for i, rec := range recs {
		i, rec := i, rec
		g.Go(func() error {
			if _, err := os.Stat(rec.AbsPath); err != nil {
				cells[i] = cell{status: "stale", app: "?", stale: true}
				return nil
			}
			app := portCell(rec.Ports, ports.PortApp)
			st, err := liveStatus(cfg, rec)
			if err != nil {
				if rec.Status != "" {
					cells[i] = cell{status: rec.Status, app: app}
				} else {
					cells[i] = cell{status: "unknown", app: app}
				}
				return nil
			}
			cells[i] = cell{status: st, app: app}
			return nil
		})
	}
	_ = g.Wait()
	rows := make([]dashboardRow, 0, len(recs))
	for i, rec := range recs {
		rows = append(rows, dashboardRow{
			Rec:    rec,
			Status: cells[i].status,
			App:    cells[i].app,
			Stale:  cells[i].stale,
			IsMain: mainBranch != "" && rec.Branch == mainBranch && rec.Index == mainWorktreeIndex,
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Rec.Branch < rows[j].Rec.Branch })
	return rows
}

// dashboardFetchBranchesCmd fetches the remote then lists refs.
func dashboardFetchBranchesCmd(p *dashboardProject, mine bool, authors []string, myprs bool, keepSel map[string]bool) tea.Cmd {
	keep := map[string]bool{}
	for k, v := range keepSel {
		keep[k] = v
	}
	return func() tea.Msg {
		if p == nil || p.loadErr != nil || p.cfg == nil {
			err := fmt.Errorf("project not loaded")
			if p != nil && p.loadErr != nil {
				err = p.loadErr
			}
			return dashboardBranchesMsg{err: err}
		}
		if err := p.src.Fetch(p.cfg.RepoPath(), p.remote); err != nil {
			return dashboardBranchesMsg{err: fmt.Errorf("fetch: %w", err)}
		}
		refs, err := dashboardBranchRefs(p.src, p.cfg.RepoPath(), p.remote, mine, authors, myprs)
		if err != nil {
			return dashboardBranchesMsg{err: fmt.Errorf("list refs: %w", err)}
		}
		recs, err := ports.Load(p.stateP)
		if err != nil {
			return dashboardBranchesMsg{err: fmt.Errorf("load state: %w", err)}
		}
		checked := map[string]bool{}
		if infos, err := p.src.List(p.cfg.RepoPath()); err == nil {
			checked = checkedOutSet(infos)
		}
		return dashboardBranchesMsg{entries: unregisteredBranchEntries(refs, recs, checked, keep)}
	}
}

// dashboardReloadBranchesCmd re-lists refs without fetching (fast poll path).
func dashboardReloadBranchesCmd(p *dashboardProject, mine bool, authors []string, myprs bool, keepSel map[string]bool) tea.Cmd {
	keep := map[string]bool{}
	for k, v := range keepSel {
		keep[k] = v
	}
	return func() tea.Msg {
		if p == nil || p.loadErr != nil || p.cfg == nil {
			return dashboardBranchesMsg{err: fmt.Errorf("project not loaded")}
		}
		refs, err := dashboardBranchRefs(p.src, p.cfg.RepoPath(), p.remote, mine, authors, myprs)
		if err != nil {
			return dashboardBranchesMsg{err: fmt.Errorf("list refs: %w", err)}
		}
		recs, err := ports.Load(p.stateP)
		if err != nil {
			return dashboardBranchesMsg{err: fmt.Errorf("load state: %w", err)}
		}
		checked := map[string]bool{}
		if infos, err := p.src.List(p.cfg.RepoPath()); err == nil {
			checked = checkedOutSet(infos)
		}
		return dashboardBranchesMsg{entries: unregisteredBranchEntries(refs, recs, checked, keep)}
	}
}

func (m dashboardModel) appendLog(line string) dashboardModel {
	m.log = append(m.log, line)
	if len(m.log) > 200 {
		m.log = m.log[len(m.log)-200:]
	}
	return m
}

// Op commands: up / down / add / remove over explicit selections.
func dashboardOpCmd(p *dashboardProject, label string, fn func(logf func(string, ...any)) error) tea.Cmd {
	return func() tea.Msg {
		var lines []string
		logf := func(format string, a ...any) {
			lines = append(lines, fmt.Sprintf(format, a...))
		}
		err := fn(logf)
		return dashboardOpDoneMsg{label: label, lines: lines, err: err}
	}
}

func dashboardUpCmd(p *dashboardProject, targets []ports.WorktreeRecord) tea.Cmd {
	return dashboardOpCmd(p, "up", func(logf func(string, ...any)) error {
		if p == nil || p.cfg == nil {
			return fmt.Errorf("project not loaded")
		}
		r := &resolved{cfg: p.cfg, src: p.src, base: p.base, stateP: p.stateP}
		g, ctx := errgroup.WithContext(context.Background())
		for _, rec := range targets {
			rec := rec
			g.Go(func() error { return upOne(ctx, r, rec, logf) })
		}
		if err := g.Wait(); err != nil {
			return err
		}
		return markStatus(r, targets, "running")
	})
}

func dashboardDownCmd(p *dashboardProject, targets []ports.WorktreeRecord) tea.Cmd {
	return dashboardOpCmd(p, "down", func(logf func(string, ...any)) error {
		if p == nil || p.cfg == nil {
			return fmt.Errorf("project not loaded")
		}
		r := &resolved{cfg: p.cfg, src: p.src, base: p.base, stateP: p.stateP}
		g, ctx := errgroup.WithContext(context.Background())
		for _, rec := range targets {
			rec := rec
			g.Go(func() error { return downOne(ctx, r, rec, logf) })
		}
		if err := g.Wait(); err != nil {
			return err
		}
		return markStatus(r, targets, "stopped")
	})
}

func dashboardAddCmd(p *dashboardProject, branches []string) tea.Cmd {
	return dashboardOpCmd(p, "add", func(logf func(string, ...any)) error {
		if p == nil || p.cfg == nil {
			return fmt.Errorf("project not loaded")
		}
		r := &resolved{cfg: p.cfg, src: p.src, base: p.base, stateP: p.stateP}
		if err := ensureBase(r.base); err != nil {
			return err
		}
		recs, err := loadState(r)
		if err != nil {
			return err
		}
		alloc := r.cfg.Allocator()
		for _, branch := range branches {
			if existing := findRecord(recs, branch); existing != nil {
				logf("skipped %s (already registered)", branch)
				continue
			}
			warns, err := addOne(r, &recs, &alloc, branch, p.remote)
			if err != nil {
				return err
			}
			for _, w := range warns {
				logf("warning: %s", w)
			}
			logf("added %s", branch)
		}
		return nil
	})
}

// dashboardRemoveCmd mirrors removeOne without cobra: compose down (warn and
// continue), strip managed .env keys, worktree remove, state cleanup.
// Refuses the implicit main checkout like `remove`.
func dashboardRemoveCmd(p *dashboardProject, branches []string) tea.Cmd {
	return dashboardOpCmd(p, "remove", func(logf func(string, ...any)) error {
		if p == nil || p.cfg == nil {
			return fmt.Errorf("project not loaded")
		}
		r := &resolved{cfg: p.cfg, src: p.src, base: p.base, stateP: p.stateP}
		for _, branch := range branches {
			recs, err := loadState(r)
			if err != nil {
				return err
			}
			rec := findRecord(recs, branch)
			if rec == nil {
				return fmt.Errorf("unknown worktree %q (see status)", branch)
			}
			if isMainPath(r, rec.AbsPath) {
				return fmt.Errorf("refusing to remove main worktree %q (repo root is always kept)", rec.Branch)
			}
			rn, err := newRunner(r.cfg, rec.Slug)
			if err != nil {
				return err
			}
			if err := rn.Down(context.Background(), rec.AbsPath, envFromPorts(rec.Ports)); err != nil {
				logf("warning: compose down for %q: %v", rec.Branch, err)
			}
			if _, err := ports.StripManaged(filepath.Join(rec.AbsPath, ports.EnvFileName), rec.Ports); err != nil {
				logf("warning: strip managed .env keys for %q: %v", rec.Branch, err)
			}
			if err := r.src.Remove(r.cfg.RepoPath(), rec.AbsPath, false); err != nil {
				return fmt.Errorf("remove worktree %q: %w", rec.Branch, err)
			}
			var kept []ports.WorktreeRecord
			for _, existing := range recs {
				if existing.Branch == rec.Branch {
					continue
				}
				kept = append(kept, existing)
			}
			if kept == nil {
				kept = []ports.WorktreeRecord{}
			}
			if err := saveState(r, kept); err != nil {
				return err
			}
			logf("removed %s", rec.Branch)
		}
		return nil
	})
}

// Update.
func (m dashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case dashboardTickMsg:
		if m.busy {
			return m, dashboardTickCmd(m.poll)
		}
		return m, tea.Batch(
			dashboardRefreshRowsCmd(m.curProject()),
			dashboardReloadBranchesCmd(m.curProject(), m.mine, m.authors, m.myprs, m.brSel),
			dashboardTickCmd(m.poll),
		)
	case dashboardRowsMsg:
		if msg.err != nil {
			m.statusMsg = msg.err.Error()
			m = m.appendLog("refresh: " + msg.err.Error())
		} else {
			m.rows = msg.rows
			// Drop selections for vanished worktrees.
			alive := map[string]bool{}
			for _, row := range m.rows {
				alive[row.Rec.Branch] = true
			}
			for k := range m.workSel {
				if !alive[k] {
					delete(m.workSel, k)
				}
			}
			if m.workCursor >= len(m.rows) {
				m.workCursor = max(0, len(m.rows)-1)
			}
			m.statusMsg = ""
		}
		return m, nil
	case dashboardBranchesMsg:
		if msg.err != nil {
			m.statusMsg = msg.err.Error()
			m = m.appendLog("branches: " + msg.err.Error())
		} else {
			prev := m.brSel
			m.branches = msg.entries
			// Re-apply kept selections (entries carry Selected already).
			m.brSel = map[string]bool{}
			for _, e := range m.branches {
				if e.Selected {
					m.brSel[e.Name] = true
				}
			}
			for k, v := range prev {
				if v {
					for _, e := range m.branches {
						if e.Name == k && !e.Registered && !e.CheckedOut {
							m.brSel[k] = true
						}
					}
				}
			}
			m.fetchedAt = time.Now()
			if m.brCursor >= len(m.branches) {
				m.brCursor = max(0, len(m.branches)-1)
			}
		}
		return m, nil
	case dashboardOpDoneMsg:
		m.busy = false
		m.busyLabel = ""
		m.confirm = ""
		m.pendingX = nil
		for _, l := range msg.lines {
			m = m.appendLog("[" + msg.label + "] " + l)
		}
		if msg.err != nil {
			m.statusMsg = msg.label + ": " + msg.err.Error()
			m = m.appendLog("error: " + msg.err.Error())
		} else {
			m.statusMsg = msg.label + " done"
			// Clear branch queue after successful add.
			if msg.label == "add" {
				m.brSel = map[string]bool{}
			}
			if msg.label == "remove" {
				m.workSel = map[string]bool{}
			}
		}
		return m, tea.Batch(
			dashboardRefreshRowsCmd(m.curProject()),
			dashboardReloadBranchesCmd(m.curProject(), m.mine, m.authors, m.myprs, m.brSel),
		)
	case dashboardFetchDoneMsg:
		m.busy = false
		m.busyLabel = ""
		m.branches = msg.entries
		m.fetchedAt = time.Now()
		m.brSel = map[string]bool{}
		for _, e := range m.branches {
			if e.Selected {
				m.brSel[e.Name] = true
			}
		}
		if m.brCursor >= len(m.branches) {
			m.brCursor = max(0, len(m.branches)-1)
		}
		m.statusMsg = "fetch done"
		m = m.appendLog("fetch done")
		return m, dashboardRefreshRowsCmd(m.curProject())
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m dashboardModel) selectedWorktrees() []ports.WorktreeRecord {
	var out []ports.WorktreeRecord
	for _, row := range m.rows {
		if m.workSel[row.Rec.Branch] {
			out = append(out, row.Rec)
		}
	}
	if len(out) == 0 && m.pane == 0 && len(m.rows) > 0 && m.workCursor < len(m.rows) {
		return []ports.WorktreeRecord{m.rows[m.workCursor].Rec}
	}
	return out
}

func (m dashboardModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Pending remove confirm.
	if m.confirm != "" {
		switch msg.String() {
		case "y", "Y":
			targets := append([]string(nil), m.pendingX...)
			m.busy = true
			m.busyLabel = "remove"
			m = m.appendLog("remove " + strings.Join(targets, ", "))
			return m, dashboardRemoveCmd(m.curProject(), targets)
		case "n", "N", "esc":
			m.confirm = ""
			m.pendingX = nil
			m.statusMsg = "remove cancelled"
			return m, nil
		}
		return m, nil
	}
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "?":
		m.showHelp = !m.showHelp
		return m, nil
	case "tab":
		if len(m.projects) > 1 {
			m.cur = (m.cur + 1) % len(m.projects)
			m.rows = nil
			m.branches = nil
			m.workSel = map[string]bool{}
			m.brSel = map[string]bool{}
			m.workCursor = 0
			m.brCursor = 0
			m.statusMsg = ""
			m = m.appendLog("switched to " + m.curProject().desc.Name)
			return m, tea.Batch(
				dashboardRefreshRowsCmd(m.curProject()),
				dashboardFetchBranchesCmd(m.curProject(), m.mine, m.authors, m.myprs, m.brSel),
			)
		}
		return m, nil
	case "1":
		m.pane = 0
		return m, nil
	case "2":
		m.pane = 1
		return m, nil
	case "left", "right":
		if m.pane == 0 {
			m.pane = 1
		} else {
			m.pane = 0
		}
		return m, nil
	case "up", "k":
		if m.pane == 0 && m.workCursor > 0 {
			m.workCursor--
		} else if m.pane == 1 && m.brCursor > 0 {
			m.brCursor--
		}
		return m, nil
	case "down", "j":
		if m.pane == 0 && m.workCursor < len(m.rows)-1 {
			m.workCursor++
		} else if m.pane == 1 && m.brCursor < len(m.branches)-1 {
			m.brCursor++
		}
		return m, nil
	case " ":
		if m.busy {
			return m, nil
		}
		if m.pane == 0 {
			if len(m.rows) > 0 && m.workCursor < len(m.rows) {
				b := m.rows[m.workCursor].Rec.Branch
				m.workSel[b] = !m.workSel[b]
			}
		} else {
			if len(m.branches) > 0 && m.brCursor < len(m.branches) {
				e := m.branches[m.brCursor]
				if !e.Registered && !e.CheckedOut {
					m.brSel[e.Name] = !m.brSel[e.Name]
					// Sync entry flag for render.
					m.branches[m.brCursor].Selected = m.brSel[e.Name]
				}
			}
		}
		return m, nil
	case "r":
		m.statusMsg = "refreshing…"
		return m, tea.Batch(
			dashboardRefreshRowsCmd(m.curProject()),
			dashboardReloadBranchesCmd(m.curProject(), m.mine, m.authors, m.myprs, m.brSel),
		)
	case "R":
		if m.busy {
			return m, nil
		}
		m.busy = true
		m.busyLabel = "fetch"
		m = m.appendLog("fetch " + m.curProject().remote + "…")
		return m, dashboardFetchOpCmd(m.curProject(), m.mine, m.authors, m.myprs, m.brSel)
	case "m":
		m.mine = !m.mine
		m.statusMsg = "mine filter " + map[bool]string{true: "on", false: "off"}[m.mine]
		return m, dashboardReloadBranchesCmd(m.curProject(), m.mine, m.authors, m.myprs, m.brSel)
	case "P":
		m.myprs = !m.myprs
		m.statusMsg = "myprs filter " + map[bool]string{true: "on", false: "off"}[m.myprs]
		return m, dashboardReloadBranchesCmd(m.curProject(), m.mine, m.authors, m.myprs, m.brSel)
	case "u":
		if m.busy {
			return m, nil
		}
		targets := m.selectedWorktrees()
		if len(targets) == 0 {
			m.statusMsg = "nothing selected (space to select, or cursor worktree)"
			return m, nil
		}
		m.busy = true
		m.busyLabel = "up"
		m = m.appendLog("up " + branchesOf(targets))
		return m, dashboardUpCmd(m.curProject(), targets)
	case "d":
		if m.busy {
			return m, nil
		}
		targets := m.selectedWorktrees()
		if len(targets) == 0 {
			m.statusMsg = "nothing selected (space to select, or cursor worktree)"
			return m, nil
		}
		m.busy = true
		m.busyLabel = "down"
		m = m.appendLog("down " + branchesOf(targets))
		return m, dashboardDownCmd(m.curProject(), targets)
	case "a":
		if m.busy {
			return m, nil
		}
		branches := selectedKeys(m.brSel)
		if len(branches) == 0 {
			m.statusMsg = "no branches queued (focus branches pane, space to queue)"
			return m, nil
		}
		m.busy = true
		m.busyLabel = "add"
		m = m.appendLog("add " + strings.Join(branches, ", "))
		return m, dashboardAddCmd(m.curProject(), branches)
	case "x":
		if m.busy {
			return m, nil
		}
		targets := m.selectedWorktrees()
		if len(targets) == 0 {
			m.statusMsg = "nothing selected (space to select, or cursor worktree)"
			return m, nil
		}
		var names []string
		for _, t := range targets {
			names = append(names, t.Branch)
		}
		m.confirm = "remove"
		m.pendingX = names
		m.statusMsg = "remove " + strings.Join(names, ", ") + "? (y/n)"
		return m, nil
	}
	return m, nil
}

// dashboardFetchOpCmd runs a network fetch, then reports branches through
// dashboardFetchDoneMsg so Update (not the background goroutine) mutates state.
func dashboardFetchOpCmd(p *dashboardProject, mine bool, authors []string, myprs bool, keepSel map[string]bool) tea.Cmd {
	return func() tea.Msg {
		msg := dashboardFetchBranchesCmd(p, mine, authors, myprs, keepSel)()
		bm, ok := msg.(dashboardBranchesMsg)
		if !ok {
			return dashboardOpDoneMsg{label: "fetch", err: fmt.Errorf("fetch failed")}
		}
		if bm.err != nil {
			return dashboardOpDoneMsg{label: "fetch", err: bm.err}
		}
		return dashboardFetchDoneMsg{entries: bm.entries}
	}
}

// dashboardFetchDoneMsg carries a successful fetch branch list.
type dashboardFetchDoneMsg struct {
	entries []branchEntry
}

func branchesOf(recs []ports.WorktreeRecord) string {
	var out []string
	for _, r := range recs {
		out = append(out, r.Branch)
	}
	return strings.Join(out, ", ")
}

// View.
var (
	dashTitleStyle = lipgloss.NewStyle().Bold(true)
	dashSelStyle   = lipgloss.NewStyle().Bold(true)
	dashDimStyle   = lipgloss.NewStyle().Faint(true)
	dashErrStyle   = lipgloss.NewStyle().Bold(true)
)

func (m dashboardModel) View() string {
	var b strings.Builder
	p := m.curProject()
	title := "wrk3 dashboard"
	if p != nil {
		title += " — " + p.desc.Name
		if len(m.projects) > 1 {
			title += fmt.Sprintf(" (%d/%d, tab to switch)", m.cur+1, len(m.projects))
		}
	}
	if m.busy {
		title += "  " + m.spinner.View() + " " + m.busyLabel + "…"
	}
	b.WriteString(dashTitleStyle.Render(title) + "\n")
	if p == nil || p.loadErr != nil || p.cfg == nil {
		err := "project not loaded"
		if p != nil && p.loadErr != nil {
			err = p.loadErr.Error()
		}
		b.WriteString(dashErrStyle.Render(err) + "\n")
		b.WriteString(dashboardHelpFooter(m))
		return b.String()
	}
	fetchInfo := "never"
	if !m.fetchedAt.IsZero() {
		fetchInfo = m.fetchedAt.Format("15:04:05")
	}
	pollInfo := "off"
	if m.poll > 0 {
		pollInfo = m.poll.String()
	}
	next := nextAppPort(p.cfg, stateRecsOf(m.rows))
	filterInfo := fmt.Sprintf("mine=%v myprs=%v", m.mine, m.myprs)
	if len(m.authors) > 0 {
		filterInfo += " authors=" + strings.Join(m.authors, ",")
	}
	fmt.Fprintf(&b, "remote %s · fetch %s · poll %s · next app port %d · %s (m toggles mine, P toggles myprs)\n",
		p.remote, fetchInfo, pollInfo, next, filterInfo)

	b.WriteString("\n" + m.worktreePane() + "\n")
	b.WriteString("\n" + m.branchPane() + "\n")
	b.WriteString("\n" + m.logPane() + "\n")
	if m.statusMsg != "" {
		b.WriteString(dashErrStyle.Render(m.statusMsg) + "\n")
	}
	if m.confirm != "" {
		fmt.Fprintf(&b, "confirm %s %s? press y/n\n", m.confirm, strings.Join(m.pendingX, ", "))
	}
	b.WriteString(dashboardHelpFooter(m))
	return b.String()
}

func stateRecsOf(rows []dashboardRow) []ports.WorktreeRecord {
	out := make([]ports.WorktreeRecord, 0, len(rows))
	for _, r := range rows {
		if r.IsMain {
			continue
		}
		out = append(out, r.Rec)
	}
	return out
}

func (m dashboardModel) worktreePane() string {
	var b strings.Builder
	head := "WORKTREES (1)"
	if m.pane == 0 {
		head = dashSelStyle.Render("WORKTREES (1) ●")
	}
	b.WriteString(head + "\n")
	b.WriteString("WORKTREE  BRANCH  STATUS  APP  COMPOSE_PROJECT\n")
	if len(m.rows) == 0 {
		b.WriteString(dashDimStyle.Render("  (no worktrees — queue branches below, press a)") + "\n")
		return b.String()
	}
	for i, row := range m.rows {
		cursor := "  "
		if m.pane == 0 && i == m.workCursor {
			cursor = "> "
		}
		box := "[ ]"
		if m.workSel[row.Rec.Branch] {
			box = "[x]"
		}
		main := ""
		if row.IsMain {
			main = " (main)"
		}
		line := fmt.Sprintf("%s%s %s  %s  %s  %s  %s%s", cursor, box,
			row.Rec.Slug, row.Rec.Branch, row.Status, row.App, row.Rec.ComposeProject, main)
		if row.Stale {
			line = dashDimStyle.Render(line)
		} else if m.pane == 0 && i == m.workCursor {
			line = dashSelStyle.Render(line)
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

func (m dashboardModel) branchPane() string {
	var b strings.Builder
	head := "REMOTE BRANCHES (2)"
	if m.pane == 1 {
		head = dashSelStyle.Render("REMOTE BRANCHES (2) ●")
	}
	b.WriteString(head + "\n")
	if len(m.branches) == 0 {
		b.WriteString(dashDimStyle.Render("  (no branches — press R to fetch)") + "\n")
		return b.String()
	}
	shown := 0
	for i, e := range m.branches {
		if shown >= 20 && i < len(m.branches)-1 {
			fmt.Fprintf(&b, "  … %d more (fetch list truncated to 20)\n", len(m.branches)-shown)
			break
		}
		shown++
		cursor := "  "
		if m.pane == 1 && i == m.brCursor {
			cursor = "> "
		}
		var line string
		switch {
		case e.Registered:
			line = dashDimStyle.Render(fmt.Sprintf("%s[·] %s (registered)", cursor, e.Name))
		case e.CheckedOut:
			line = dashDimStyle.Render(fmt.Sprintf("%s[·] %s (checked out)", cursor, e.Name))
		default:
			box := "[ ]"
			if m.brSel[e.Name] {
				box = "[x]"
			}
			line = fmt.Sprintf("%s%s %s", cursor, box, e.Name)
			if m.pane == 1 && i == m.brCursor {
				line = dashSelStyle.Render(line)
			}
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

func (m dashboardModel) logPane() string {
	var b strings.Builder
	b.WriteString("LOG\n")
	tail := m.log
	if len(tail) > 8 {
		tail = tail[len(tail)-8:]
	}
	for _, l := range tail {
		b.WriteString("  " + l + "\n")
	}
	return b.String()
}

func dashboardHelpFooter(m dashboardModel) string {
	if m.showHelp {
		return "keys: j/k move · space select · 1/2 or ←/→ pane · tab project · u up · d down · a add queued · x remove (confirm y/n) · r refresh · R fetch · m mine-filter · P myprs-filter (GitHub, via gh) · ? help · q quit\n"
	}
	return "q quit · 1/2 pane · space select · u/d up/down · a add · x remove · r refresh · R fetch · m mine · P myprs · tab project · ? help\n"
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func init() {
	dashboardCmd.Flags().DurationVar(&dashboardPoll, "poll", 15*time.Second, "background refresh interval (0 disables polling)")
	dashboardCmd.Flags().StringVar(&dashboardRemote, "remote", "", "remote to fetch/list (default: source.git.remote, else origin)")
	dashboardCmd.Flags().BoolVar(&dashboardMine, "mine", false, "only your branches (tip or branch-exclusive history matches git config user)")
	dashboardCmd.Flags().StringSliceVar(&dashboardAuthors, "author", nil, "only branches matching author substring in tip or history (repeatable)")
	dashboardCmd.Flags().BoolVar(&dashboardMyPRS, "myprs", false, "only branches with an open PR involving you (GitHub remotes only, via gh)")
	dashboardCmd.Flags().StringVar(&dashboardProjectArg, "project", "", "project name from registry (default: local project in cwd)")
	_ = dashboardCmd.RegisterFlagCompletionFunc("project", completeProjectNames)
	_ = dashboardCmd.RegisterFlagCompletionFunc("remote", completeRemotes)
	rootCmd.AddCommand(dashboardCmd)
}
