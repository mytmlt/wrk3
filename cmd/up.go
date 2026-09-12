package cmd

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/mytmlt/wrk3/internal/ports"
	"github.com/mytmlt/wrk3/internal/runner"
)

var upCmd = &cobra.Command{
	Use:               "up [branch...]",
	Short:             "setup + compose up + run (bare = all worktrees including main)",
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
		return runUpTargets(context.Background(), r, targets, logf)
	},
}

// upOne runs setup entries, compose up, then the run entry.
func upOne(ctx context.Context, r *resolved, rec ports.WorktreeRecord, logf func(string, ...any)) error {
	if warns, err := ensureWorktreeEnv(r, rec); err != nil {
		return err
	} else {
		for _, w := range warns {
			logf("[%s] warning: %s", rec.Slug, w)
		}
	}
	rn, err := newRunner(r.cfg, rec.Slug)
	if err != nil {
		return fmt.Errorf("up %q: %w", rec.Branch, err)
	}
	// Preflight before any setup entry (which typically runs
	// `docker compose up --wait --build` and would otherwise fail
	// minutes in with a container-name conflict).
	if r.cfg.Runner.Type == "docker" {
		if err := runner.CheckComposeFiles(rec.AbsPath, r.cfg.Runner.Docker.ComposeFiles); err != nil {
			return fmt.Errorf("up %q: %w", rec.Branch, err)
		}
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

// runUpTargets marks every target as setting up up front (so status and
// the dashboard stay honest while setup/run entries execute), runs upOne
// per target in parallel, then marks successes running and failures
// failed. The implicit main worktree has no state-file entry, so it is
// silently skipped by markStatus. Returns the joined per-target errors.
func runUpTargets(ctx context.Context, r *resolved, targets []ports.WorktreeRecord, logf func(string, ...any)) error {
	if err := markStatus(r, targets, ports.StatusSettingUp); err != nil {
		return err
	}
	errs := make([]error, len(targets))
	g, ctx := errgroup.WithContext(ctx)
	for i, rec := range targets {
		i, rec := i, rec
		g.Go(func() error {
			if err := upOne(ctx, r, rec, logf); err != nil {
				errs[i] = err
			}
			return nil
		})
	}
	_ = g.Wait()
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
			return errors.Join(joined, err)
		}
	}
	if len(failed) > 0 {
		if err := markStatus(r, failed, ports.StatusFailed); err != nil {
			return errors.Join(joined, err)
		}
	}
	return joined
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
	rootCmd.AddCommand(upCmd)
}
