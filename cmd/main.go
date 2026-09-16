package cmd

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

	"github.com/mytmlt/wrk3/internal/config"
	"github.com/mytmlt/wrk3/internal/ports"
	"github.com/mytmlt/wrk3/internal/source"
)

// mainWorktreeIndex is the reserved port index for the implicit main
// worktree (repo root). Allocate(0) is exactly the ports.base allocation,
// so main always serves the ports written in wrk3.yaml. It is never
// assigned by nextIndex (which grows from max+1 >= 1), so main ports stay
// stable as managed worktrees are added/removed. With defaults
// (8000+index*100) main gets 8000 and the first managed worktree 8100.
const mainWorktreeIndex = 0

// sameRepoRoot compares a git-reported worktree path with the repo root,
// tolerating macOS /var -> /private/var symlinks (git reports the resolved
// path while RepoPath keeps the logical TempDir form).
func sameRepoRoot(worktreePath, repoRoot string) bool {
	if filepath.Clean(worktreePath) == filepath.Clean(repoRoot) {
		return true
	}
	norm := func(p string) string {
		if resolved, err := filepath.EvalSymlinks(p); err == nil {
			return filepath.Clean(resolved)
		}
		return filepath.Clean(p)
	}
	return norm(worktreePath) == norm(repoRoot)
}

// isMainPath reports whether path is the repo root (main checkout).
func isMainPath(r *resolved, path string) bool {
	if r == nil || r.cfg == nil {
		return false
	}
	return sameRepoRoot(path, r.cfg.RepoPath())
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
		if !sameRepoRoot(info.Path, repoRoot) {
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
			return nil, fmt.Errorf("main worktree ports collide with worktree %q (main reserves index %d, the ports.base allocation)", rec.Branch, mainWorktreeIndex)
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
// includes the implicit main worktree. On-disk worktrees missing from
// state are reconciled (adopted) first, then live runtime state is synced
// back (so bare up/down decides on fresh running/stopped), so bare up/down covers them.
// Bare args means all worktrees including main (docker compose style).
func resolveTargetsWithMain(r *resolved, recs []ports.WorktreeRecord, args []string) ([]ports.WorktreeRecord, error) {
	recs, adopted, warns, err := reconcileAndSave(r, recs)
	if err != nil {
		return nil, err
	}
	logReconciledCLI(adopted, warns)
	recs, changed, err := syncRuntimeAndSave(r, recs)
	if err != nil {
		return nil, err
	}
	logSyncedCLI(changed)
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
// Orphan on-disk worktrees are reconciled (adopted) first, then live
// runtime state is synced so callers decide on fresh status.
func findRecordIncludingMain(r *resolved, recs []ports.WorktreeRecord, branchOrSlug string) (*ports.WorktreeRecord, error) {
	recs, adopted, warns, err := reconcileAndSave(r, recs)
	if err != nil {
		return nil, err
	}
	logReconciledCLI(adopted, warns)
	recs, changed, err := syncRuntimeAndSave(r, recs)
	if err != nil {
		return nil, err
	}
	logSyncedCLI(changed)
	all, err := recordsWithMain(r, recs)
	if err != nil {
		return nil, err
	}
	return findRecord(all, branchOrSlug), nil
}

// recordsForDisplay returns state records plus the implicit main record
// for status/ls. Orphan on-disk worktrees are reconciled (adopted and
// saved) first, so a deleted state file rebuilds from git worktrees.
// Main lookup never fails hard (offline git -> state only),
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
	r := &resolved{cfg: cfg, src: src, base: cfg.AbsWorktreeBase(), stateP: cfg.StatePath()}
	recs, adopted, warns, err := reconcileAndSave(r, recs)
	if err != nil {
		return nil, err
	}
	logReconciledCLI(adopted, warns)
	recs, changed, err := syncRuntimeAndSave(r, recs)
	if err != nil {
		return nil, err
	}
	logSyncedCLI(changed)
	return recordsWithMain(r, recs)
}

// ensureWorktreeEnv guarantees rec's worktree .env contains the wrk3-managed
// port section, seeding non-managed keys (secrets) from the repo-root
// checkout's .env when the file is missing. It applies to every worktree —
// managed worktrees as well as the implicit main checkout (for which the
// seed is itself, so only gap-filling happens). Existing values are never
// overwritten; managed keys already set to a different value come back as
// warnings for the caller to report.
func ensureWorktreeEnv(r *resolved, rec ports.WorktreeRecord) ([]string, error) {
	seed := filepath.Join(r.cfg.RepoPath(), ports.EnvFileName)
	_, diverged, err := ports.EnsureInherited(rec.AbsPath, seed, rec.Ports)
	if err != nil {
		return nil, fmt.Errorf("write .env for worktree %q: %w", rec.Branch, err)
	}
	// Gateway URL: append-only APP_URL when proxy.enabled (never overwrite).
	if extra := proxyEnvForSlug(r.cfg, rec.Slug); len(extra) > 0 {
		if _, err := ports.EnsureKeys(rec.AbsPath, extra); err != nil {
			return nil, fmt.Errorf("write .env for worktree %q: %w", rec.Branch, err)
		}
	}
	if len(diverged) == 0 {
		return nil, nil
	}
	desired, err := ports.ManagedValues(rec.Ports)
	if err != nil {
		return nil, fmt.Errorf("write .env for worktree %q: %w", rec.Branch, err)
	}
	var warns []string
	for _, k := range sortedKeys(diverged) {
		warns = append(warns, fmt.Sprintf(
			".env for %q already sets %s=%s (wrk3 allocation %s); leaving intact",
			rec.Branch, k, diverged[k], desired[k]))
	}
	return warns, nil
}

// sortedKeys returns the map keys in sorted order for stable output.
func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// warnEnv prints .env divergence warnings to stderr.
func warnEnv(cmd *cobra.Command, warns []string) {
	for _, w := range warns {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", w)
	}
}
