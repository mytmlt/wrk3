package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/viewport"
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
	keys       dashboardKeys
	help       help.Model
	logView    viewport.Model
}

func newDashboardModel(descs []dashboardProjectDesc, poll time.Duration, remote string, mine bool, authors []string, myprs bool) dashboardModel {
	projects := make([]*dashboardProject, 0, len(descs))
	for _, d := range descs {
		projects = append(projects, loadDashboardProject(d, remote))
	}
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	lv := viewport.New(78, dashboardLogHeight)
	lv.SetContent("dashboard started — r refresh, R fetch, ? help")
	lv.GotoBottom()
	hp := help.New()
	hp.ShowAll = false
	return dashboardModel{
		projects: projects,
		workSel:  map[string]bool{},
		brSel:    map[string]bool{},
		poll:     poll,
		mine:     mine,
		authors:  append([]string(nil), authors...),
		myprs:    myprs,
		spinner:  sp,
		keys:     newDashboardKeys(),
		help:     hp,
		logView:  lv,
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
// Stored transitional/terminal states (setting up, stopping, failed) win
// over the live probe so rows stay honest while entries execute.
func probeDashboardRows(cfg *config.Config, recs []ports.WorktreeRecord, mainBranch string) []dashboardRow {
	type cell struct {
		status string
		ports  string
		stale  bool
	}
	cells := make([]cell, len(recs))
	g, _ := errgroup.WithContext(context.Background())
	for i, rec := range recs {
		i, rec := i, rec
		g.Go(func() error {
			if _, err := os.Stat(rec.AbsPath); err != nil {
				cells[i] = cell{status: "stale", ports: "?", stale: true}
				return nil
			}
			portText := portsCell(rec.Ports)
			if ports.StoredStatusOverridesLive(rec.Status) {
				cells[i] = cell{status: rec.Status, ports: portText}
				return nil
			}
			st, err := liveStatus(cfg, rec)
			if err != nil {
				if rec.Status != "" {
					cells[i] = cell{status: rec.Status, ports: portText}
				} else {
					cells[i] = cell{status: "unknown", ports: portText}
				}
				return nil
			}
			cells[i] = cell{status: st, ports: portText}
			return nil
		})
	}
	_ = g.Wait()
	rows := make([]dashboardRow, 0, len(recs))
	for i, rec := range recs {
		rows = append(rows, dashboardRow{
			Rec:    rec,
			Status: cells[i].status,
			Ports:  cells[i].ports,
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
		return runUpTargets(context.Background(), r, targets, logf)
	})
}

func dashboardDownCmd(p *dashboardProject, targets []ports.WorktreeRecord) tea.Cmd {
	return dashboardOpCmd(p, "down", func(logf func(string, ...any)) error {
		if p == nil || p.cfg == nil {
			return fmt.Errorf("project not loaded")
		}
		r := &resolved{cfg: p.cfg, src: p.src, base: p.base, stateP: p.stateP}
		return runDownTargets(context.Background(), r, targets, logf)
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

// markRowsSettingUp flips the in-memory rows for targets to setting up so
// the table updates instantly on `u`, before the background op writes state
// and the next refresh picks it up. Stale rows (missing dirs) are left
// alone. The implicit main worktree has no state-file entry, so the
// in-memory flip is its only setting-up signal.
func (m dashboardModel) markRowsSettingUp(targets []ports.WorktreeRecord) dashboardModel {
	return m.markRowsStatus(targets, ports.StatusSettingUp)
}

// markRowsStopping flips the in-memory rows for targets to stopping so
// the table updates instantly on `d`, before the background op writes state
// and the next refresh picks it up. Stale rows (missing dirs) are left
// alone. The implicit main worktree has no state-file entry, so the
// in-memory flip is its only stopping signal.
func (m dashboardModel) markRowsStopping(targets []ports.WorktreeRecord) dashboardModel {
	return m.markRowsStatus(targets, ports.StatusStopping)
}

func (m dashboardModel) markRowsStatus(targets []ports.WorktreeRecord, status string) dashboardModel {
	want := map[string]bool{}
	for _, t := range targets {
		want[t.Branch] = true
	}
	for i := range m.rows {
		if want[m.rows[i].Rec.Branch] && !m.rows[i].Stale {
			m.rows[i].Status = status
			m.rows[i].Rec.Status = status
		}
	}
	return m
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
		m.help.ShowAll = m.showHelp
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
		m = m.markRowsSettingUp(targets)
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
		m = m.markRowsStopping(targets)
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

// Layout tuning: side-by-side panes need roughly this many columns;
// narrower terminals stack the panes vertically instead.
const (
	dashboardWideLayout = 132
	dashboardLogHeight  = 5
)

// Dashboard key bindings. They double as the always-visible shortcut bar:
// the footer renders ShortHelp, `?` toggles the full grouped view. The
// bindings are display-only; handleKey still owns dispatch so selection
// and op semantics stay in one place (and stay unit-testable).
type dashboardKeys struct {
	Move, Select, Pane, Project   key.Binding
	OpUp, OpDown, OpAdd, OpRemove key.Binding
	Refresh, Fetch, Mine, MyPRS   key.Binding
	Help, Quit                    key.Binding
}

func newDashboardKeys() dashboardKeys {
	return dashboardKeys{
		Move:     key.NewBinding(key.WithKeys("j", "k", "up", "down"), key.WithHelp("j/k", "move")),
		Select:   key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "select")),
		Pane:     key.NewBinding(key.WithKeys("1", "2", "left", "right"), key.WithHelp("1/2", "pane")),
		Project:  key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "project")),
		OpUp:     key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "up")),
		OpDown:   key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "down")),
		OpAdd:    key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add")),
		OpRemove: key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "remove")),
		Refresh:  key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		Fetch:    key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "fetch")),
		Mine:     key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "mine")),
		MyPRS:    key.NewBinding(key.WithKeys("P"), key.WithHelp("P", "myprs")),
		Help:     key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "keys")),
		Quit:     key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}

