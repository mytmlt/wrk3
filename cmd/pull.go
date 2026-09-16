package cmd

import (
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/spf13/cobra"

	"github.com/mytmlt/wrk3/internal/ports"
	"github.com/mytmlt/wrk3/internal/source"
)

var (
	pullRebase bool
	pullFFOnly bool
)

var pullCmd = &cobra.Command{
	Use:   "pull [branch...]",
	Short: "git pull in worktrees (bare = all worktrees including main)",
	Long: `Run git pull inside worktrees to fast-forward them from their upstreams.

  wrk3 pull feature-a              pull one worktree (branch names and slugs work)
  wrk3 pull feature-a feature-b    pull several worktrees in parallel
  wrk3 pull                        pull all worktrees including main, in parallel
  wrk3 pull feature-a --rebase     pull with --rebase
  wrk3 pull feature-a --ff-only    pull with --ff-only (refuse merges/rebases)

--rebase and --ff-only are mutually exclusive. Dirty worktrees fail with
the git error (commit, stash, or resolve it yourself, then pull again).
Stale entries (missing directory) are errors, not silent no-ops.`,
	ValidArgsFunction: completeWorktrees,
	RunE: func(cmd *cobra.Command, args []string) error {
		if pullRebase && pullFFOnly {
			return fmt.Errorf("pass either --rebase or --ff-only, not both")
		}
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
		opts := source.PullOptions{Rebase: pullRebase, FFOnly: pullFFOnly}
		out := cmd.OutOrStdout()
		var mu sync.Mutex
		logf := func(format string, a ...any) {
			mu.Lock()
			defer mu.Unlock()
			_, _ = fmt.Fprintf(out, format+"\n", a...)
		}
		return runPullTargets(r, targets, opts, logf)
	},
}

// pullOne runs git pull inside one worktree. Missing directories surface
// as stale errors (same wording as checkout) instead of raw git failures.
func pullOne(r *resolved, rec ports.WorktreeRecord, opts source.PullOptions, logf func(string, ...any)) error {
	if st, err := os.Stat(rec.AbsPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("worktree %q is stale (directory %s missing); run `wrk3 remove --force %s` to clean state, or re-add", rec.Branch, rec.AbsPath, rec.Branch)
		}
		return fmt.Errorf("stat worktree %q path %q: %w", rec.Branch, rec.AbsPath, err)
	} else if !st.IsDir() {
		return fmt.Errorf("worktree %q path %q is not a directory", rec.Branch, rec.AbsPath)
	}
	logf("[%s] pull", rec.Slug)
	if err := r.src.Pull(rec.AbsPath, opts); err != nil {
		return fmt.Errorf("pull %q: %w", rec.Branch, err)
	}
	logf("[%s] pulled", rec.Slug)
	return nil
}

// runPullTargets pulls every target in parallel. A plain WaitGroup (not
// errgroup) is intentional: every target runs to completion and
// per-target errors join at the end, mirroring `up`/`down` multi-service
// behavior.
func runPullTargets(r *resolved, targets []ports.WorktreeRecord, opts source.PullOptions, logf func(string, ...any)) error {
	errs := make([]error, len(targets))
	var wg sync.WaitGroup
	for i, rec := range targets {
		i, rec := i, rec
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := pullOne(r, rec, opts, logf); err != nil {
				errs[i] = err
			}
		}()
	}
	wg.Wait()
	var joined error
	for _, err := range errs {
		if err != nil {
			joined = errors.Join(joined, err)
		}
	}
	return joined
}

func init() {
	pullCmd.Flags().BoolVar(&pullRebase, "rebase", false, "pull with --rebase (rebase local commits onto the fetched tip)")
	pullCmd.Flags().BoolVar(&pullFFOnly, "ff-only", false, "pull with --ff-only (refuse merges/rebases, fast-forward only)")
	rootCmd.AddCommand(pullCmd)
}
