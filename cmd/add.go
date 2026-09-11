package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/mytmlt/wrk3/internal/ports"
	"github.com/mytmlt/wrk3/internal/source"
)

var addSelect bool

var addCmd = &cobra.Command{
	Use:   "add <branch...> | --select",
	Short: "worktree add + port assign + .env write",
	RunE: func(cmd *cobra.Command, args []string) error {
		r, err := resolveConfig()
		if err != nil {
			return err
		}
		branches := args
		if addSelect {
			if len(args) > 0 {
				return fmt.Errorf("pass either branch names or --select, not both")
			}
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
		return nil
	},
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

func init() {
	addCmd.Flags().BoolVar(&addSelect, "select", false, "interactive branch select")
	rootCmd.AddCommand(addCmd)
}
