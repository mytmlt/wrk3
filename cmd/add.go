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
)

var addCmd = &cobra.Command{
	Use:               "add [branch...] | --select | --local",
	Short:             "worktree add + port assign + .env write (bare = select)",
	ValidArgsFunction: completeAddBranches,
	RunE: func(cmd *cobra.Command, args []string) error {
		r, err := resolveConfig()
		if err != nil {
			return err
		}
		if addLocal && addSelect {
			return fmt.Errorf("pass either --local or --select, not both")
		}
		if addLocal {
			return addLocalMode(cmd, r, args)
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
			if err := r.src.Fetch(r.cfg.RepoPath()); err != nil {
				return fmt.Errorf("fetch: %w", err)
			}
			refs, err := r.src.Refs(r.cfg.RepoPath())
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
		for _, branch := range branches {
			if branch == "" {
				return fmt.Errorf("branch name must not be empty")
			}
			if existing := findRecord(recs, branch); existing != nil {
				return fmt.Errorf("worktree for %q already exists at %s", branch, existing.AbsPath)
			}
			if err := addOne(r, &recs, &alloc, branch); err != nil {
				return err
			}
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
			if err := adoptOne(r, &recs, &alloc, branch, path); err != nil {
				return err
			}
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
		if err := adoptOne(r, &recs, &alloc, branch, path); err != nil {
			return err
		}
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
		if filepath.Clean(info.Path) == repoRoot {
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

// addOne creates one worktree: git add + port assign + state + .env.
func addOne(r *resolved, recs *[]ports.WorktreeRecord, alloc *ports.Allocator, branch string) error {
	slug := source.Slugify(branch)
	path := worktreePath(r.base, slug)
	if existing := findRecord(*recs, slug); existing != nil && existing.Branch != branch {
		return fmt.Errorf("slug %q for branch %q collides with branch %q", slug, branch, existing.Branch)
	}
	idx := nextIndex(*recs)
	allocation := alloc.Allocate(idx)
	composeProject := r.cfg.ComposeOptions(slug).ProjectName()
	if err := r.src.Add(r.cfg.RepoPath(), branch, path); err != nil {
		return fmt.Errorf("add worktree %q: %w", branch, err)
	}
	if err := ports.Write(path, allocation.Ports); err != nil {
		return fmt.Errorf("write .env for %q: %w", branch, err)
	}
	*recs = append(*recs, ports.WorktreeRecord{
		Branch:         branch,
		Slug:           slug,
		AbsPath:        path,
		Index:          idx,
		Ports:          allocation.Ports,
		ComposeProject: composeProject,
		Status:         "stopped",
	})
	if err := saveState(r, *recs); err != nil {
		return err
	}
	return nil
}

// adoptOne registers one pre-existing worktree path: port assign +
// state + .env. No git worktree add — the checkout already exists.
func adoptOne(r *resolved, recs *[]ports.WorktreeRecord, alloc *ports.Allocator, branch, path string) error {
	slug := source.Slugify(branch)
	if existing := findRecord(*recs, slug); existing != nil && existing.Branch != branch {
		return fmt.Errorf("slug %q for branch %q collides with branch %q", slug, branch, existing.Branch)
	}
	if st, err := os.Stat(path); err != nil || !st.IsDir() {
		return fmt.Errorf("adopt worktree %q: path %s missing or not a directory", branch, path)
	}
	idx := nextIndex(*recs)
	allocation := alloc.Allocate(idx)
	composeProject := r.cfg.ComposeOptions(slug).ProjectName()
	if err := ports.Write(path, allocation.Ports); err != nil {
		return fmt.Errorf("write .env for %q: %w", branch, err)
	}
	*recs = append(*recs, ports.WorktreeRecord{
		Branch:         branch,
		Slug:           slug,
		AbsPath:        path,
		Index:          idx,
		Ports:          allocation.Ports,
		ComposeProject: composeProject,
		Status:         "stopped",
	})
	if err := saveState(r, *recs); err != nil {
		return err
	}
	return nil
}

func init() {
	addCmd.Flags().BoolVar(&addSelect, "select", false, "interactive branch select")
	addCmd.Flags().BoolVar(&addLocal, "local", false, "adopt existing local worktrees (no fetch from origin)")
	rootCmd.AddCommand(addCmd)
}
