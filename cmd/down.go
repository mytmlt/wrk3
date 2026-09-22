package cmd

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/spf13/cobra"

	"github.com/mytmlt/wrk3/internal/ports"
)

var downCmd = &cobra.Command{
	Use:               "down [branch...]",
	Short:             "stop worktrees (bare = all worktrees including main)",
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
		return runDownTargets(cmd.Context(), r, targets, logf)
	},
}

// runDownTargets marks every target as stopping up front (so status and
// the dashboard stay honest while compose down executes), runs downOne
// per target in parallel, then marks successes stopped. A plain WaitGroup
// is intentional: every target runs to completion and per-target errors
// join at the end.
func runDownTargets(ctx context.Context, r *resolved, targets []ports.WorktreeRecord, logf func(string, ...any)) error {
	r.logOpStart("down", "down "+branchesOf(targets))
	if err := markStatus(r, targets, ports.StatusStopping); err != nil {
		r.logOpDone("down", "down "+branchesOf(targets), err)
		return err
	}
	errs := make([]error, len(targets))
	var wg sync.WaitGroup
	for i, rec := range targets {
		i, rec := i, rec
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := downOne(ctx, r, rec, logf); err != nil {
				errs[i] = err
			}
		}()
	}
	wg.Wait()
	var succeeded []ports.WorktreeRecord
	var joined error
	for i, rec := range targets {
		if errs[i] != nil {
			joined = errors.Join(joined, errs[i])
		} else {
			succeeded = append(succeeded, rec)
		}
	}
	if len(succeeded) > 0 {
		if err := markStatus(r, succeeded, ports.StatusStopped); err != nil {
			joined = errors.Join(joined, err)
		}
	}
	r.logOpDone("down", "down "+branchesOf(targets), joined)
	return joined
}

func downOne(ctx context.Context, r *resolved, rec ports.WorktreeRecord, logf func(string, ...any)) error {
	if err := ensureWorktreeEnv(r, rec, r.cfg.URLSpecs()); err != nil {
		return err
	}
	rn, err := r.runnerFor(rec)
	if err != nil {
		return fmt.Errorf("down %q: %w", rec.Branch, err)
	}
	env := envForWorktree(r.cfg, rec)
	if s := r.cfg.Entry.Stop; s != "" {
		logf("[%s] stop: %s", rec.Slug, s)
		if err := rn.Exec(ctx, rec.AbsPath, shellCmd(s), env); err != nil {
			logf("[%s] warning: stop %q: %v", rec.Slug, s, err)
		}
	}
	if r.cfg.Runner.Type != "none" {
		logf("[%s] compose down (%s)", rec.Slug, rec.ComposeProject)
		if err := rn.Down(ctx, rec.AbsPath, env); err != nil {
			return fmt.Errorf("down %q: %w", rec.Branch, err)
		}
	}
	logf("[%s] down", rec.Slug)
	return nil
}

func init() {
	rootCmd.AddCommand(downCmd)
}
