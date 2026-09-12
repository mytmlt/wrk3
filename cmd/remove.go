package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/mytmlt/wrk3/internal/ports"
)

var removeAll bool

var removeForce bool

var removeCmd = &cobra.Command{
	Use:               "remove [branch...] | --all",
	Short:             "compose down -v + worktree remove, cleanup state",
	ValidArgsFunction: completeWorktrees,
	RunE: func(cmd *cobra.Command, args []string) error {
		r, err := resolveConfig()
		if err != nil {
			return err
		}
		recs, err := loadState(r)
		if err != nil {
			return err
		}
		targets, err := resolveTargetsRequired(recs, args, removeAll)
		if err != nil {
			return err
		}
		for _, target := range targets {
			if err := removeOne(cmd, r, recs, target.Branch); err != nil {
				return err
			}
			// Refresh recs view after each removal.
			recs, err = loadState(r)
			if err != nil {
				return err
			}
		}
		return nil
	},
}

func removeOne(cmd *cobra.Command, r *resolved, recs []ports.WorktreeRecord, branch string) error {
	rec := findRecord(recs, branch)
	if rec == nil {
		return fmt.Errorf("unknown worktree %q (see status)", branch)
	}
	rn, err := newRunner(r.cfg, rec.Slug)
	if err != nil {
		return err
	}
	env := envFromPorts(rec.Ports)
	if err := rn.Down(context.Background(), rec.AbsPath, env); err != nil {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: compose down for %q: %v\n", rec.Branch, err)
	}
	// The generated .env is untracked and would block a
	// non-force `git worktree remove`; delete it first so plain
	// remove works on otherwise clean worktrees.
	_ = os.Remove(filepath.Join(rec.AbsPath, ports.EnvFileName))
	if err := r.src.Remove(r.cfg.RepoPath(), rec.AbsPath, removeForce); err != nil {
		if !removeForce {
			return fmt.Errorf("remove worktree %q: %w", rec.Branch, err)
		}
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: worktree remove for %q: %v\n", rec.Branch, err)
		_ = os.RemoveAll(rec.AbsPath)
	}
	removedBranch := rec.Branch
	var kept []ports.WorktreeRecord
	for _, existing := range recs {
		if existing.Branch == removedBranch {
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
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "removed %s\n", removedBranch); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	return nil
}

func init() {
	removeCmd.Flags().BoolVar(&removeAll, "all", false, "remove all worktrees")
	removeCmd.Flags().BoolVar(&removeForce, "force", false, "force removal")
	rootCmd.AddCommand(removeCmd)
}
