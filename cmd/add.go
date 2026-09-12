package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

	"github.com/mytmlt/wrk3/internal/ports"
	"github.com/mytmlt/wrk3/internal/source"
)

var (
	addSelect bool
	addLocal  bool
	addRemote string
	addMine   bool
	addMyPRS  bool
)

var addCmd = &cobra.Command{
	Use:               "add [branch...] | --select | --local | --remote <name> [--mine] [--myprs]",
	Short:             "worktree add + port assign + .env ensure (bare = select)",
	ValidArgsFunction: completeAddBranches,
	RunE: func(cmd *cobra.Command, args []string) error {
		r, err := resolveConfig()
		if err != nil {
			return err
		}
		if addLocal && (addSelect || addRemote != "" || addMine || addMyPRS) {
			return fmt.Errorf("pass only one of --local, --select, --remote/--mine/--myprs")
		}
		if addSelect && (addRemote != "" || addMine || addMyPRS) {
			return fmt.Errorf("pass only one of --local, --select, --remote/--mine/--myprs")
		}
		if addLocal {
			return addLocalMode(cmd, r, args)
		}
		if addRemote != "" || addMine || addMyPRS {
			return addRemoteMode(cmd, r, args)
		}
		branches := args
		selectMode := addSelect
		if selectMode {
			if len(args) > 0 {
				return fmt.Errorf("pass either branch names or --select, not both")
			}
		} else if len(args) == 0 {
			// Bare `add` behaves like --select for fast interactive pick.
			selectMode = true
		}
		if selectMode {
			remote := resolveRemote(r, "")
			if err := r.src.Fetch(r.cfg.RepoPath(), remote); err != nil {
				return fmt.Errorf("fetch: %w", err)
			}
			refs, err := r.src.Refs(r.cfg.RepoPath(), remote)
			if err != nil {
				return fmt.Errorf("list refs: %w", err)
			}
			branches, err = selectBranches(refs)
			if err != nil {
				return err
			}
		}
		if len(branches) == 0 {
			return fmt.Errorf("pass branch names or --select")
		}
		if err := ensureBase(r.base); err != nil {
			return err
		}
		recs, err := loadState(r)
		if err != nil {
			return err
		}
		alloc := r.cfg.Allocator()
		remote := resolveRemote(r, "")
		for _, branch := range branches {
			if branch == "" {
				return fmt.Errorf("branch name must not be empty")
			}
			if existing := findRecord(recs, branch); existing != nil {
				return fmt.Errorf("worktree for %q already exists at %s", branch, existing.AbsPath)
			}
			warns, err := addOne(r, &recs, &alloc, branch, remote)
			if err != nil {
				return err
			}
			warnEnv(cmd, warns)
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "added %s\n", branch); err != nil {
				return fmt.Errorf("write output: %w", err)
			}
		}
		if err := touchProject(r.cfg); err != nil {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: register project: %v\n", err)
		}
		return nil
	},
}

// addRemoteMode bulk-creates worktrees from a git remote:
// fetch <remote> --prune, then create every (or only --mine/--myprs)
// branch, skipping ones already registered. With explicit args (no
// --mine/--myprs) it creates just those branches, resolving remote-only
// names as tracking branches. No interactive picker here — bare
// `add --remote` means "all".
func addRemoteMode(cmd *cobra.Command, r *resolved, args []string) error {
	if (addMine || addMyPRS) && len(args) > 0 {
		return fmt.Errorf("pass either branch names or --mine/--myprs, not both")
	}
	remote := resolveRemote(r, addRemote)
	if err := r.src.Fetch(r.cfg.RepoPath(), remote); err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	if err := ensureBase(r.base); err != nil {
		return err
	}
	recs, err := loadState(r)
	if err != nil {
		return err
	}
	alloc := r.cfg.Allocator()
	// Explicit names + --remote: fail fast on duplicates (typo safety),
	// like plain `add branch...`, but resolve via the given remote.
	if len(args) > 0 {
		for _, branch := range args {
			if branch == "" {
				return fmt.Errorf("branch name must not be empty")
			}
			if existing := findRecord(recs, branch); existing != nil {
				return fmt.Errorf("worktree for %q already exists at %s", branch, existing.AbsPath)
			}
			warns, err := addOne(r, &recs, &alloc, branch, remote)
			if err != nil {
				return err
			}
			warnEnv(cmd, warns)
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "added %s\n", branch); err != nil {
				return fmt.Errorf("write output: %w", err)
			}
		}
		if err := touchProject(r.cfg); err != nil {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: register project: %v\n", err)
		}
		return nil
	}
	var branches []string
	if addMine {
		branches, err = filterRefs(r.src, r.cfg.RepoPath(), remote, true, nil)
		if err != nil {
			return err
		}
		if len(branches) == 0 {
			return fmt.Errorf("no branches for --mine on remote %q (see fetch --mine)", remote)
		}
	} else {
		branches, err = r.src.Refs(r.cfg.RepoPath(), remote)
		if err != nil {
			return fmt.Errorf("list refs: %w", err)
		}
		if len(branches) == 0 {
			return fmt.Errorf("no remote branches on %q", remote)
		}
	}
	if addMyPRS {
		prs, err := myPRBranches(r.cfg.RepoPath(), remote)
		if err != nil {
			return err
		}
		branches = intersectMyPRS(branches, prs)
		if len(branches) == 0 {
			return fmt.Errorf("no branches with open PRs involving you on remote %q (see fetch --myprs)", remote)
		}
	}
	sort.Strings(branches)
	// Branches already checked out anywhere (e.g. main at the repo root)
	// cannot get a second worktree — skip them like already-registered.
	checkedOut := map[string]string{}
	if infos, err := r.src.List(r.cfg.RepoPath()); err == nil {
		for _, info := range infos {
			if info.Branch != "" {
				if _, ok := checkedOut[info.Branch]; !ok {
					checkedOut[info.Branch] = info.Path
				}
			}
		}
	}
	added := 0
	for _, branch := range branches {
		if existing := findRecord(recs, branch); existing != nil {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "skipped %s (already registered)\n", branch); err != nil {
				return fmt.Errorf("write output: %w", err)
			}
			continue
		}
		if path, ok := checkedOut[branch]; ok {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "skipped %s (already checked out at %s)\n", branch, path); err != nil {
				return fmt.Errorf("write output: %w", err)
			}
			continue
		}
		warns, err := addOne(r, &recs, &alloc, branch, remote)
		if err != nil {
			return err
		}
		warnEnv(cmd, warns)
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "added %s\n", branch); err != nil {
			return fmt.Errorf("write output: %w", err)
		}
		added++
	}
	if added == 0 {
		return fmt.Errorf("nothing to add (%d branches, all already registered or checked out)", len(branches))
	}
	if err := touchProject(r.cfg); err != nil {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: register project: %v\n", err)
	}
	return nil
}

