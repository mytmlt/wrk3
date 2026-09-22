package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mytmlt/wrk3/internal/ports"
)

var removeAll bool

var removeForce bool

var removeCmd = &cobra.Command{
	Use:               "remove [branch...] | --all",
	Short:             "compose down -v + worktree remove, cleanup state",
	ValidArgsFunction: completeWorktreesExcludingMain,
	RunE: func(cmd *cobra.Command, args []string) error {
		r, err := resolveConfig()
		if err != nil {
			return err
		}
		recs, err := loadState(r)
		if err != nil {
			return err
		}
		// Reconcile (adopt orphans) + migrate legacy zeros colliding with
		// main at ports.base, persisting when anything changed, so the
		// main guard below never sees a stale colliding allocation.
		if updated, adopted, warns, err := reconcileAndSave(r, recs); err != nil {
			return err
		} else {
			recs = updated
			for _, w := range warns {
				warnf(cmd, "%s", w)
			}
			if len(adopted) > 0 {
				warnf(cmd, "reconciled state: adopted %s", strings.Join(adopted, ", "))
			}
		}
		// The main checkout is implicit and never in state: refuse to
		// remove it by branch/slug instead of reporting "unknown".
		if !removeAll {
			for _, a := range args {
				if main, err := mainRecord(r, recs); err != nil {
					return err
				} else if main != nil && (a == main.Branch || a == main.Slug) {
					return fmt.Errorf("refusing to remove main worktree %q (repo root is always kept)", main.Branch)
				}
			}
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
	r.logOpStart("remove", "remove "+branch)
	rec := findRecord(recs, branch)
	if rec == nil {
		err := fmt.Errorf("unknown worktree %q (see status)", branch)
		r.logOpDone("remove", "remove "+branch, err)
		return err
	}
	if isMainPath(r, rec.AbsPath) {
		err := fmt.Errorf("refusing to remove main worktree %q (repo root is always kept)", rec.Branch)
		r.logOpDone("remove", "remove "+branch, err)
		return err
	}
	rn, err := r.runnerFor(*rec)
	if err != nil {
		r.logOpDone("remove", "remove "+branch, err)
		return err
	}
	env := envForWorktree(r.cfg, *rec)
	if r.cfg.Runner.Type != "none" {
		if err := rn.Down(cmd.Context(), rec.AbsPath, env); err != nil {
			warnf(cmd, "compose down for %q: %v", rec.Branch, err)
		}
	}
	// The worktree's .env may hold user secrets alongside wrk3-managed
	// port keys, so strip only the managed keys instead of deleting the
	// file. StripManaged deletes it when nothing but managed keys remain,
	// keeping plain remove working on otherwise clean worktrees.
	if len(rec.Ports) > 0 {
		if _, err := ports.StripManaged(filepath.Join(rec.AbsPath, ports.EnvFileName), rec.Ports); err != nil {
			warnf(cmd, "strip managed .env keys for %q: %v", rec.Branch, err)
		}
	}
	if err := r.src.Remove(r.cfg.RepoPath(), rec.AbsPath, removeForce); err != nil {
		if !removeForce {
			err = fmt.Errorf("remove worktree %q: %w", rec.Branch, err)
			r.logOpDone("remove", "remove "+branch, err)
			return err
		}
		warnf(cmd, "worktree remove for %q: %v", rec.Branch, err)
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
		r.logOpDone("remove", "remove "+branch, err)
		return err
	}
	r.logOpDone("remove", "removed "+removedBranch, nil)
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "removed %s\n", removedBranch); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	return nil
}

func init() {
	removeCmd.Flags().BoolVar(&removeAll, "all", false, "remove all worktrees")
	removeCmd.Flags().BoolVar(&removeForce, "force", false, "force removal (git worktree remove --force, fallback to rm -rf)")
	rootCmd.AddCommand(removeCmd)
}
