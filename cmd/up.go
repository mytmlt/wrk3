package cmd

import (
	"context"
	"fmt"
	"sync"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/mytmlt/wrk3/internal/ports"
)

var upAll bool

var upCmd = &cobra.Command{
	Use:   "up [branch...] | --all",
	Short: "parallel setup+run (errgroup, prefixed logs)",
	RunE: func(cmd *cobra.Command, args []string) error {
		r, err := resolveConfig()
		if err != nil {
			return err
		}
		recs, err := loadState(r)
		if err != nil {
			return err
		}
		targets, err := resolveTargets(recs, args, upAll)
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
				return upOne(ctx, r, rec, logf)
			})
		}
		if err := g.Wait(); err != nil {
			return err
		}
		if err := markStatus(r, targets, "running"); err != nil {
			return err
		}
		return nil
	},
}

// upOne runs setup entries, compose up, then the run entry.
func upOne(ctx context.Context, r *resolved, rec ports.WorktreeRecord, logf func(string, ...any)) error {
	rn, err := newRunner(r.cfg, rec.Slug)
	if err != nil {
		return fmt.Errorf("up %q: %w", rec.Branch, err)
	}
	env := envFromPorts(rec.Ports)
	for _, s := range r.cfg.Entry.Setup {
		if s == "" {
			continue
		}
		logf("[%s] setup: %s", rec.Slug, s)
		if err := rn.Exec(ctx, rec.AbsPath, shellCmd(s), env); err != nil {
			return fmt.Errorf("up %q setup %q: %w", rec.Branch, s, err)
		}
	}
	logf("[%s] compose up (%s)", rec.Slug, rec.ComposeProject)
	if err := rn.Up(ctx, rec.AbsPath, env); err != nil {
		return fmt.Errorf("up %q compose: %w", rec.Branch, err)
	}
	if s := r.cfg.Entry.Run; s != "" {
		logf("[%s] run: %s", rec.Slug, s)
		if err := rn.Exec(ctx, rec.AbsPath, shellCmd(s), env); err != nil {
			return fmt.Errorf("up %q run %q: %w", rec.Branch, s, err)
		}
	}
	logf("[%s] up", rec.Slug)
	return nil
}

// markStatus reloads state and sets status for targets.
func markStatus(r *resolved, targets []ports.WorktreeRecord, status string) error {
	recs, err := loadState(r)
	if err != nil {
		return err
	}
	want := map[string]struct{}{}
	for _, t := range targets {
		want[t.Branch] = struct{}{}
	}
	for i := range recs {
		if _, ok := want[recs[i].Branch]; ok {
			recs[i].Status = status
		}
	}
	return saveState(r, recs)
}

func init() {
	upCmd.Flags().BoolVar(&upAll, "all", false, "apply to all worktrees")
	rootCmd.AddCommand(upCmd)
}
