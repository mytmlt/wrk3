package cmd

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

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
		return runDownTargets(context.Background(), r, targets, logf)
	},
}

// runDownTargets marks every target as stopping up front (so status and
// the dashboard stay honest while compose down executes), runs downOne
// per target in parallel, then marks successes stopped. Failures keep the
// stopping status until the next down fixes them (same pattern as failed
// for up). The implicit main worktree has no state-file entry, so it is
// silently skipped by markStatus. Returns the joined per-target errors.
func runDownTargets(ctx context.Context, r *resolved, targets []ports.WorktreeRecord, logf func(string, ...any)) error {
	if err := markStatus(r, targets, ports.StatusStopping); err != nil {
		return err
	}
	errs := make([]error, len(targets))
	g, ctx := errgroup.WithContext(ctx)
	for i, rec := range targets {
		i, rec := i, rec
		g.Go(func() error {
			if err := downOne(ctx, r, rec, logf); err != nil {
				errs[i] = err
			}
			return nil
		})
	}
	_ = g.Wait()
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
			return errors.Join(joined, err)
		}
	}
	return joined
}

func downOne(ctx context.Context, r *resolved, rec ports.WorktreeRecord, logf func(string, ...any)) error {
	if warns, err := ensureWorktreeEnv(r, rec); err != nil {
		return err
	} else {
		for _, w := range warns {
			logf("[%s] warning: %s", rec.Slug, w)
		}
	}
	rn, err := newRunner(r.cfg, rec.Slug)
	if err != nil {
		return fmt.Errorf("down %q: %w", rec.Branch, err)
	}
	env := envFromPorts(rec.Ports)
	logf("[%s] compose down (%s)", rec.Slug, rec.ComposeProject)
	if err := rn.Down(ctx, rec.AbsPath, env); err != nil {
		return fmt.Errorf("down %q: %w", rec.Branch, err)
	}
	logf("[%s] down", rec.Slug)
	return nil
}

func init() {
	rootCmd.AddCommand(downCmd)
}