// addLocalMode adopts branches that already have local git worktrees
// (git worktree list) into wrk3 state: port assign + .env write, without
// fetching from origin or creating new worktrees.
//
// With explicit args only those branches are adopted (each must have a
// worktree, else error). Bare `add --local` adopts every unregistered
// local worktree, skipping ones already in state.
func addLocalMode(cmd *cobra.Command, r *resolved, args []string) error {
	infos, err := r.src.List(r.cfg.RepoPath())
	if err != nil {
		return fmt.Errorf("list worktrees: %w", err)
	}
	byBranch := localWorktreesByBranch(r, infos)
	if len(args) > 0 {
		recs, err := loadState(r)
		if err != nil {
			return err
		}
		alloc := r.cfg.Allocator()
		for _, branch := range args {
			if branch == "" {
				return fmt.Errorf("branch name must not be empty")
			}
			// Accept branch names and slugs interchangeably.
			resolved := lookupLocalBranch(byBranch, branch)
			if resolved == "" {
				return fmt.Errorf("no local worktree for %q (see git worktree list)", branch)
			}
			branch = resolved
			path := byBranch[branch]
			if existing := findRecord(recs, branch); existing != nil {
				return fmt.Errorf("worktree for %q already exists at %s", branch, existing.AbsPath)
			}
			warns, err := adoptOne(r, &recs, &alloc, branch, path)
			if err != nil {
				return err
			}
			warnEnv(cmd, warns)
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "added %s\n", branch); err != nil {
				return fmt.Errorf("write output: %w", err)
			}
		}
		if err := touchProject(r.cfg); err != nil {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: register project: %v\n", err)
		}
		return nil
	}
	recs, err := loadState(r)
	if err != nil {
		return err
	}
	alloc := r.cfg.Allocator()
	added := 0
	// Sorted for deterministic port index assignment.
	branches := make([]string, 0, len(byBranch))
	for branch := range byBranch {
		branches = append(branches, branch)
	}
	sort.Strings(branches)
	for _, branch := range branches {
		path := byBranch[branch]
		if existing := findRecord(recs, branch); existing != nil {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "skipped %s (already registered)\n", branch); err != nil {
				return fmt.Errorf("write output: %w", err)
			}
			continue
		}
		warns, err := adoptOne(r, &recs, &alloc, branch, path)
		if err != nil {
			return err
		}
		warnEnv(cmd, warns)
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "added %s\n", branch); err != nil {
			return fmt.Errorf("write output: %w", err)
		}
		added++
	}
	if added == 0 {
		return fmt.Errorf("no unregistered local worktrees found (see git worktree list)")
	}
	if err := touchProject(r.cfg); err != nil {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: register project: %v\n", err)
	}
	return nil
}

