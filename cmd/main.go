package cmd

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/mytmlt/wrk3/internal/config"
	"github.com/mytmlt/wrk3/internal/ports"
	"github.com/mytmlt/wrk3/internal/source"
)

// mainWorktreeIndex is the reserved port index for the implicit main
// worktree (repo root). It is never assigned by nextIndex (which grows
// from max+1 >= 0), so main ports stay stable as managed worktrees are
// added/removed. With defaults (8000+index*100) main gets 7900.
const mainWorktreeIndex = -1

// isMainPath reports whether path is the repo root (main checkout).
func isMainPath(r *resolved, path string) bool {
	if r == nil || r.cfg == nil {
		return false
	}
	return filepath.Clean(path) == filepath.Clean(r.cfg.RepoPath())
}

// mainRecord synthesizes the implicit main worktree record (repo root).
// It returns (nil, nil) when there is no applicable main checkout:
// git list fails, root not listed, bare repo, or detached HEAD.
// It returns an error on port/compose-project collisions with state.
func mainRecord(r *resolved, recs []ports.WorktreeRecord) (*ports.WorktreeRecord, error) {
	infos, err := r.src.List(r.cfg.RepoPath())
	if err != nil {
		return nil, nil
	}
	repoRoot := filepath.Clean(r.cfg.RepoPath())
	var branch string
	found := false
	for _, info := range infos {
		if filepath.Clean(info.Path) != repoRoot {
			continue
		}
		if info.Bare || info.Branch == "" {
			return nil, nil
		}
		branch = info.Branch
		found = true
		break
	}
	if !found {
		return nil, nil
	}
	slug := source.Slugify(branch)
	// Managed state wins: if the branch is already registered, skip main
	// (prevents duplicate targets when state goes stale).
	if existing := findRecord(recs, branch); existing != nil {
		return nil, nil
	}
	if existing := findRecord(recs, slug); existing != nil {
		return nil, fmt.Errorf("main worktree slug %q for branch %q collides with branch %q", slug, branch, existing.Branch)
	}
	alloc := r.cfg.Allocator()
	allocation := alloc.Allocate(mainWorktreeIndex)
	composeProject := r.cfg.ComposeOptions(slug).ProjectName()
	for _, rec := range recs {
		if alloc.IndexesCollide(mainWorktreeIndex, rec.Index) || ports.AllocationsCollide(allocation.Ports, rec.Ports) {
			return nil, fmt.Errorf("main worktree ports collide with worktree %q (ports.base/step leaves no room for reserved index %d)", rec.Branch, mainWorktreeIndex)
		}
		if rec.ComposeProject == composeProject {
			return nil, fmt.Errorf("main worktree compose project %q collides with worktree %q", composeProject, rec.Branch)
		}
	}
	return &ports.WorktreeRecord{
		Branch:         branch,
		Slug:           slug,
		AbsPath:        repoRoot,
		Index:          mainWorktreeIndex,
		Ports:          allocation.Ports,
		ComposeProject: composeProject,
	}, nil
}

// recordsWithMain returns state records plus the implicit main record
// (when present and not already registered), sorted by branch.
func recordsWithMain(r *resolved, recs []ports.WorktreeRecord) ([]ports.WorktreeRecord, error) {
	out := append([]ports.WorktreeRecord(nil), recs...)
	main, err := mainRecord(r, recs)
	if err != nil {
		return nil, err
	}
	if main != nil {
		out = append(out, *main)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Branch < out[j].Branch })
	return out, nil
}

// resolveTargetsWithMain is like resolveTargets but the candidate set
// includes the implicit main worktree. Bare args means all worktrees
// including main (docker compose style).
func resolveTargetsWithMain(r *resolved, recs []ports.WorktreeRecord, args []string) ([]ports.WorktreeRecord, error) {
	all, err := recordsWithMain(r, recs)
	if err != nil {
		return nil, err
	}
	if len(args) == 0 {
		if len(all) == 0 {
			return nil, fmt.Errorf("no worktrees registered")
		}
		return all, nil
	}
	var out []ports.WorktreeRecord
	for _, a := range args {
		rec := findRecord(all, a)
		if rec == nil {
			return nil, fmt.Errorf("unknown worktree %q (see status)", a)
		}
		out = append(out, *rec)
	}
	return out, nil
}

// findRecordIncludingMain matches branch/slug against state plus main.
func findRecordIncludingMain(r *resolved, recs []ports.WorktreeRecord, branchOrSlug string) (*ports.WorktreeRecord, error) {
	all, err := recordsWithMain(r, recs)
	if err != nil {
		return nil, err
	}
	return findRecord(all, branchOrSlug), nil
}

// recordsForDisplay returns state records plus the implicit main record
// for status/ls. Main lookup never fails hard (offline git -> state only),
// but port/project collisions are surfaced as errors.
func recordsForDisplay(cfg *config.Config) ([]ports.WorktreeRecord, error) {
	recs, err := ports.Load(cfg.StatePath())
	if err != nil {
		return nil, fmt.Errorf("load state: %w", err)
	}
	src, err := newSource(cfg)
	if err != nil {
		out := append([]ports.WorktreeRecord(nil), recs...)
		sort.Slice(out, func(i, j int) bool { return out[i].Branch < out[j].Branch })
		return out, nil
	}
	r := &resolved{cfg: cfg, src: src}
	return recordsWithMain(r, recs)
}

// ensureMainEnv writes .env for the implicit main checkout so entry
// commands and compose see allocated ports. No-op for managed worktrees
// (their .env is written at add time).
func ensureMainEnv(r *resolved, rec ports.WorktreeRecord) error {
	if !isMainPath(r, rec.AbsPath) {
		return nil
	}
	if err := ports.Write(rec.AbsPath, rec.Ports); err != nil {
		return fmt.Errorf("write .env for main worktree %q: %w", rec.Branch, err)
	}
	return nil
}