// ShortHelp implements help.KeyMap: the sticky shortcut bar, first line
// (navigation). The bar is split in two so every shortcut stays visible
// on 80-column terminals; the help bubble would otherwise truncate the
// tail (hiding `q quit`).
func (k dashboardKeys) ShortHelp() []key.Binding {
	return []key.Binding{k.Move, k.Select, k.Pane, k.Project, k.Help, k.Quit}
}

// ActHelp is the second sticky-bar line (worktree/branch operations).
func (k dashboardKeys) ActHelp() []key.Binding {
	return []key.Binding{
		k.OpUp, k.OpDown, k.OpAdd, k.OpRemove,
		k.Refresh, k.Fetch, k.Mine, k.MyPRS,
	}
}

// FullHelp implements help.KeyMap: the `?` overlay groups.
func (k dashboardKeys) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Move, k.Select, k.Pane, k.Project},
		{k.OpUp, k.OpDown, k.OpAdd, k.OpRemove},
		{k.Refresh, k.Fetch, k.Mine, k.MyPRS},
		{k.Help, k.Quit},
	}
}

// dashboardWorkColumns scales the text columns to the available width so
// the table never exceeds its pane: ✓/STATUS have fixed widths (STATUS fits
// "setting up"), PORTS gets a wider fixed width for multi-port lists
// (bubbles/table truncates excess), the rest split 25/40/35 across
// WORKTREE/BRANCH/PROJECT.
func dashboardWorkColumns(width int) []table.Column {
	rest := max(width-3-11-28-12, 30)
	wt := max(rest*25/100, 8)
	br := max(rest*40/100, 12)
	pr := max(rest-wt-br, 8)
	return []table.Column{
		{Title: "✓", Width: 3},
		{Title: "WORKTREE", Width: wt},
		{Title: "BRANCH", Width: br},
		{Title: "STATUS", Width: 11},
		{Title: "PORTS", Width: 28},
		{Title: "PROJECT", Width: pr},
	}
}

// dashboardBranchColumns gives everything left after ✓/STATE/padding to
// the BRANCH column.
func dashboardBranchColumns(width int) []table.Column {
	br := max(width-3-13-6, 20)
	return []table.Column{
		{Title: "✓", Width: 3},
		{Title: "BRANCH", Width: br},
		{Title: "STATE", Width: 13},
	}
}

