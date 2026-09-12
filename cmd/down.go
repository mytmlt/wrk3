package cmd

import (
	"context"
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
		g, ctx := errgroup.WithContext(context.Background())
		for _, rec := range targets {
			rec := rec
			g.Go(func() error {
				return downOne(ctx, r, rec, logf)
			})
		}
		if err := g.Wait(); err != nil {
			return err
		}
		return markStatus(r, targets, "stopped")
	},
}

func downOne(ctx context.Context, r *resolved, rec ports.WorktreeRecord, logf func(string, ...any)) error {
	if err := ensureMainEnv(r, rec); err != nil {
		return err
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