// localWorktreesByBranch indexes existing worktrees by branch name,
// skipping the main checkout (repo root), bare repos, and detached HEADs
// (no branch). Paths are cleaned for stable comparison.
func localWorktreesByBranch(r *resolved, infos []source.WorktreeInfo) map[string]string {
	out := map[string]string{}
	repoRoot := filepath.Clean(r.cfg.RepoPath())
	for _, info := range infos {
		if info.Branch == "" || info.Bare {
			continue
		}
		// sameRepoRoot tolerates macOS /var -> /private/var symlinks: git
		// reports the resolved path while RepoPath keeps the logical form.
		if sameRepoRoot(info.Path, repoRoot) {
			continue
		}
		if _, ok := out[info.Branch]; !ok {
			out[info.Branch] = filepath.Clean(info.Path)
		}
	}
	return out
}

// lookupLocalBranch resolves a branch-or-slug name to the branch key in
// byBranch, or "" when there is no match.
func lookupLocalBranch(byBranch map[string]string, branchOrSlug string) string {
	if _, ok := byBranch[branchOrSlug]; ok {
		return branchOrSlug
	}
	for branch := range byBranch {
		if source.Slugify(branch) == branchOrSlug {
			return branch
		}
	}
	return ""
}

// addOne creates one worktree: git add + port assign + state + .env ensure.
// The new .env inherits non-managed keys (secrets) from the repo-root
// checkout's .env when present; pre-existing values are never overwritten
// (divergences are returned as warnings). Remote-only branches are created
// as tracking branches (--track -b).
func addOne(r *resolved, recs *[]ports.WorktreeRecord, alloc *ports.Allocator, branch, remote string) ([]string, error) {
	slug := source.Slugify(branch)
	path := worktreePath(r.base, slug)
	if existing := findRecord(*recs, slug); existing != nil && existing.Branch != branch {
		return nil, fmt.Errorf("slug %q for branch %q collides with branch %q", slug, branch, existing.Branch)
	}
	idx := nextIndex(*recs)
	allocation := alloc.Allocate(idx)
	composeProject := r.cfg.ComposeOptions(slug).ProjectName()
	if err := r.src.Add(r.cfg.RepoPath(), branch, path, remote); err != nil {
		return nil, fmt.Errorf("add worktree %q: %w", branch, err)
	}
	warns, err := ensureWorktreeEnv(r, ports.WorktreeRecord{
		Branch: branch, Slug: slug, AbsPath: path, Ports: allocation.Ports,
	})
	if err != nil {
		return nil, err
	}
	*recs = append(*recs, ports.WorktreeRecord{
		Branch:         branch,
		Slug:           slug,
		AbsPath:        path,
		Index:          idx,
		Ports:          allocation.Ports,
		ComposeProject: composeProject,
		Status:         ports.StatusStopped,
	})
	if err := saveState(r, *recs); err != nil {
		return nil, err
	}
	return warns, nil
}

// adoptOne registers one pre-existing worktree path: port assign + state +
// .env ensure. A missing .env is seeded with non-managed keys from the
// repo-root checkout's .env; pre-existing values are never overwritten
// (divergences are returned as warnings). No git worktree add — the checkout
// already exists.
func adoptOne(r *resolved, recs *[]ports.WorktreeRecord, alloc *ports.Allocator, branch, path string) ([]string, error) {
	slug := source.Slugify(branch)
	if existing := findRecord(*recs, slug); existing != nil && existing.Branch != branch {
		return nil, fmt.Errorf("slug %q for branch %q collides with branch %q", slug, branch, existing.Branch)
	}
	if st, err := os.Stat(path); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("adopt worktree %q: path %s missing or not a directory", branch, path)
	}
	idx := nextIndex(*recs)
	allocation := alloc.Allocate(idx)
	composeProject := r.cfg.ComposeOptions(slug).ProjectName()
	warns, err := ensureWorktreeEnv(r, ports.WorktreeRecord{
		Branch: branch, Slug: slug, AbsPath: path, Ports: allocation.Ports,
	})
	if err != nil {
		return nil, err
	}
	*recs = append(*recs, ports.WorktreeRecord{
		Branch:         branch,
		Slug:           slug,
		AbsPath:        path,
		Index:          idx,
		Ports:          allocation.Ports,
		ComposeProject: composeProject,
		Status:         ports.StatusStopped,
	})
	if err := saveState(r, *recs); err != nil {
		return nil, err
	}
	return warns, nil
}

func init() {
	addCmd.Flags().BoolVar(&addSelect, "select", false, "interactive branch select")
	addCmd.Flags().BoolVar(&addLocal, "local", false, "adopt existing local worktrees (no fetch)")
	addCmd.Flags().StringVar(&addRemote, "remote", "", "create worktrees from remote branches (default: source.git.remote, else origin)")
	addCmd.Flags().BoolVar(&addMine, "mine", false, "with --remote: only your branches (tip or branch-exclusive history matches git config user)")
	addCmd.Flags().BoolVar(&addMyPRS, "myprs", false, "with --remote: only branches with an open PR involving you (GitHub remotes only, via gh)")
	_ = addCmd.RegisterFlagCompletionFunc("remote", completeRemotes)
	rootCmd.AddCommand(addCmd)
}
