package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
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
	"github.com/mattn/go-runewidth"
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
	Use:     "dashboard",
	Aliases: []string{"db"},
	Short:   "Interactive TUI: worktrees, branches, ports, up/down/reload/pull/add/remove",
	Long: `Open an interactive dashboard for this repo (plus registered projects).

Polls worktree state and remote branches, shows the worktree table with
ports, and runs up/down/reload/pull/add/remove without leaving the TUI. The CLI keeps
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
	p.src = wrapSource(src, cfg.StatePath(), "dashboard")
	p.base = cfg.AbsWorktreeBase()
	p.stateP = cfg.StatePath()
	if strings.TrimSpace(remoteOverride) != "" {
		p.remote = strings.TrimSpace(remoteOverride)
	} else {
		p.remote = cfg.EffectiveRemote()
	}
	return p
}

// resolved builds the command-resolved view for this project. All
// dashboard ops go through here so they share the persistent system log
// (origin "dashboard") with the CLI.
func (p *dashboardProject) resolved() *resolved {
	return &resolved{cfg: p.cfg, src: p.src, base: p.base, stateP: p.stateP, logSource: "dashboard"}
}

// dashboardOp tracks one in-flight background operation so the UI stays
// interactive while it runs. Branch-scoped ops (up/down/reload/pull/add/
// remove) carry their target branches for the per-branch overlap guard;
// global ops (fetch/open) carry no branches and never conflict.
type dashboardOp struct {
	id       int
	label    string
	branches []string
	proj     int
}

// dashboardModel is the BubbleTea model for the whole dashboard.
type dashboardModel struct {
	projects     []*dashboardProject
	cur          int
	rows         []dashboardRow
	branches     []branchEntry
	workCursor   int
	brCursor     int
	pane         int // 0 = worktrees, 1 = branches
	workSel      map[string]bool
	brSel        map[string]bool
	log          []string
	statusMsg    string
	proxyInfo    string // gateway status for the meta line (set on refresh)
	fetchedAt    time.Time
	ops          []dashboardOp
	nextOpID     int
	confirm      string // pending confirm label, "" when none
	pendingX     []string
	pendingForce bool // true when the pending remove confirm is a --force remove
	mine         bool
	authors      []string
	myprs        bool
	poll         time.Duration
	width        int
	height       int
	spinner      spinner.Model
	showMenu     bool
	menuCursor   int
	keys         dashboardKeys
	help         help.Model
	logView      viewport.Model
}

func newDashboardModel(descs []dashboardProjectDesc, poll time.Duration, remote string, mine bool, authors []string, myprs bool) dashboardModel {
	projects := make([]*dashboardProject, 0, len(descs))
	for _, d := range descs {
		projects = append(projects, loadDashboardProject(d, remote))
	}
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	lv := viewport.New(78, dashboardLogFallbackHeight)
	seed := []string{"dashboard started — r refresh, R fetch, ? menu"}
	lv.SetContent(strings.Join(wrapLogLines(seed, 78), "\n"))
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
		log:      []string{"dashboard started — r refresh, R fetch, ? menu"},
	}
}

func (m dashboardModel) curProject() *dashboardProject { return m.projects[m.cur] }

// isBusy reports whether any background op is in flight.
func (m dashboardModel) isBusy() bool { return len(m.ops) > 0 }

// busyTitle summarizes running ops for the header spinner, e.g.
// "up feature-a" or "up feature-a +1 more".
func (m dashboardModel) busyTitle() string {
	if len(m.ops) == 0 {
		return ""
	}
	first := m.ops[0].label
	if len(m.ops[0].branches) > 0 {
		first += " " + strings.Join(m.ops[0].branches, ", ")
	}
	if len(m.ops) == 1 {
		return first
	}
	return fmt.Sprintf("%s +%d more", first, len(m.ops)-1)
}

// startOp registers a background op and returns its ID.
func (m *dashboardModel) startOp(label string, branches []string) int {
	id := m.nextOpID
	m.nextOpID++
	m.ops = append(m.ops, dashboardOp{id: id, label: label, branches: append([]string(nil), branches...), proj: m.cur})
	return id
}

// finishOp removes the op with the given ID, reporting whether it existed.
func (m *dashboardModel) finishOp(id int) bool {
	_, ok := m.popOp(id)
	return ok
}

// popOp removes the op with the given ID and returns it, so completion
// handlers can clear only that op's branches from the selection sets.
func (m *dashboardModel) popOp(id int) (dashboardOp, bool) {
	for i, op := range m.ops {
		if op.id == id {
			m.ops = append(m.ops[:i], m.ops[i+1:]...)
			return op, true
		}
	}
	return dashboardOp{}, false
}

// conflictingOp returns the running op in the same project that already
// touches one of the target branches, or nil when there is no overlap.
func (m dashboardModel) conflictingOp(targets []string) *dashboardOp {
	if len(targets) == 0 {
		return nil
	}
	want := map[string]bool{}
	for _, t := range targets {
		want[t] = true
	}
	for i := range m.ops {
		if m.ops[i].proj != m.cur {
			continue
		}
		for _, b := range m.ops[i].branches {
			if want[b] {
				op := m.ops[i]
				return &op
			}
		}
	}
	return nil
}

// fetchRunning reports whether a fetch op is already in flight for the
// current project.
func (m dashboardModel) fetchRunning() bool {
	for _, op := range m.ops {
		if op.proj == m.cur && op.label == "fetch" {
			return true
		}
	}
	return false
}

// reconciledState loads state and adopts orphan on-disk worktrees,
// persisting when anything was adopted. Adopted branch names and .env
// divergence warnings are returned for the log pane.
func (p *dashboardProject) reconciledState() (recs []ports.WorktreeRecord, adopted []string, warns []string, err error) {
	r := p.resolved()
	recs, err = ports.Load(p.stateP)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("load state: %w", err)
	}
	return reconcileAndSave(r, recs)
}

// logReconciled appends adoption and divergence notes to the model log.
func (m dashboardModel) logReconciled(adopted, warns []string) dashboardModel {
	if len(adopted) > 0 {
		m = m.appendLog("reconciled state: adopted " + strings.Join(adopted, ", "))
	}
	for _, w := range warns {
		m = m.appendLog("warning: " + w)
	}
	return m
}

// Messages.
type dashboardTickMsg time.Time
type dashboardRowsMsg struct {
	rows      []dashboardRow
	adopted   []string
	warns     []string
	proxyInfo string // gateway status for the meta line ("", "proxy off" never set here)
	proxyNote string // log line when the gateway was started or failed to start
	err       error
}
type dashboardBranchesMsg struct {
	entries []branchEntry
	adopted []string
	warns   []string
	err     error
}
type dashboardOpDoneMsg struct {
	opID  int
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
		recs, adopted, warns, err := p.reconciledState()
		if err != nil {
			return dashboardRowsMsg{err: err}
		}
		r := p.resolved()
		proxyInfo, proxyNote := dashboardProxyEnsure(r)
		recs, changed, err := syncRuntimeAndSave(r, recs)
		if err != nil {
			return dashboardRowsMsg{err: err}
		}
		if len(changed) > 0 {
			warns = append(warns, "synced runtime state: "+strings.Join(changed, ", "))
		}
		all, err := recordsWithMain(r, recs)
		if err != nil {
			return dashboardRowsMsg{err: err}
		}
		main, _ := mainRecord(r, recs)
		mainBranch := ""
		if main != nil {
			mainBranch = main.Branch
		}
		rows := probeDashboardRows(r, all, mainBranch)
		return dashboardRowsMsg{rows: rows, adopted: adopted, warns: warns, proxyInfo: proxyInfo, proxyNote: proxyNote}
	}
}

// dashboardProxyEnsure keeps the gateway running while the dashboard is open
// (best-effort via ensureProxyForUp, never errors). It returns a short
// status for the meta line plus a log note when the gateway was started or
// failed to start. Disabled projects yield ("", "") and the meta line falls
// back to "proxy off".
func dashboardProxyEnsure(r *resolved) (info, note string) {
	if r == nil || r.cfg == nil || !r.cfg.Proxy.Enabled {
		return "", ""
	}
	if msg, warn := ensureProxyForUp(r); msg != "" {
		note = msg
	} else if warn != "" {
		note = "warning: " + warn
	}
	if proxyRunning(r.cfg) {
		return "proxy " + r.cfg.ProxyAddr(), note
	}
	return "proxy stopped", note
}

// probeDashboardRows maps records to rows, probing live runner status in
// parallel. Missing dirs short-circuit to stale/? without runner calls.
// Live running always wins over stored state; otherwise stored
// transitional/terminal states (setting up, stopping, failed) win over a
// non-running probe so rows stay honest while entries execute. Probe
// errors persist to the system log (debug) with the resolved origin.
// Running rows gain a health suffix plus a per-check summary for DETAILS.
func probeDashboardRows(r *resolved, recs []ports.WorktreeRecord, mainBranch string) []dashboardRow {
	type cell struct {
		status string
		ports  string
		health string
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
			st, err := liveStatusWithOrigin(r.cfg, r.stateP, originOf(r), rec)
			lifecycle := ports.ResolveDisplayStatus(rec.Status, st, err)
			if lifecycle != ports.StatusRunning {
				cells[i] = cell{status: lifecycle, ports: portText}
				return nil
			}
			rep := healthReportFor(r.cfg, rec)
			cells[i] = cell{status: lifecycle + rep.Suffix(), ports: portText, health: rep.Summary()}
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
			Health: cells[i].health,
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
		refs, err := displayBranches(p.src, p.cfg.RepoPath(), p.remote, mine, authors, myprs)
		if err != nil {
			return dashboardBranchesMsg{err: fmt.Errorf("list refs: %w", err)}
		}
		recs, adopted, warns, err := p.reconciledState()
		if err != nil {
			return dashboardBranchesMsg{err: err}
		}
		checked := map[string]bool{}
		if infos, err := p.src.List(p.cfg.RepoPath()); err == nil {
			checked = checkedOutSet(infos)
		}
		return dashboardBranchesMsg{entries: unregisteredBranchEntries(refs, recs, checked, keep), adopted: adopted, warns: warns}
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
		refs, err := displayBranches(p.src, p.cfg.RepoPath(), p.remote, mine, authors, myprs)
		if err != nil {
			return dashboardBranchesMsg{err: fmt.Errorf("list refs: %w", err)}
		}
		recs, adopted, warns, err := p.reconciledState()
		if err != nil {
			return dashboardBranchesMsg{err: err}
		}
		checked := map[string]bool{}
		if infos, err := p.src.List(p.cfg.RepoPath()); err == nil {
			checked = checkedOutSet(infos)
		}
		return dashboardBranchesMsg{entries: unregisteredBranchEntries(refs, recs, checked, keep), adopted: adopted, warns: warns}
	}
}

func (m dashboardModel) appendLog(line string) dashboardModel {
	m.log = append(m.log, line)
	if len(m.log) > 200 {
		m.log = m.log[len(m.log)-200:]
	}
	m.syncLogView()
	return m
}

// syncLogView rewraps m.log to the viewport width and refreshes content.
// It preserves the user's scroll position: only sticks to the bottom when
// the view was already at the bottom (follow mode). New log lines must go
// through here (via appendLog) so scrolling up is never yanked away.
func (m *dashboardModel) syncLogView() {
	w := m.logView.Width
	if w <= 0 {
		w = 78
		m.logView.Width = w
	}
	if m.logView.Height <= 0 {
		m.logView.Height = dashboardLogViewportHeight(m.height)
	}
	wasBottom := m.logView.AtBottom()
	m.logView.SetContent(strings.Join(wrapLogLines(m.log, max(w, 10)), "\n"))
	if wasBottom {
		m.logView.GotoBottom()
	}
}

// wrapLogLines breaks every over-wide line into multiple lines so long log
// output is never cut off horizontally by the viewport's MaxWidth truncate.
func wrapLogLines(lines []string, width int) []string {
	if width <= 0 {
		return lines
	}
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, wrapLogLine(l, width)...)
	}
	return out
}

// wrapLogLine greedy word-wraps one line, hard-splitting words longer than
// width. Widths are rune counts (logs are plain text, no ANSI).
func wrapLogLine(line string, width int) []string {
	if width <= 0 {
		return []string{line}
	}
	if line == "" {
		return []string{""}
	}
	if len([]rune(line)) <= width {
		return []string{line}
	}
	words := strings.Split(line, " ")
	var out []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			out = append(out, string(cur))
			cur = nil
		}
	}
	for _, w := range words {
		wr := []rune(w)
		// Hard-split an oversized word first.
		for len(wr) > width {
			flush()
			out = append(out, string(wr[:width]))
			wr = wr[width:]
		}
		if len(cur) == 0 {
			cur = wr
			continue
		}
		if len(cur)+1+len(wr) <= width {
			cur = append(cur, ' ')
			cur = append(cur, wr...)
			continue
		}
		flush()
		cur = wr
	}
	flush()
	if len(out) == 0 {
		return []string{line}
	}
	return out
}

// Op commands: up / down / add / remove over explicit selections.
func dashboardOpCmd(p *dashboardProject, opID int, label string, fn func(logf func(string, ...any)) error) tea.Cmd {
	return func() tea.Msg {
		var lines []string
		logf := func(format string, a ...any) {
			lines = append(lines, fmt.Sprintf(format, a...))
		}
		err := fn(logf)
		return dashboardOpDoneMsg{opID: opID, label: label, lines: lines, err: err}
	}
}

func dashboardUpCmd(p *dashboardProject, opID int, targets []ports.WorktreeRecord) tea.Cmd {
	return dashboardOpCmd(p, opID, "up", func(logf func(string, ...any)) error {
		if p == nil || p.cfg == nil {
			return fmt.Errorf("project not loaded")
		}
		r := p.resolved()
		if msg, warn := ensureProxyForUp(r); msg != "" {
			logf("%s", msg)
			r.logProxyResult(msg, "")
		} else if warn != "" {
			logf("warning: %s", warn)
			r.logProxyResult("", warn)
		}
		return runUpTargets(context.Background(), r, targets, logf)
	})
}

func dashboardDownCmd(p *dashboardProject, opID int, targets []ports.WorktreeRecord) tea.Cmd {
	return dashboardOpCmd(p, opID, "down", func(logf func(string, ...any)) error {
		if p == nil || p.cfg == nil {
			return fmt.Errorf("project not loaded")
		}
		r := p.resolved()
		return runDownTargets(context.Background(), r, targets, logf)
	})
}

func dashboardReloadCmd(p *dashboardProject, opID int, targets []ports.WorktreeRecord) tea.Cmd {
	return dashboardOpCmd(p, opID, "reload", func(logf func(string, ...any)) error {
		if p == nil || p.cfg == nil {
			return fmt.Errorf("project not loaded")
		}
		r := p.resolved()
		return runReloadTargets(context.Background(), r, targets, logf)
	})
}

func dashboardPullCmd(p *dashboardProject, opID int, targets []ports.WorktreeRecord) tea.Cmd {
	return dashboardOpCmd(p, opID, "pull", func(logf func(string, ...any)) error {
		if p == nil || p.cfg == nil {
			return fmt.Errorf("project not loaded")
		}
		r := p.resolved()
		return runPullTargets(r, targets, source.PullOptions{}, logf)
	})
}

func dashboardAddCmd(p *dashboardProject, opID int, branches []string) tea.Cmd {
	return dashboardOpCmd(p, opID, "add", func(logf func(string, ...any)) error {
		if p == nil || p.cfg == nil {
			return fmt.Errorf("project not loaded")
		}
		r := p.resolved()
		if msg, warn := ensureProxyForUp(r); msg != "" {
			logf("%s", msg)
			r.logProxyResult(msg, "")
		} else if warn != "" {
			logf("warning: %s", warn)
			r.logProxyResult("", warn)
		}
		if err := ensureBase(r.base); err != nil {
			return err
		}
		recs, err := loadState(r)
		if err != nil {
			return err
		}
		alloc := r.cfg.Allocator()
		infos, err := r.src.List(r.cfg.RepoPath())
		if err != nil {
			return fmt.Errorf("list worktrees: %w", err)
		}
		byBranch := localWorktreesByBranch(r, infos)
		for _, branch := range branches {
			if existing := findRecord(recs, branch); existing != nil {
				logf("skipped %s (already registered)", branch)
				continue
			}
			if resolved := lookupLocalBranch(byBranch, branch); resolved != "" {
				warns, err := adoptOne(r, &recs, &alloc, resolved, byBranch[resolved])
				if err != nil {
					return err
				}
				for _, w := range warns {
					logf("warning: %s", w)
				}
				logf("adopted %s", resolved)
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
// Refuses the implicit main checkout like `remove`. Force runs
// `git worktree remove --force` and falls back to rm -rf like
// `remove --force` when git still refuses.
func dashboardRemoveCmd(p *dashboardProject, opID int, branches []string, force bool) tea.Cmd {
	label := "remove"
	if force {
		label = "remove --force"
	}
	return dashboardOpCmd(p, opID, label, func(logf func(string, ...any)) error {
		if p == nil || p.cfg == nil {
			return fmt.Errorf("project not loaded")
		}
		r := p.resolved()
		r.logOpStart(label, label+" "+strings.Join(branches, ", "))
		for _, branch := range branches {
			recs, err := loadState(r)
			if err != nil {
				return err
			}
			rec := findRecord(recs, branch)
			if rec == nil {
				err := fmt.Errorf("unknown worktree %q (see status)", branch)
				r.logOpDone(label, label+" "+branch, err)
				return err
			}
			if isMainPath(r, rec.AbsPath) {
				err := fmt.Errorf("refusing to remove main worktree %q (repo root is always kept)", rec.Branch)
				r.logOpDone(label, label+" "+branch, err)
				return err
			}
			rn, err := r.runnerFor(*rec)
			if err != nil {
				r.logOpDone(label, label+" "+rec.Branch, err)
				return err
			}
			if err := rn.Down(context.Background(), rec.AbsPath, envForWorktree(r.cfg, *rec)); err != nil {
				logf("warning: compose down for %q: %v", rec.Branch, err)
			}
			if _, err := ports.StripManaged(filepath.Join(rec.AbsPath, ports.EnvFileName), rec.Ports); err != nil {
				logf("warning: strip managed .env keys for %q: %v", rec.Branch, err)
			}
			if err := r.src.Remove(r.cfg.RepoPath(), rec.AbsPath, force); err != nil {
				if !force {
					err = fmt.Errorf("remove worktree %q: %w", rec.Branch, err)
					r.logOpDone(label, label+" "+rec.Branch, err)
					return err
				}
				logf("warning: worktree remove for %q: %v", rec.Branch, err)
				_ = os.RemoveAll(rec.AbsPath)
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
				r.logOpDone(label, label+" "+rec.Branch, err)
				return err
			}
			r.logOpDone(label, "removed "+rec.Branch, nil)
			logf("removed %s", rec.Branch)
		}
		return nil
	})
}

// dashboardCopiedMsg reports a clipboard copy of a worktree URL.
type dashboardCopiedMsg struct {
	url string
	err error
}

// dashboardCopyCmd copies url to the OS clipboard (pbcopy on macOS,
// wl-copy/xclip/xsel on Linux, clip on Windows) and reports back via
// dashboardCopiedMsg so Update (not the background goroutine) mutates
// state.
func dashboardCopyCmd(url string) tea.Cmd {
	return func() tea.Msg {
		return dashboardCopiedMsg{url: url, err: copyTextToClipboard(url)}
	}
}

// dashboardOpenCmd logs the worktree URL and opens it in a browser.
// Browser failures still leave the clickable URL in the log pane.
// Open is tracked as a branch-less op: it shows in the title spinner but
// never conflicts with other ops and never blocks the UI.
func dashboardOpenCmd(p *dashboardProject, opID int, rec ports.WorktreeRecord) tea.Cmd {
	return dashboardOpCmd(p, opID, "open", func(logf func(string, ...any)) error {
		if p == nil || p.cfg == nil {
			return fmt.Errorf("project not loaded")
		}
		target := dashboardURLFor(p.cfg, rec)
		logf("%s", target)
		opener, err := browserOpener()
		if err != nil {
			return fmt.Errorf("open browser: %w", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		c := exec.CommandContext(ctx, opener[0], append(opener[1:], target)...)
		if err := c.Run(); err != nil {
			return fmt.Errorf("open browser: %w", err)
		}
		return nil
	})
}

// dashboardEnvDoneMsg reports the result of editing a worktree .env in
// an external editor (tea.ExecProcess suspends the TUI while it runs).
type dashboardEnvDoneMsg struct {
	branch string
	path   string
	err    error
}

// dashboardEditEnvCmd suspends the TUI and opens path in the user's editor
// fullscreen, resuming when the editor exits. The .env is ensured before
// suspending so the file always exists.
func dashboardEditEnvCmd(path, branch string) tea.Cmd {
	c, err := editorCmdFor(path)
	if err != nil {
		return func() tea.Msg { return dashboardEnvDoneMsg{branch: branch, path: path, err: err} }
	}
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return dashboardEnvDoneMsg{branch: branch, path: path, err: err}
	})
}

// Update.
func (m dashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// Keep the log viewport in sync with the rendered grid geometry
		// so the rewrapped line count (and maxYOffset) stays valid for
		// scrolling: logPane renders logViewW wide and logViewH tall.
		wasBottom := m.logView.AtBottom()
		grid := computeDashboardGrid(msg.Width, msg.Height)
		m.logView.Width = grid.logViewW
		m.logView.Height = grid.logViewH
		m.logView.SetContent(strings.Join(wrapLogLines(m.log, m.logView.Width), "\n"))
		if wasBottom || len(m.log) == 0 {
			m.logView.GotoBottom()
		} else {
			m.logView.SetYOffset(m.logView.YOffset)
		}
		return m, nil
	case dashboardTickMsg:
		// Polling stays live while ops run so unrelated worktrees keep
		// refreshing; transitional setting-up/stopping states survive
		// refresh via ports.ResolveDisplayStatus.
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
			m.proxyInfo = msg.proxyInfo
			if msg.proxyNote != "" {
				m = m.appendLog(msg.proxyNote)
			}
			m = m.logReconciled(msg.adopted, msg.warns)
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
			m = m.logReconciled(msg.adopted, msg.warns)
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
						if e.Name == k && e.selectable() {
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
		op, _ := m.popOp(msg.opID)
		// Staged remove confirms are cleared at op start (y), never here:
		// a newer y/n prompt staged while this op ran must survive its
		// completion.
		for _, l := range msg.lines {
			m = m.appendLog("[" + msg.label + "] " + l)
		}
		if msg.err != nil {
			m.statusMsg = msg.label + ": " + msg.err.Error()
			m = m.appendLog("error: " + msg.err.Error())
		} else {
			m.statusMsg = msg.label + " done"
			// Clear only this op's branches so selections queued while it
			// ran (e.g. a second add/remove on disjoint branches) survive.
			if msg.label == "add" {
				for _, b := range op.branches {
					delete(m.brSel, b)
				}
			}
			if strings.HasPrefix(msg.label, "remove") {
				for _, b := range op.branches {
					delete(m.workSel, b)
				}
			}
		}
		return m, tea.Batch(
			dashboardRefreshRowsCmd(m.curProject()),
			dashboardReloadBranchesCmd(m.curProject(), m.mine, m.authors, m.myprs, m.brSel),
		)
	case dashboardFetchDoneMsg:
		m.finishOp(msg.opID)
		prev := m.brSel
		m.branches = msg.entries
		m.fetchedAt = time.Now()
		// Current selections alone reflect intent: entries carry
		// fetch-start selections, so unioning them back would resurrect
		// branches the user explicitly dequeued while the fetch ran.
		// Keep only current selections that are still selectable.
		m.brSel = map[string]bool{}
		for k, v := range prev {
			if !v {
				continue
			}
			for _, e := range m.branches {
				if e.Name == k && e.selectable() {
					m.brSel[k] = true
				}
			}
		}
		if m.brCursor >= len(m.branches) {
			m.brCursor = max(0, len(m.branches)-1)
		}
		m.statusMsg = "fetch done"
		m = m.appendLog("fetch done")
		return m, dashboardRefreshRowsCmd(m.curProject())
	case dashboardCopiedMsg:
		if msg.err != nil {
			m.statusMsg = "copy failed: " + msg.err.Error()
			m = m.appendLog("copy failed for " + msg.url + ": " + msg.err.Error())
		} else {
			m.statusMsg = "copied " + msg.url
			m = m.appendLog("copied " + msg.url)
		}
		return m, nil
	case dashboardEnvDoneMsg:
		if msg.err != nil {
			m.statusMsg = "edit .env: " + msg.err.Error()
			m = m.appendLog("edit " + msg.branch + " .env (" + msg.path + "): " + msg.err.Error())
		} else {
			m.statusMsg = "edited " + msg.branch + " .env"
			m = m.appendLog("edited " + msg.branch + " .env")
		}
		return m, tea.Batch(
			dashboardRefreshRowsCmd(m.curProject()),
			dashboardReloadBranchesCmd(m.curProject(), m.mine, m.authors, m.myprs, m.brSel),
		)
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

// cursorWorktree returns the worktree row under the cursor, ignoring any
// space selections. Single-target actions (o/O open/copy URL) use this so
// a stale selection on another row never hijacks which link opens.
func (m dashboardModel) cursorWorktree() (ports.WorktreeRecord, bool) {
	if len(m.rows) == 0 || m.workCursor < 0 || m.workCursor >= len(m.rows) {
		return ports.WorktreeRecord{}, false
	}
	return m.rows[m.workCursor].Rec, true
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
	// Pending remove confirm (normal or --force) wins over everything:
	// the Menu cannot open while a confirm is pending.
	if m.confirm != "" {
		switch msg.String() {
		case "y", "Y":
			targets := append([]string(nil), m.pendingX...)
			force := m.pendingForce
			label := "remove"
			if force {
				label = "remove --force"
			}
			if conflict := m.conflictingOp(targets); conflict != nil {
				m.statusMsg = label + " blocked: " + conflict.label + " already running for " + strings.Join(targets, ", ") + " (n to cancel, y to retry)"
				return m, nil
			}
			id := m.startOp(label, targets)
			m.confirm = ""
			m.pendingX = nil
			m.pendingForce = false
			m = m.appendLog(label + " " + strings.Join(targets, ", "))
			return m, dashboardRemoveCmd(m.curProject(), id, targets, force)
		case "n", "N", "esc":
			m.confirm = ""
			m.pendingX = nil
			m.pendingForce = false
			m.statusMsg = "remove cancelled"
			return m, nil
		case "tab":
			// A staged confirm belongs to the current project: cancel it
			// so y cannot execute old branch names after the switch,
			// then fall through to the normal project switch.
			m.confirm = ""
			m.pendingX = nil
			m.pendingForce = false
			m.statusMsg = "remove cancelled (project switched)"
			if m.showMenu {
				return m, nil
			}
			return m.handleNormalKey(msg)
		}
		return m, nil
	}
	// Lazydocker-style Menu popup captures all keys while open.
	if m.showMenu {
		return m.handleMenuKey(msg)
	}
	return m.handleNormalKey(msg)
}

// handleMenuKey navigates the Menu popup: j/k/up/down move, enter runs
// the selected row, esc/? closes without acting. Raw op keys are
// swallowed so an open menu never triggers an accidental up/remove.
func (m dashboardModel) handleMenuKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	items := dashboardMenuItems()
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		if len(items) > 0 {
			m.menuCursor = (m.menuCursor + len(items) - 1) % len(items)
		}
		return m, nil
	case "down", "j":
		if len(items) > 0 {
			m.menuCursor = (m.menuCursor + 1) % len(items)
		}
		return m, nil
	case "enter":
		if m.menuCursor < 0 || m.menuCursor >= len(items) {
			return m, nil
		}
		run := items[m.menuCursor].Run
		m.showMenu = false
		m.menuCursor = 0
		m.statusMsg = ""
		return m.handleNormalKey(dashboardKeyMsg(run))
	case "esc", "?", "q":
		m.showMenu = false
		m.menuCursor = 0
		m.statusMsg = ""
		return m, nil
	}
	return m, nil
}

// dashboardKeyMsg synthesizes a KeyMsg for a Menu Run value so enter
// reuses the exact handleNormalKey dispatch (no duplicated op logic).
func dashboardKeyMsg(s string) tea.KeyMsg {
	switch s {
	case " ", "space":
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

func (m dashboardModel) handleNormalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Log pane scrolling always works (menu closed, no pending confirm).
	switch msg.String() {
	case "pgup":
		m.logView.ScrollUp(max(m.logView.Height, 1))
		return m, nil
	case "pgdown":
		m.logView.ScrollDown(max(m.logView.Height, 1))
		return m, nil
	case "home":
		m.logView.GotoTop()
		return m, nil
	case "end":
		m.logView.GotoBottom()
		return m, nil
	}
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "?":
		m.showMenu = true
		m.menuCursor = 0
		m.statusMsg = ""
		return m, nil
	case "tab":
		if len(m.projects) > 1 {
			m.cur = (m.cur + 1) % len(m.projects)
			m.rows = nil
			m.branches = nil
			m.proxyInfo = ""
			m.workSel = map[string]bool{}
			m.brSel = map[string]bool{}
			m.workCursor = 0
			m.brCursor = 0
			m.statusMsg = ""
			// A staged remove confirm belongs to the previous project:
			// drop it so y cannot execute old branch names in the new
			// project.
			m.confirm = ""
			m.pendingX = nil
			m.pendingForce = false
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
	case "3":
		m.pane = 2
		return m, nil
	case "left", "right":
		// Cycle worktrees -> branches -> logs (detail preview is not
		// focusable; it follows the worktree cursor/selection).
		if msg.String() == "left" {
			m.pane = (m.pane + 2) % 3
		} else {
			m.pane = (m.pane + 1) % 3
		}
		return m, nil
	case "up", "k":
		if m.pane == 2 {
			m.logView.ScrollUp(1)
			return m, nil
		}
		if m.pane == 0 && m.workCursor > 0 {
			m.workCursor--
		} else if m.pane == 1 && m.brCursor > 0 {
			m.brCursor--
		}
		return m, nil
	case "down", "j":
		if m.pane == 2 {
			m.logView.ScrollDown(1)
			return m, nil
		}
		if m.pane == 0 && m.workCursor < len(m.rows)-1 {
			m.workCursor++
		} else if m.pane == 1 && m.brCursor < len(m.branches)-1 {
			m.brCursor++
		}
		return m, nil
	case " ":
		// Selection never blocks, even while ops run.
		if m.pane == 2 {
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
				if e.selectable() {
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
		if m.fetchRunning() {
			m.statusMsg = "fetch already running"
			return m, nil
		}
		id := m.startOp("fetch", nil)
		m = m.appendLog("fetch " + m.curProject().remote + "…")
		return m, dashboardFetchOpCmd(m.curProject(), id, m.mine, m.authors, m.myprs, m.brSel)
	case "m":
		m.mine = !m.mine
		m.statusMsg = "mine filter " + map[bool]string{true: "on", false: "off"}[m.mine]
		return m, dashboardReloadBranchesCmd(m.curProject(), m.mine, m.authors, m.myprs, m.brSel)
	case "P":
		m.myprs = !m.myprs
		m.statusMsg = "myprs filter " + map[bool]string{true: "on", false: "off"}[m.myprs]
		return m, dashboardReloadBranchesCmd(m.curProject(), m.mine, m.authors, m.myprs, m.brSel)
	case "u":
		targets := m.selectedWorktrees()
		if len(targets) == 0 {
			m.statusMsg = "nothing selected (space to select, or cursor worktree)"
			return m, nil
		}
		names := branchNamesOf(targets)
		if conflict := m.conflictingOp(names); conflict != nil {
			m.statusMsg = "up blocked: " + conflict.label + " already running for " + strings.Join(names, ", ")
			return m, nil
		}
		id := m.startOp("up", names)
		m = m.appendLog("up " + branchesOf(targets))
		m = m.markRowsSettingUp(targets)
		return m, dashboardUpCmd(m.curProject(), id, targets)
	case "d":
		targets := m.selectedWorktrees()
		if len(targets) == 0 {
			m.statusMsg = "nothing selected (space to select, or cursor worktree)"
			return m, nil
		}
		names := branchNamesOf(targets)
		if conflict := m.conflictingOp(names); conflict != nil {
			m.statusMsg = "down blocked: " + conflict.label + " already running for " + strings.Join(names, ", ")
			return m, nil
		}
		id := m.startOp("down", names)
		m = m.appendLog("down " + branchesOf(targets))
		m = m.markRowsStopping(targets)
		return m, dashboardDownCmd(m.curProject(), id, targets)
	case "l":
		targets := m.selectedWorktrees()
		if len(targets) == 0 {
			m.statusMsg = "nothing selected (space to select, or cursor worktree)"
			return m, nil
		}
		names := branchNamesOf(targets)
		if conflict := m.conflictingOp(names); conflict != nil {
			m.statusMsg = "reload blocked: " + conflict.label + " already running for " + strings.Join(names, ", ")
			return m, nil
		}
		id := m.startOp("reload", names)
		m = m.appendLog("reload " + branchesOf(targets))
		m = m.markRowsSettingUp(targets)
		return m, dashboardReloadCmd(m.curProject(), id, targets)
	case "p":
		targets := m.selectedWorktrees()
		if len(targets) == 0 {
			m.statusMsg = "nothing selected (space to select, or cursor worktree)"
			return m, nil
		}
		names := branchNamesOf(targets)
		if conflict := m.conflictingOp(names); conflict != nil {
			m.statusMsg = "pull blocked: " + conflict.label + " already running for " + strings.Join(names, ", ")
			return m, nil
		}
		id := m.startOp("pull", names)
		m = m.appendLog("pull " + branchesOf(targets))
		return m, dashboardPullCmd(m.curProject(), id, targets)
	case "a":
		branches := selectedKeys(m.brSel)
		if len(branches) == 0 {
			m.statusMsg = "no branches queued (focus branches pane, space to queue)"
			return m, nil
		}
		if conflict := m.conflictingOp(branches); conflict != nil {
			m.statusMsg = "add blocked: " + conflict.label + " already running for " + strings.Join(branches, ", ")
			return m, nil
		}
		id := m.startOp("add", branches)
		m = m.appendLog("add " + strings.Join(branches, ", "))
		return m, dashboardAddCmd(m.curProject(), id, branches)
	case "o":
		rec, ok := m.cursorWorktree()
		if !ok {
			m.statusMsg = "no worktree under cursor"
			return m, nil
		}
		m = m.appendLog("open " + rec.Branch)
		id := m.startOp("open", nil)
		return m, dashboardOpenCmd(m.curProject(), id, rec)
	case "O":
		rec, ok := m.cursorWorktree()
		if !ok {
			m.statusMsg = "no worktree under cursor"
			return m, nil
		}
		var urlCfg *config.Config
		if p := m.curProject(); p != nil {
			urlCfg = p.cfg
		}
		target := dashboardURLFor(urlCfg, rec)
		if target == "" || target == "?" {
			m.statusMsg = "no URL to copy for " + rec.Branch + " (no app port)"
			return m, nil
		}
		m = m.appendLog("copy " + rec.Branch)
		m.statusMsg = "copying " + target + "…"
		return m, dashboardCopyCmd(target)
	case "e":
		targets := m.selectedWorktrees()
		if len(targets) == 0 {
			m.statusMsg = "nothing selected (space to select, or cursor worktree)"
			return m, nil
		}
		rec := targets[0]
		if len(targets) > 1 {
			m = m.appendLog(fmt.Sprintf("edit %s .env (first of %d selected)", rec.Branch, len(targets)))
		} else {
			m = m.appendLog("edit " + rec.Branch + " .env")
		}
		if st, err := os.Stat(rec.AbsPath); err != nil {
			m.statusMsg = "worktree " + rec.Branch + " is stale (directory missing)"
			return m, nil
		} else if !st.IsDir() {
			m.statusMsg = "worktree " + rec.Branch + " path is not a directory"
			return m, nil
		}
		if p := m.curProject(); p != nil && p.cfg != nil {
			r := &resolved{cfg: p.cfg, src: p.src, base: p.base, stateP: p.stateP}
			if warns, err := ensureWorktreeEnv(r, rec); err != nil {
				m.statusMsg = "edit .env: " + err.Error()
				return m, nil
			} else {
				for _, w := range warns {
					m = m.appendLog("warning: " + w)
				}
			}
		}
		path := envFilePath(rec.AbsPath)
		m.statusMsg = "editing " + rec.Branch + " .env…"
		return m, dashboardEditEnvCmd(path, rec.Branch)
	case "x", "X":
		targets := m.selectedWorktrees()
		if len(targets) == 0 {
			m.statusMsg = "nothing selected (space to select, or cursor worktree)"
			return m, nil
		}
		var names []string
		for _, t := range targets {
			names = append(names, t.Branch)
		}
		if conflict := m.conflictingOp(names); conflict != nil {
			m.statusMsg = "remove blocked: " + conflict.label + " already running for " + strings.Join(names, ", ")
			return m, nil
		}
		// x asks for a clean remove; X asks for --force (dirty worktrees
		// with modified/untracked files).
		force := msg.String() == "X"
		label := "remove"
		if force {
			label = "remove --force"
		}
		m.confirm = label
		m.pendingX = names
		m.pendingForce = force
		m.statusMsg = label + " " + strings.Join(names, ", ") + "? (y/n)"
		return m, nil
	}
	return m, nil
}

// dashboardFetchOpCmd runs a network fetch, then reports branches through
// dashboardFetchDoneMsg so Update (not the background goroutine) mutates state.
func dashboardFetchOpCmd(p *dashboardProject, opID int, mine bool, authors []string, myprs bool, keepSel map[string]bool) tea.Cmd {
	keep := make(map[string]bool, len(keepSel))
	for k, v := range keepSel {
		keep[k] = v
	}
	return func() tea.Msg {
		msg := dashboardFetchBranchesCmd(p, mine, authors, myprs, keep)()
		bm, ok := msg.(dashboardBranchesMsg)
		if !ok {
			return dashboardOpDoneMsg{opID: opID, label: "fetch", err: fmt.Errorf("fetch failed")}
		}
		if bm.err != nil {
			return dashboardOpDoneMsg{opID: opID, label: "fetch", err: bm.err}
		}
		return dashboardFetchDoneMsg{opID: opID, entries: bm.entries}
	}
}

// dashboardFetchDoneMsg carries a successful fetch branch list.
type dashboardFetchDoneMsg struct {
	opID    int
	entries []branchEntry
}

func branchesOf(recs []ports.WorktreeRecord) string {
	return strings.Join(branchNamesOf(recs), ", ")
}

func branchNamesOf(recs []ports.WorktreeRecord) []string {
	out := make([]string, 0, len(recs))
	for _, r := range recs {
		out = append(out, r.Branch)
	}
	return out
}

// View.

// Layout tuning: side-by-side panes need roughly this many columns;
// narrower terminals stack the panes vertically instead.
const dashboardWideLayout = 80

// dashboardLogFallbackHeight is the log viewport height before the first
// WindowSizeMsg arrives (and in unit tests that never set a size).
const dashboardLogFallbackHeight = 5

// dashboardLogViewportHeight gives the log viewport its 40% share of the
// screen: the log box (title + viewport + border) takes 40% of the total
// terminal height, minus 3 lines of box chrome for the viewport itself.
// Branches/details shrink to make room; worktrees keep priority.
func dashboardLogViewportHeight(totalH int) int {
	if totalH <= 0 {
		return dashboardLogFallbackHeight
	}
	return max(totalH*40/100-3, dashboardLogFallbackHeight)
}

// Dashboard key bindings. They double as the always-visible shortcut bar:
// the footer renders ShortHelp, `?` toggles the full grouped view. The
// bindings are display-only; handleKey still owns dispatch so selection
// and op semantics stay in one place (and stay unit-testable).
type dashboardKeys struct {
	Move, Select, Pane, Project                                                                  key.Binding
	OpUp, OpDown, OpReload, OpPull, OpAdd, OpOpen, OpCopyURL, OpEditEnv, OpRemove, OpForceRemove key.Binding
	Refresh, Fetch, Mine, MyPRS                                                                  key.Binding
	LogScroll                                                                                    key.Binding
	Help, Quit                                                                                   key.Binding
}

func newDashboardKeys() dashboardKeys {
	return dashboardKeys{
		Move:          key.NewBinding(key.WithKeys("j", "k", "up", "down"), key.WithHelp("j/k", "move/scroll")),
		Select:        key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "select")),
		Pane:          key.NewBinding(key.WithKeys("1", "2", "3", "left", "right"), key.WithHelp("1/2/3", "pane")),
		Project:       key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "project")),
		OpUp:          key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "up")),
		OpDown:        key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "down")),
		OpReload:      key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "reload")),
		OpPull:        key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "pull")),
		OpAdd:         key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add")),
		OpOpen:        key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "open URL")),
		OpCopyURL:     key.NewBinding(key.WithKeys("O"), key.WithHelp("O", "copy URL")),
		OpEditEnv:     key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit .env")),
		OpRemove:      key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "remove")),
		OpForceRemove: key.NewBinding(key.WithKeys("X"), key.WithHelp("X", "force remove")),
		Refresh:       key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		Fetch:         key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "fetch")),
		Mine:          key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "mine")),
		MyPRS:         key.NewBinding(key.WithKeys("P"), key.WithHelp("P", "myprs")),
		LogScroll: key.NewBinding(
			key.WithKeys("pgup", "pgdown", "home", "end"),
			key.WithHelp("pgup/pgdn", "scroll log"),
		),
		Help: key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "menu")),
		Quit: key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
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
		k.OpUp, k.OpDown, k.OpReload, k.OpPull, k.OpAdd, k.OpOpen, k.OpCopyURL, k.OpEditEnv, k.OpRemove, k.OpForceRemove,
		k.Refresh, k.Fetch, k.Mine, k.MyPRS,
	}
}

// FullHelp implements help.KeyMap: the grouped key reference. The live
// `?` Menu popup (dashboardMenuItems) is the interactive surface; this
// stays as the static grouping for the KeyMap contract.
func (k dashboardKeys) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Move, k.Select, k.Pane, k.Project},
		{k.OpUp, k.OpDown, k.OpReload, k.OpPull, k.OpAdd, k.OpOpen, k.OpCopyURL, k.OpEditEnv, k.OpRemove, k.OpForceRemove},
		{k.Refresh, k.Fetch, k.Mine, k.MyPRS},
		{k.LogScroll, k.Help, k.Quit},
	}
}

// dashboardWorkCells is the visible text of one worktree row, used to
// size columns to fit content. The lazygit-style list shows slug and
// health status only.
type dashboardWorkCells struct {
	slug   string
	status string
}

// dashboardWorkColumns sizes columns without content (all flex columns at
// their minimums, table filled when wide). Prefer
// dashboardWorkColumnsFor with real row cells so columns expand or shrink
// to fit content.
func dashboardWorkColumns(width int) []table.Column {
	return dashboardWorkColumnsFor(width, nil)
}

// dashboardWorkColumnsFor sizes the slim slug+status list to fit its
// content: SLUG flexes between 8 and 48, STATUS fits "setting up" at 11
// and grows to 32 for health suffixes. Overflow shrinks SLUG first so
// health status stays visible. Width is the table width; 6 columns are
// reserved for bubbles/table cell padding (1 left+right per column) so
// the rendered rows never overflow the pane border.
func dashboardWorkColumnsFor(width int, cells []dashboardWorkCells) []table.Column {
	const checkW = 3
	avail := max(width-6, 1)
	statusW := 11
	slugNeed := len("SLUG")
	for _, c := range cells {
		if w := len([]rune(c.status)); w > statusW {
			statusW = w
		}
		if w := len([]rune(c.slug)); w > slugNeed {
			slugNeed = w
		}
	}
	statusW = min(max(statusW, 11), 32)
	slugW := min(max(slugNeed, 8), 48)
	total := checkW + slugW + statusW
	if total > avail {
		over := total - avail
		if cut := min(over, slugW-8); cut > 0 {
			slugW -= cut
			over -= cut
		}
		if over > 0 {
			if cut := min(over, statusW-11); cut > 0 {
				statusW -= cut
				over -= cut
			}
		}
		// Absurdly narrow: force-fit STATUS so total <= width.
		if over > 0 {
			statusW = max(statusW-over, 1)
		}
	}
	// No slack expansion: the slim list stays compact so STATUS is never
	// truncated to fill the pane (bubbles/table padding would cut the
	// last column).
	return []table.Column{
		{Title: "✓", Width: checkW},
		{Title: "SLUG", Width: slugW},
		{Title: "STATUS", Width: statusW},
	}
}

// dashboardBranchColumns gives everything left after ✓/STATE/padding to
// the BRANCH column. When the pane is too narrow for all three, STATE
// shrinks (min 8) instead of pushing BRANCH off — STATE values longer
// than that truncate, which only happens on tiny terminals. Width is the
// table width; 6 columns are reserved for bubbles/table cell padding so
// the rendered rows never overflow the pane border.
func dashboardBranchColumns(width int) []table.Column {
	avail := max(width-6, 1)
	br := avail - 3 - 13
	if br < 10 {
		br = 10
	}
	stateW := 13
	if total := 3 + br + stateW; total > avail {
		stateW = max(avail-3-br, 8)
	}
	return []table.Column{
		{Title: "✓", Width: 3},
		{Title: "BRANCH", Width: br},
		{Title: "STATE", Width: stateW},
	}
}

var (
	dashTitleStyle = lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.Color("230")).Background(lipgloss.Color("33")).
			Padding(0, 1)
	dashMetaStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("247"))
	dashDimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
	dashErrStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("9"))
	dashConfirmStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11"))

	// Lazygit-inspired pane titles: focused is bright cyan, blurred is
	// dim gray. Borders follow the same scheme (blue focus, gray blur).
	dashPaneTitleFocused = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("51"))
	dashPaneTitleBlurred = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("247"))
	dashLogTitleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("247"))

	dashMenuTitleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42"))
	dashMenuSelectedStyle = lipgloss.NewStyle().Bold(true).
				Foreground(lipgloss.Color("230")).Background(lipgloss.Color("33")).
				Padding(0, 1)
	dashMenuKeyStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("51")).Bold(true)
	dashMenuHintStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
)

// dashboardCountLabel renders lazygit-style "x of y" positions, e.g.
// "1 of 3". Empty lists report "0 of 0"; out-of-range cursors clamp.
func dashboardCountLabel(cursor, total int) string {
	if total <= 0 {
		return "0 of 0"
	}
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= total {
		cursor = total - 1
	}
	return fmt.Sprintf("%d of %d", cursor+1, total)
}

// dashboardWorkStatusText is the slim-list status cell (slug + health
// status only). probeDashboardRows already folds the health suffix into
// Status ("running (healthy)", "running (degraded 1/2)"), while Health
// carries the per-check breakdown for the DETAILS pane — so Status alone
// is the health status; appending Health would duplicate it and bloat
// the column.
func dashboardWorkStatusText(row dashboardRow) string {
	if row.Stale {
		return "stale"
	}
	if row.Status == "" {
		return "?"
	}
	return row.Status
}

// dashboardPaneStyle borders a pane; the focused one gets the lazygit
// accent border (blue) so the active pane is obvious at a glance.
func dashboardPaneStyle(focused bool) lipgloss.Style {
	if focused {
		return lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("33")).
			Padding(0, 1)
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("238")).
		Padding(0, 1)
}

// dashboardTableStyles keeps header/cell padding identical so the cursor
// row never shifts the columns; only the focused pane gets the bright
// lazygit-blue cursor style.
func dashboardTableStyles(focused bool) table.Styles {
	s := table.DefaultStyles()
	s.Header = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("247")).Padding(0, 1)
	s.Cell = lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Padding(0, 1)
	if focused {
		s.Selected = lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.Color("231")).Background(lipgloss.Color("33")).
			Padding(0, 1)
	} else {
		s.Selected = lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Padding(0, 1)
	}
	return s
}

// buildWorkTable renders the lazygit-style worktree pane: slug plus
// health status only, with a ✓ marker column for the multi-select set.
// Cell values stay plain text on purpose — bubbles/table truncates with
// runewidth (not ANSI-aware), so embedded color codes would break column
// alignment. Full branch/ports/URL details live in the DETAILS pane.
func (m dashboardModel) buildWorkTable(width, height int, focused bool) table.Model {
	cells := make([]dashboardWorkCells, 0, len(m.rows))
	for _, row := range m.rows {
		slug := row.Rec.Slug
		if row.IsMain {
			slug += " (main)"
		}
		cells = append(cells, dashboardWorkCells{
			slug:   slug,
			status: dashboardWorkStatusText(row),
		})
	}
	t := table.New(
		table.WithColumns(dashboardWorkColumnsFor(width, cells)),
		table.WithFocused(false),
		table.WithStyles(dashboardTableStyles(focused)),
	)
	rows := make([]table.Row, 0, len(cells))
	for i, c := range cells {
		box := "[ ]"
		if m.workSel[m.rows[i].Rec.Branch] {
			box = "[x]"
		}
		rows = append(rows, table.Row{box, c.slug, c.status})
	}
	t.SetRows(rows)
	t.SetWidth(max(width, 10))
	t.SetHeight(max(height, 4))
	t.SetCursor(m.workCursor)
	// Fresh tables start with viewport YOffset 0, so a cursor past the
	// first page renders one row below the visible window (start =
	// cursor-height, visible = start..start+height-1). Nudging with
	// MoveDown(0) advances the viewport exactly when needed so the
	// cursor row stays visible while j-scrolling.
	t.MoveDown(0)
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
		case e.CheckedOut && !e.Adoptable:
			box, state = "[·]", "checked out"
		case e.CheckedOut:
			state = "orphan"
			if m.brSel[e.Name] {
				box = "[x]"
			}
		case m.brSel[e.Name]:
			box = "[x]"
		}
		rows = append(rows, table.Row{box, e.Name, state})
	}
	t.SetRows(rows)
	t.SetWidth(max(width, 10))
	t.SetHeight(max(height, 4))
	t.SetCursor(m.brCursor)
	// See buildWorkTable: keep the rebuilt viewport pinned to the cursor
	// so it never slips one row below while j-scrolling long branch lists.
	t.MoveDown(0)
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
	if m.isBusy() {
		title += "  " + m.spinner.View() + " " + m.busyTitle() + "…"
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
	proxySeg := m.proxyInfo
	if proxySeg == "" {
		if p.cfg != nil && p.cfg.Proxy.Enabled {
			proxySeg = "proxy " + p.cfg.ProxyAddr()
		} else {
			proxySeg = "proxy off"
		}
	}
	return fmt.Sprintf("remote %s · fetch %s · poll %s · next app port %d · %s · %s (m toggles mine, P toggles myprs, o opens URL, O copies URL)",
		p.remote, fetchInfo, pollInfo, next, filterInfo, proxySeg)
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
	hp.ShowAll = false
	hp.Width = width
	keys := m.helpKeys()
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
	count := dashboardCountLabel(m.workCursor, len(m.rows))
	var title string
	if focused {
		title = dashPaneTitleFocused.Render("[1]-Worktrees (1) - " + count + " ●")
	} else {
		title = dashPaneTitleBlurred.Render("[1]-Worktrees (1) - " + count)
	}
	body := m.buildWorkTable(max(width-4, 10), height, focused).View()
	if len(m.rows) == 0 {
		body += "\n" + dashDimStyle.Render("  (no worktrees — queue branches below, press a)")
	}
	return dashboardPaneStyle(focused).Width(width).Render(title + "\n" + body)
}

func (m dashboardModel) branchPane(width, height int) string {
	focused := m.pane == 1
	count := dashboardCountLabel(m.brCursor, len(m.branches))
	var title string
	if focused {
		title = dashPaneTitleFocused.Render("[2]-Branches (2) - " + count + " ●")
	} else {
		title = dashPaneTitleBlurred.Render("[2]-Branches (2) - " + count)
	}
	body := m.buildBranchTable(max(width-4, 10), height, focused).View()
	if len(m.branches) == 0 {
		body += "\n" + dashDimStyle.Render("  (no branches — press R to fetch)")
	}
	return dashboardPaneStyle(focused).Width(width).Render(title + "\n" + body)
}

// dashboardFitLines truncates every line to the pane inner width and pads
// with blanks to exactly height lines, so the box always fills its grid
// cell. Without the pad, short content (few projects, 8 detail lines)
// leaves its column shorter than the sibling column and the grid below
// shows an empty gap; without truncation, long lines (ports/health
// lists, absolute paths) overflow the border and misalign the column.
func dashboardFitLines(lines []string, width, height int) []string {
	inner := max(width-4, 1)
	out := make([]string, 0, max(height, len(lines)))
	for _, l := range lines {
		out = append(out, runewidth.Truncate(l, inner, "…"))
	}
	if height > 0 {
		if len(out) > height {
			out = out[:height]
		}
		for len(out) < height {
			out = append(out, "")
		}
	}
	return out
}

// projectPane is the small bottom-left box: project switcher plus the
// status meta (remote/fetch/poll/proxy). Never focusable; tab switches.
func (m dashboardModel) projectPane(width, height int) string {
	title := dashPaneTitleBlurred.Render("Projects - " + dashboardCountLabel(m.cur, len(m.projects)))
	lines := make([]string, 0, len(m.projects)+4)
	for i, p := range m.projects {
		marker := "  "
		var name string
		if p == nil {
			name = "?"
		} else {
			name = p.desc.Name
			if p.loadErr != nil || p.cfg == nil {
				name += " (error)"
			}
		}
		if i == m.cur {
			marker = "* "
		}
		lines = append(lines, marker+name)
	}
	if len(m.projects) > 1 {
		lines = append(lines, "tab to switch")
	}
	if cur := m.curProject(); cur != nil {
		fetchInfo := "never"
		if !m.fetchedAt.IsZero() {
			fetchInfo = m.fetchedAt.Format("15:04:05")
		}
		pollInfo := "off"
		if m.poll > 0 {
			pollInfo = m.poll.String()
		}
		proxySeg := m.proxyInfo
		if proxySeg == "" {
			if cur.cfg != nil && cur.cfg.Proxy.Enabled {
				proxySeg = "proxy " + cur.cfg.ProxyAddr()
			} else {
				proxySeg = "proxy off"
			}
		}
		remote := cur.remote
		if remote == "" {
			remote = "origin"
		}
		lines = append(lines,
			"remote "+remote+" · fetch "+fetchInfo,
			"poll "+pollInfo+" · "+proxySeg,
		)
	}
	lines = dashboardFitLines(lines, width, height)
	return dashboardPaneStyle(false).Width(width).Render(
		title + "\n" + strings.Join(lines, "\n"))
}

// detailRecord follows the worktree selection: first selected row in table
// order, else the cursor row. The detail pane is a read-only preview, never
// focusable.
func (m dashboardModel) detailRecord() *dashboardRow {
	if len(m.rows) == 0 {
		return nil
	}
	for _, row := range m.rows {
		if m.workSel[row.Rec.Branch] {
			r := row
			return &r
		}
	}
	if m.workCursor >= 0 && m.workCursor < len(m.rows) {
		r := m.rows[m.workCursor]
		return &r
	}
	r := m.rows[0]
	return &r
}

func (m dashboardModel) detailPane(width, height int) string {
	title := dashPaneTitleBlurred.Render("Details (preview)")
	rec := m.detailRecord()
	if rec == nil {
		lines := dashboardFitLines([]string{"  (no worktree selected)"}, width, height)
		if len(lines) > 0 {
			lines[0] = dashDimStyle.Render(lines[0])
		}
		return dashboardPaneStyle(false).Width(width).Render(
			title + "\n" + strings.Join(lines, "\n"))
	}
	var urlCfg *config.Config
	if p := m.curProject(); p != nil {
		urlCfg = p.cfg
	}
	status := rec.Status
	if rec.Stale {
		status += " (stale)"
	}
	if rec.IsMain {
		status += " (main)"
	}
	healthLine := rec.Health
	if healthLine == "" {
		healthLine = "no checks"
	}
	lines := []string{
		"branch:  " + rec.Rec.Branch,
		"slug:    " + rec.Rec.Slug,
		"status:  " + status,
		"health:  " + healthLine,
		"ports:   " + rec.Ports,
		"url:     " + dashboardURLFor(urlCfg, rec.Rec),
		"path:    " + rec.Rec.AbsPath,
		"project: " + rec.Rec.ComposeProject,
	}
	lines = dashboardFitLines(lines, width, height)
	return dashboardPaneStyle(false).Width(width).Render(
		title + "\n" + strings.Join(lines, "\n"))
}

// confirmPane renders the centered blocking remove-confirm popup: a
// bordered box naming the pending action (remove / remove --force),
// listing the target branches, and hinting y/n. It mirrors the menuPane
// pattern (rounded lipgloss border, clamped width, dashConfirmStyle
// accents) so the destructive prompt is impossible to miss.
func (m dashboardModel) confirmPane(width int) string {
	label := m.confirm
	if label == "" {
		label = "remove"
	}
	title := dashConfirmStyle.Render("Confirm " + label)
	question := "Remove these worktrees?"
	if m.pendingForce {
		question = "Force remove these worktrees?"
	}
	targets := strings.Join(m.pendingX, ", ")
	if strings.TrimSpace(targets) == "" {
		targets = "(nothing selected)"
	}
	innerW := max(width-4, 20)
	targets = runewidth.Truncate(targets, innerW, "…")
	hint := dashMenuHintStyle.Render("y/n: y confirms, n or esc cancels")
	body := title + "\n" + question + "\n" + targets + "\n" + hint
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("11")).
		Padding(0, 1).
		Width(width).
		Render(body)
}

// menuPane renders the lazydocker-style Menu popup: a bordered box with
// one "key  description" row per dashboard action, the cursor row
// highlighted. Plain-text rows keep column alignment (no embedded ANSI
// in the measured widths except the selected-row style, which pads
// identically).
func (m dashboardModel) menuPane(width int) string {
	items := dashboardMenuItems()
	if m.menuCursor < 0 || m.menuCursor >= len(items) {
		m.menuCursor = 0
	}
	keyW := 5
	for _, it := range items {
		if len([]rune(it.Key)) > keyW {
			keyW = len([]rune(it.Key))
		}
	}
	innerW := max(width-2, 20)
	lines := make([]string, 0, len(items))
	for i, it := range items {
		keyCell := dashMenuKeyStyle.Render(fmt.Sprintf("%-*s", keyW, it.Key))
		row := fmt.Sprintf("%s  %s", keyCell, it.Desc)
		if i == m.menuCursor {
			// Pad to the inner width so the highlight spans the row.
			pad := innerW - len([]rune(it.Key)) - 2 - len([]rune(it.Desc))
			if pad < 1 {
				pad = 1
			}
			row += strings.Repeat(" ", pad)
			row = dashMenuSelectedStyle.Render(row)
		} else {
			row = " " + row
		}
		lines = append(lines, row)
	}
	title := dashMenuTitleStyle.Render("Menu")
	hint := dashMenuHintStyle.Render("j/k move · enter run · esc close")
	body := title + "\n" + strings.Join(lines, "\n") + "\n" + hint
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("42")).
		Padding(0, 1).
		Width(width).
		Render(body)
}

func (m dashboardModel) logPane(width, height int) string {
	focused := m.pane == 2
	// Same chrome as the table panes (border 2 + padding 2): wrapping
	// wider would let lipgloss clip the tail of every long log line.
	w := max(width-4, 10)
	// Outer pane is title (1) + top/bottom borders (2); the rest is viewport.
	vpH := max(height-3, 3)
	if height <= 0 {
		vpH = m.logView.Height
		if vpH <= 0 {
			vpH = dashboardLogViewportHeight(m.height)
		}
	}
	vpH = max(vpH, 3)
	// Wrap to the current pane width so long lines become multiple lines
	// instead of being cut off horizontally (viewport truncates MaxWidth).
	wrapped := wrapLogLines(m.log, w)
	lv := m.logView
	lv.Width = w
	lv.Height = vpH
	lv.SetContent(strings.Join(wrapped, "\n"))
	// Preserve the user's scroll position: only stick to the bottom when
	// the model view was already there. Never force GotoBottom here —
	// doing so every frame is what made the log unscrollable.
	if m.logView.AtBottom() {
		lv.GotoBottom()
	} else {
		lv.SetYOffset(m.logView.YOffset)
	}
	title := "[3]-Logs (3, j/k scroll) - " + fmt.Sprintf("%d lines", len(wrapped))
	if focused {
		title += " ●"
	}
	if total := len(wrapped); total > vpH {
		remaining := total - vpH - lv.YOffset
		if remaining > 0 {
			title += fmt.Sprintf(" ↑%d more", remaining)
		} else {
			title += " [bottom]"
		}
	}
	titleStyled := dashLogTitleStyle.Render(title)
	if focused {
		titleStyled = dashPaneTitleFocused.Render(title)
	}
	return dashboardPaneStyle(focused).Width(width).Render(
		titleStyled + "\n" + lv.View())
}

// dashboardGrid splits the body (header/footer excluded) into the
// lazygit 5-box grid. Wide terminals (>= dashboardWideLayout) use two
// columns — left: worktrees/branches/projects, right: details/logs.
// Narrow terminals stack all five panes in a single bodyH budget so
// nothing is pushed off-screen. Table/inner heights exclude the 3
// lines of pane chrome (title + top/bottom borders); logViewH is the
// matching log viewport height so Update (paging, scroll) and View
// (render) agree on geometry.
type dashboardGrid struct {
	wide                                            bool
	leftW, rightW                                   int
	workTableH, branchTableH, projInnerH, detInnerH int
	logH                                            int
	logViewW, logViewH                              int
}

func computeDashboardGrid(w, h int) dashboardGrid {
	const footerReserve = 4
	bodyH := max(h-2-1-1-footerReserve, 10)
	if w >= dashboardWideLayout {
		leftW := max(w*35/100, 36)
		leftW = min(leftW, max(w-40, 36))
		rightW := max(w-3-leftW, 20)
		workH := max(bodyH*32/100, 5)
		branchH := max(bodyH*40/100, 5)
		projH := max(bodyH-workH-branchH, 4)
		detH := max(bodyH*35/100, 5)
		logH := max(bodyH-detH, 6)
		return dashboardGrid{
			wide: true, leftW: leftW, rightW: rightW,
			workTableH: max(workH-3, 3), branchTableH: max(branchH-3, 3),
			projInnerH: max(projH-3, 3), detInnerH: max(detH-3, 3),
			logH:     logH,
			logViewW: max(rightW-4, 10), logViewH: max(logH-3, 3),
		}
	}
	workH := max(bodyH*25/100, 4)
	branchH := max(bodyH*25/100, 4)
	detH := max(bodyH*15/100, 3)
	projH := max(bodyH*10/100, 3)
	logH := max(bodyH-workH-branchH-detH-projH, 4)
	return dashboardGrid{
		wide: false, leftW: max(w-2, 10), rightW: max(w-2, 10),
		workTableH: max(workH-3, 3), branchTableH: max(branchH-3, 3),
		projInnerH: max(projH-3, 3), detInnerH: max(detH-3, 3),
		logH:     logH,
		logViewW: max(w-6, 10), logViewH: max(logH-3, 3),
	}
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

	// Lazydocker-style Menu popup: replaces the panes while open.
	if m.showMenu {
		menuW := min(max(w-4, 40), 64)
		m.menuCursor = max(0, min(m.menuCursor, len(dashboardMenuItems())-1))
		b.WriteString(lipgloss.Place(w-2, len(dashboardMenuItems())+6, lipgloss.Center, lipgloss.Top, m.menuPane(menuW)) + "\n")
		if m.statusMsg != "" {
			b.WriteString(dashErrStyle.Render(m.statusMsg) + "\n")
		}
		b.WriteString(m.helpBar(w) + "\n")
		return b.String()
	}

	// Centered blocking remove-confirm popup: replaces the grid panes
	// while open, following the showMenu branch above. Pending confirm
	// wins over the menu in handleKey, so the menu can never open on top.
	if m.confirm != "" {
		confirmW := min(max(w-4, 40), 64)
		b.WriteString(lipgloss.Place(w-2, 8, lipgloss.Center, lipgloss.Center, m.confirmPane(confirmW)) + "\n")
		if m.statusMsg != "" {
			b.WriteString(dashErrStyle.Render(m.statusMsg) + "\n")
		}
		b.WriteString(m.helpBar(w) + "\n")
		return b.String()
	}

	// Lazygit-style 5-box grid (see computeDashboardGrid): left column
	// [1] worktrees (slug+health only), [2] branches, projects/status;
	// right column details of the selected worktree, large scrollable
	// [3] logs (focused pane scrolls with j/k, arrows switch panes).
	grid := computeDashboardGrid(w, h)

	if grid.wide {
		left := lipgloss.JoinVertical(lipgloss.Left,
			m.worktreePane(grid.leftW, grid.workTableH),
			m.branchPane(grid.leftW, grid.branchTableH),
			m.projectPane(grid.leftW, grid.projInnerH),
		)
		right := lipgloss.JoinVertical(lipgloss.Left,
			m.detailPane(grid.rightW, grid.detInnerH),
			m.logPane(grid.rightW, grid.logH),
		)
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right) + "\n")
	} else {
		b.WriteString(m.worktreePane(w-2, grid.workTableH) + "\n")
		b.WriteString(m.detailPane(w-2, grid.detInnerH) + "\n")
		b.WriteString(m.branchPane(w-2, grid.branchTableH) + "\n")
		b.WriteString(m.projectPane(w-2, grid.projInnerH) + "\n")
		b.WriteString(m.logPane(w-2, grid.logH) + "\n")
	}
	if m.statusMsg != "" {
		b.WriteString(dashErrStyle.Render(m.statusMsg) + "\n")
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
