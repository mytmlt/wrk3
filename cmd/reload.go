package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/mytmlt/wrk3/internal/ports"
)

var reloadCmd = &cobra.Command{
	Use:               "reload [branch...]",
	Short:             "run entry.reload commands (bare = all worktrees including main)",
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
		targets, err := resolveTargetsWithMain(r, recs, args)
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		var mu sync.Mutex
		logf := func(format string, a ...any) {
			mu.Lock()
			defer mu.Unlock()
			_, _ = fmt.Fprintf(out, format+"\n", a...)
		}
		return runReloadTargets(cmd.Context(), r, targets, logf)
	},
}

// effectiveReload returns the non-empty entry.reload commands.
func effectiveReload(r *resolved) []string {
	var out []string
	if r == nil || r.cfg == nil {
		return nil
	}
	for _, s := range r.cfg.Entry.Reload {
		if strings.TrimSpace(s) == "" {
			continue
		}
		out = append(out, s)
	}
	return out
}

// reloadOne runs the entry.reload commands inside one worktree.
func reloadOne(ctx context.Context, r *resolved, rec ports.WorktreeRecord, logf func(string, ...any)) error {
	if err := ensureWorktreeEnv(r, rec, r.cfg.URLSpecs()); err != nil {
		return err
	}
	cmds := effectiveReload(r)
	if len(cmds) == 0 {
		return fmt.Errorf("reload %q: entry.reload is not set (define entry.reload in wrk3.yaml)", rec.Branch)
	}
	rn, err := r.runnerFor(rec)
	if err != nil {
		return fmt.Errorf("reload %q: %w", rec.Branch, err)
	}
	env := envForWorktree(r.cfg, rec)
	for _, s := range cmds {
		logf("[%s] reload: %s", rec.Slug, s)
		if err := rn.Exec(ctx, rec.AbsPath, shellCmd(s), env); err != nil {
			return fmt.Errorf("reload %q %q: %w", rec.Branch, s, err)
		}
	}
	logf("[%s] reloaded", rec.Slug)
	return nil
}

// runReloadTargets marks every target as setting up up front (so status and
// the dashboard stay honest while reload entries execute), runs reloadOne
// per target in parallel, then marks successes running and failures
// failed. A plain WaitGroup (not errgroup) is intentional: every target
// runs to completion and per-target errors join at the end, mirroring
// `docker compose` multi-service behavior.
func runReloadTargets(ctx context.Context, r *resolved, targets []ports.WorktreeRecord, logf func(string, ...any)) error {
	if len(effectiveReload(r)) == 0 {
		return fmt.Errorf("entry.reload is not set (define entry.reload in wrk3.yaml)")
	}
	r.logOpStart("reload", "reload "+branchesOf(targets))
	if err := markStatus(r, targets, ports.StatusSettingUp); err != nil {
		r.logOpDone("reload", "reload "+branchesOf(targets), err)
		return err
	}
	errs := make([]error, len(targets))
	var wg sync.WaitGroup
	for i, rec := range targets {
		i, rec := i, rec
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := reloadOne(ctx, r, rec, logf); err != nil {
				errs[i] = err
			}
		}()
	}
	wg.Wait()
	var succeeded, failed []ports.WorktreeRecord
	var joined error
	for i, rec := range targets {
		if errs[i] != nil {
			failed = append(failed, rec)
			joined = errors.Join(joined, errs[i])
		} else {
			succeeded = append(succeeded, rec)
		}
	}
	if len(succeeded) > 0 {
		if err := markStatus(r, succeeded, ports.StatusRunning); err != nil {
			joined = errors.Join(joined, err)
		}
	}
	if len(failed) > 0 {
		if err := markStatus(r, failed, ports.StatusFailed); err != nil {
			joined = errors.Join(joined, err)
		}
	}
	r.logOpDone("reload", "reload "+branchesOf(targets), joined)
	return joined
}

func init() {
	rootCmd.AddCommand(reloadCmd)
}