var (
	dashTitleStyle = lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.Color("230")).Background(lipgloss.Color("62")).
			Padding(0, 1)
	dashMetaStyle    = lipgloss.NewStyle().Faint(true)
	dashDimStyle     = lipgloss.NewStyle().Faint(true)
	dashErrStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("9"))
	dashConfirmStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11"))

	dashPaneTitleFocused = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	dashPaneTitleBlurred = lipgloss.NewStyle().Bold(true).Faint(true)
	dashLogTitleStyle    = lipgloss.NewStyle().Bold(true).Faint(true)
)

// dashboardPaneStyle borders a pane; the focused one gets the accent
// border so the active pane is obvious at a glance.
func dashboardPaneStyle(focused bool) lipgloss.Style {
	if focused {
		return lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62")).
			Padding(0, 1)
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(0, 1)
}

// dashboardTableStyles keeps header/cell padding identical so the cursor
// row never shifts the columns; only the focused pane gets the bright
// cursor style.
func dashboardTableStyles(focused bool) table.Styles {
	s := table.DefaultStyles()
	s.Header = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("244")).Padding(0, 1)
	s.Cell = lipgloss.NewStyle().Padding(0, 1)
	if focused {
		s.Selected = lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.Color("230")).Background(lipgloss.Color("62")).
			Padding(0, 1)
	} else {
		s.Selected = lipgloss.NewStyle().Faint(true).Padding(0, 1)
	}
	return s
}

// buildWorkTable renders the worktree pane as a real table: aligned
// columns, a scrolling viewport around the cursor, and a ✓ marker column
// for the multi-select set. Cell values stay plain text on purpose —
// bubbles/table truncates with runewidth (not ANSI-aware), so embedded
// color codes would break column alignment.
func (m dashboardModel) buildWorkTable(width, height int, focused bool) table.Model {
	t := table.New(
		table.WithColumns(dashboardWorkColumns(width)),
		table.WithFocused(false),
		table.WithStyles(dashboardTableStyles(focused)),
	)
	rows := make([]table.Row, 0, len(m.rows))
	for _, row := range m.rows {
		box := "[ ]"
		if m.workSel[row.Rec.Branch] {
			box = "[x]"
		}
		branch := row.Rec.Branch
		if row.IsMain {
			branch += " (main)"
		}
		rows = append(rows, table.Row{box, row.Rec.Slug, branch, row.Status, row.Ports, row.Rec.ComposeProject})
	}
	t.SetRows(rows)
	t.SetWidth(max(width, 10))
	t.SetHeight(max(height, 4))
	t.SetCursor(m.workCursor)
	return t
}

// buildBranchTable renders the remote-branch pane: queued branches get
// [x], already-registered/checked-out ones [·] with their STATE so it is
// obvious why they cannot be queued.
func (m dashboardModel) buildBranchTable(width, height int, focused bool) table.Model {
	t := table.New(
		table.WithColumns(dashboardBranchColumns(width)),
		table.WithFocused(false),
		table.WithStyles(dashboardTableStyles(focused)),
	)
	rows := make([]table.Row, 0, len(m.branches))
	for _, e := range m.branches {
		box, state := "[ ]", "new"
		switch {
		case e.Registered:
			box, state = "[·]", "registered"
		case e.CheckedOut:
			box, state = "[·]", "checked out"
		case m.brSel[e.Name]:
			box = "[x]"
		}
		rows = append(rows, table.Row{box, e.Name, state})
	}
	t.SetRows(rows)
	t.SetWidth(max(width, 10))
	t.SetHeight(max(height, 4))
	t.SetCursor(m.brCursor)
	return t
}

func (m dashboardModel) dashboardTitle() string {
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
	return title
}

func (m dashboardModel) dashboardMeta() string {
	p := m.curProject()
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
	return fmt.Sprintf("remote %s · fetch %s · poll %s · next app port %d · %s (m toggles mine, P toggles myprs)",
		p.remote, fetchInfo, pollInfo, next, filterInfo)
}

// helpKeys guards zero-value models (tests build dashboardModel
// literally): fall back to the default set so the bar always renders.
func (m dashboardModel) helpKeys() dashboardKeys {
	if m.keys.Move.Enabled() {
		return m.keys
	}
	return newDashboardKeys()
}

