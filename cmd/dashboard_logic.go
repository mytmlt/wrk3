package cmd

import (
	"sort"
	"strconv"

	"github.com/mytmlt/wrk3/internal/config"
	"github.com/mytmlt/wrk3/internal/ports"
	"github.com/mytmlt/wrk3/internal/source"
)

// dashboardRow is one worktree row for the dashboard worktree pane.
// Status/Ports mirror `status` cells; Stale is true when the dir is missing.
type dashboardRow struct {
	Rec    ports.WorktreeRecord
	Status string
	Ports  string
	Stale  bool
	IsMain bool
}

// branchEntry is one remote branch for the dashboard branch pane.
type branchEntry struct {
	Name       string
	Registered bool
	CheckedOut bool
	// Adoptable marks an on-disk worktree missing from state
	// (CheckedOut && !Registered): selectable so `a` adopts it via
	// adoptOne instead of being dimmed like registered branches.
	Adoptable bool
	Selected  bool
}

// selectable reports whether the entry can be queued for add/adopt.
func (e branchEntry) selectable() bool {
	return !e.Registered && (!e.CheckedOut || e.Adoptable)
}

// dashboardProjectDesc identifies one switchable project.
type dashboardProjectDesc struct {
	Name       string
	ConfigPath string
	Current    bool
}

// mapDashboardRows converts records to rows using a status lookup.
// statusFn reports (status, ports, stale) per record; callers wire the live
// probe (os.Stat + runner) or a stub in tests.
func mapDashboardRows(recs []ports.WorktreeRecord, mainBranch string, statusFn func(ports.WorktreeRecord) (string, string, bool)) []dashboardRow {
	rows := make([]dashboardRow, 0, len(recs))
	for _, rec := range recs {
		status, portText, stale := statusFn(rec)
		rows = append(rows, dashboardRow{
			Rec:    rec,
			Status: status,
			Ports:  portText,
			Stale:  stale,
			IsMain: mainBranch != "" && rec.Branch == mainBranch && rec.Index == mainWorktreeIndex,
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Rec.Branch < rows[j].Rec.Branch })
	return rows
}

// unregisteredBranchEntries builds the branch pane: every ref sorted, with
// already-registered (branch or slug match via findRecord) flagged so the
// TUI dims them instead of offering add. Checked-out-but-unregistered
// branches (orphan worktrees missing from state) are flagged Adoptable so
// the TUI offers adopt via adoptOne. Refs whose slug collides with another
// registered branch are also flagged: addOne would refuse them with a
// slug-collision error.
func unregisteredBranchEntries(refs []string, recs []ports.WorktreeRecord, checkedOut map[string]bool, keepSelected map[string]bool) []branchEntry {
	sorted := append([]string(nil), refs...)
	sort.Strings(sorted)
	slugs := map[string]bool{}
	for _, rec := range recs {
		slugs[rec.Slug] = true
	}
	out := make([]branchEntry, 0, len(sorted))
	for _, ref := range sorted {
		registered := findRecord(recs, ref) != nil || slugs[source.Slugify(ref)]
		_, co := checkedOut[ref]
		adoptable := co && !registered
		out = append(out, branchEntry{
			Name:       ref,
			Registered: registered,
			CheckedOut: co,
			Adoptable:  adoptable,
			Selected:   keepSelected[ref] && !registered && (!co || adoptable),
		})
	}
	return out
}

// checkedOutSet indexes worktree infos by branch (non-bare, non-detached).
func checkedOutSet(infos []source.WorktreeInfo) map[string]bool {
	out := map[string]bool{}
	for _, info := range infos {
		if info.Branch == "" || info.Bare {
			continue
		}
		out[info.Branch] = true
	}
	return out
}

// nextAppPort previews the app port the next added worktree would get
// (first collision-free managed index >= max(index)+1, i.e. base+step
// onwards since index 0 is the main checkout). Returns -1 when unknown
// or the port space is exhausted.
func nextAppPort(cfg *config.Config, recs []ports.WorktreeRecord) int {
	if cfg == nil {
		return -1
	}
	alloc := cfg.Allocator()
	idx, ok := nextFreeIndex(alloc, recs)
	if !ok {
		return -1
	}
	allocation := alloc.Allocate(idx)
	if allocation.Ports == nil {
		return -1
	}
	v, ok := allocation.Ports[ports.PortApp]
	if !ok {
		return -1
	}
	return v
}

// dashboardProjectDescs merges the registry list with the current config so
// the switcher always contains the repo the dashboard was launched from.
// Entries dedupe by config path; sorted by name with current first on ties.
func dashboardProjectDescs(currentName, currentPath string, names []string, paths []string) []dashboardProjectDesc {
	seen := map[string]bool{}
	var out []dashboardProjectDesc
	push := func(name, path string, current bool) {
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		if name == "" {
			name = path
		}
		out = append(out, dashboardProjectDesc{Name: name, ConfigPath: path, Current: current})
	}
	push(currentName, currentPath, true)
	for i := range names {
		path := ""
		if i < len(paths) {
			path = paths[i]
		}
		if path == currentPath {
			continue
		}
		name := ""
		if i < len(names) {
			name = names[i]
		}
		push(name, path, false)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Current != out[j].Current {
			return out[i].Current
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ConfigPath < out[j].ConfigPath
	})
	return out
}

// dashboardURLFor returns the clickable link for a worktree row: the
// gateway <slug>.<domain> URL when proxy.enabled, else plain
// localhost:<appPort>. Returns "?" when neither is known (no app port).
func dashboardURLFor(cfg *config.Config, rec ports.WorktreeRecord) string {
	if cfg != nil && cfg.Proxy.Enabled {
		return cfg.ProxyURL(rec.Slug)
	}
	if rec.Ports != nil {
		if app, ok := rec.Ports[ports.PortApp]; ok && app > 0 && app <= 65535 {
			return "http://localhost:" + strconv.Itoa(app)
		}
	}
	return "?"
}

// selectedKeys returns the sorted keys of a selection set.
func selectedKeys(set map[string]bool) []string {
	var out []string
	for k, v := range set {
		if v {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}
