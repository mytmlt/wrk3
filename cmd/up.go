package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/spf13/cobra"

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
		if msg, warn := ensureProxyForUp(r); msg != "" {
			logf("%s", msg)
			r.logProxyResult(msg, "")
		} else if warn != "" {
			logf("warning: %s", warn)
			r.logProxyResult("", warn)
		}
		return runUpTargets(cmd.Context(), r, targets, logf)
	},
}

// upOne runs setup entries, compose up, then the run entry.
func upOne(ctx context.Context, r *resolved, rec ports.WorktreeRecord, logf func(string, ...any)) error {
	if err := os.MkdirAll(rec.AbsPath, 0o755); err != nil {
		return fmt.Errorf("up %q: %w", rec.Branch, err)
	}
	lock, err := runner.LockCompose(ctx, rec.AbsPath)
	if err != nil {
		return fmt.Errorf("up %q: %w", rec.Branch, err)
	}
	defer func() { _ = runner.UnlockCompose(lock) }()
	if err := ensureWorktreeEnv(r, rec, r.cfg.URLSpecs()); err != nil {
		return err
	}
	rn, err := r.runnerFor(rec)
	if err != nil {
		return fmt.Errorf("up %q: %w", rec.Branch, err)
	}
	// Preflight before any setup entry. For compose backends check that
	// compose files exist. Skip for local runner which has no compose step.
	if r.cfg.Runner.Type == "docker" || r.cfg.Runner.Type == "podman" {
		if err := runner.CheckComposeFiles(rec.AbsPath, r.cfg.ComposeFiles()); err != nil {
			return fmt.Errorf("up %q: %w", rec.Branch, err)
		}
	}
	env := envForWorktree(r.cfg, rec)
	for _, s := range r.cfg.Entry.Setup {
		if s == "" {
			continue
		}
		logf("[%s] setup: %s", rec.Slug, s)
		if err := rn.Exec(ctx, rec.AbsPath, shellCmd(s), env); err != nil {
			return fmt.Errorf("up %q setup %q: %w", rec.Branch, s, err)
		}
	}
	if r.cfg.Runner.Type == "local" {
		if s := r.cfg.Entry.Run; s != "" {
			logf("[%s] run: %s", rec.Slug, s)
			if err := rn.Exec(ctx, rec.AbsPath, shellCmd(s), env); err != nil {
				return fmt.Errorf("up %q run %q: %w", rec.Branch, s, err)
			}
		}
		logf("[%s] up", rec.Slug)
		return nil
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
// failed. A plain WaitGroup (not errgroup) is intentional: every target
// runs to completion and per-target errors join at the end, mirroring
// `docker compose` multi-service behavior.
func runUpTargets(ctx context.Context, r *resolved, targets []ports.WorktreeRecord, logf func(string, ...any)) error {
	r.logOpStart("up", "up "+branchesOf(targets))
	if err := markStatus(r, targets, ports.StatusSettingUp); err != nil {
		r.logOpDone("up", "up "+branchesOf(targets), err)
		return err
	}
	errs := make([]error, len(targets))
	var wg sync.WaitGroup
	for i, rec := range targets {
		i, rec := i, rec
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := upOne(ctx, r, rec, logf); err != nil {
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
	r.logOpDone("up", "up "+branchesOf(targets), joined)
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