func (m dashboardModel) helpBar(width int) string {
	hp := m.help
	if hp.ShortSeparator == "" {
		hp = help.New()
	}
	hp.ShowAll = m.showHelp
	hp.Width = width
	keys := m.helpKeys()
	if m.showHelp {
		return hp.FullHelpView(keys.FullHelp())
	}
	return hp.ShortHelpView(keys.ShortHelp()) + "\n" + hp.ShortHelpView(keys.ActHelp())
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

func (m dashboardModel) worktreePane(width, height int) string {
	focused := m.pane == 0
	title := "WORKTREES (1)"
	if focused {
		title = dashPaneTitleFocused.Render("WORKTREES (1) ●")
	} else {
		title = dashPaneTitleBlurred.Render("WORKTREES (1)")
	}
	body := m.buildWorkTable(max(width-2, 10), height, focused).View()
	if len(m.rows) == 0 {
		body += "\n" + dashDimStyle.Render("  (no worktrees — queue branches below, press a)")
	}
	return dashboardPaneStyle(focused).Width(width).Render(title + "\n" + body)
}

func (m dashboardModel) branchPane(width, height int) string {
	focused := m.pane == 1
	title := "REMOTE BRANCHES (2)"
	if focused {
		title = dashPaneTitleFocused.Render("REMOTE BRANCHES (2) ●")
	} else {
		title = dashPaneTitleBlurred.Render("REMOTE BRANCHES (2)")
	}
	body := m.buildBranchTable(max(width-2, 10), height, focused).View()
	if len(m.branches) == 0 {
		body += "\n" + dashDimStyle.Render("  (no branches — press R to fetch)")
	}
	return dashboardPaneStyle(focused).Width(width).Render(title + "\n" + body)
}

func (m dashboardModel) logPane(width int) string {
	lv := m.logView
	lv.Width = max(width-2, 10)
	lv.Height = dashboardLogHeight
	// Re-render from the model log each frame: viewport is view state,
	// m.log stays the source of truth.
	lv.SetContent(strings.Join(m.log, "\n"))
	lv.GotoBottom()
	return dashboardPaneStyle(false).Width(width).Render(
		dashLogTitleStyle.Render("LOG") + "\n" + lv.View())
}

func (m dashboardModel) View() string {
	w := m.width
	if w <= 0 {
		w = 80
	}
	h := m.height
	if h <= 0 {
		h = 24
	}
	var b strings.Builder
	b.WriteString(dashTitleStyle.Width(w).MaxWidth(w).Render(m.dashboardTitle()) + "\n")
	p := m.curProject()
	if p == nil || p.loadErr != nil || p.cfg == nil {
		err := "project not loaded"
		if p != nil && p.loadErr != nil {
			err = p.loadErr.Error()
		}
		b.WriteString(dashErrStyle.Render(err) + "\n")
		b.WriteString(m.helpBar(w) + "\n")
		return b.String()
	}
	b.WriteString(dashMetaStyle.Render(m.dashboardMeta()) + "\n\n")

	// Vertical budget: header (2) + gap (1) + footer (help 1-2 + status
	// 0-1 + confirm 0-1, reserve 4) + log box + body split.
	const footerReserve = 4
	logBoxH := dashboardLogHeight + 3 // title + viewport + border
	bodyH := max(h-2-1-footerReserve-logBoxH-1, 8)

	if w >= dashboardWideLayout {
		// lazydocker-style: worktrees left, branches right.
		workBoxW := w*3/5 - 1
		brBoxW := w - workBoxW - 1
		tableH := max(bodyH-3, 4) // pane title + borders
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top,
			m.worktreePane(workBoxW-2, tableH),
			" ",
			m.branchPane(brBoxW-2, tableH),
		) + "\n")
	} else {
		workH := max(bodyH*3/5, 4)
		brH := max(bodyH-workH, 4)
		b.WriteString(m.worktreePane(w-2, max(workH-3, 4)) + "\n")
		b.WriteString(m.branchPane(w-2, max(brH-3, 4)) + "\n")
	}
	b.WriteString("\n" + m.logPane(w-2) + "\n")
	if m.statusMsg != "" {
		b.WriteString(dashErrStyle.Render(m.statusMsg) + "\n")
	}
	if m.confirm != "" {
		fmt.Fprintf(&b, "%s\n", dashConfirmStyle.Render(
			fmt.Sprintf("confirm %s %s? press y/n", m.confirm, strings.Join(m.pendingX, ", "))))
	}
	b.WriteString(m.helpBar(w) + "\n")
	return b.String()
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
