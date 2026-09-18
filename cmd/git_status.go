package cmd

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/mytmlt/wrk3/internal/ports"
	"github.com/mytmlt/wrk3/internal/source"
)

var gitStatusShort bool

var gitStatusCmd = &cobra.Command{
	Use:     "git-status [branch...]",
	Aliases: []string{"gs"},
	Short:   "Show git status for worktrees (bare = all worktrees including main)",
	Long: `Check git status of all local worktrees and display the result.

  wrk3 git-status                  all worktrees including main
  wrk3 git-status feature-a        one worktree (branch names and slugs work)
  wrk3 git-status feature-a --short  same plus the short file list per worktree

Bare args mean all worktrees including the implicit main checkout (docker
compose style, like pull/up/down). Stale entries (missing directory) are
errors, not silent no-ops. Dirty worktrees never fail the command: every
target is probed and per-target git errors join at the end.`,
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
		results := runGitStatusTargets(r, targets)
		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
		if _, err := fmt.Fprintln(w, "WORKTREE\tBRANCH\tGIT\tAHEAD\tBEHIND\tSTAGED\tUNSTAGED\tUNTRACKED"); err != nil {
			return fmt.Errorf("write output: %w", err)
		}
		for _, res := range results {
			if res.statErr != nil {
				if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
					res.rec.Slug, res.rec.Branch, "error", "?", "?", "?", "?", "?"); err != nil {
					return fmt.Errorf("write output: %w", err)
				}
				continue
			}
			git := "clean"
			if !res.status.Clean {
				git = "dirty"
			}
			if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d\t%d\t%d\t%d\n",
				res.rec.Slug, res.rec.Branch, git,
				res.status.Ahead, res.status.Behind,
				res.status.Staged, res.status.Unstaged, res.status.Untracked); err != nil {
				return fmt.Errorf("write output: %w", err)
			}
		}
		if err := w.Flush(); err != nil {
			return fmt.Errorf("write output: %w", err)
		}
		if gitStatusShort {
			out := cmd.OutOrStdout()
			for _, res := range results {
				if res.statErr != nil || len(res.status.Files) == 0 {
					continue
				}
				sorted := append([]string(nil), res.status.Files...)
				sort.Strings(sorted)
				_, _ = fmt.Fprintf(out, "\n[%s] %s\n", res.rec.Slug, strings.Join(sorted, "\n["+res.rec.Slug+"] "))
			}
		}
		return joinGitStatusErrors(results)
	},
}

// gitStatusResult pairs a target with its probed status (or statErr when
// the directory is stale/missing or git failed inside it).
type gitStatusResult struct {
	rec     ports.WorktreeRecord
	status  source.WorktreeStatus
	statErr error
}

// gitStatusOne probes git status for one worktree. Missing directories
// surface as stale errors (same wording as pull/checkout) instead of raw
// git failures.
func gitStatusOne(r *resolved, rec ports.WorktreeRecord) gitStatusResult {
	if st, err := os.Stat(rec.AbsPath); err != nil {
		if os.IsNotExist(err) {
			return gitStatusResult{rec: rec, statErr: fmt.Errorf("worktree %q is stale (directory %s missing); run `wrk3 remove --force %s` to clean state, or re-add", rec.Branch, rec.AbsPath, rec.Branch)}
		}
		return gitStatusResult{rec: rec, statErr: fmt.Errorf("stat worktree %q path %q: %w", rec.Branch, rec.AbsPath, err)}
	} else if !st.IsDir() {
		return gitStatusResult{rec: rec, statErr: fmt.Errorf("worktree %q path %q is not a directory", rec.Branch, rec.AbsPath)}
	}
	st, err := r.src.GitStatus(rec.AbsPath)
	if err != nil {
		return gitStatusResult{rec: rec, statErr: fmt.Errorf("git-status %q: %w", rec.Branch, err)}
	}
	return gitStatusResult{rec: rec, status: st}
}

// runGitStatusTargets probes every target in parallel, preserving input
// order. A WaitGroup (not errgroup) is intentional: every target runs to
// completion and per-target errors join at print time, mirroring pull.
func runGitStatusTargets(r *resolved, targets []ports.WorktreeRecord) []gitStatusResult {
	results := make([]gitStatusResult, len(targets))
	var wg sync.WaitGroup
	for i, rec := range targets {
		i, rec := i, rec
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = gitStatusOne(r, rec)
		}()
	}
	wg.Wait()
	return results
}

// joinGitStatusErrors joins per-target probe errors (nil when all clean).
func joinGitStatusErrors(results []gitStatusResult) error {
	var joined error
	for _, res := range results {
		if res.statErr != nil {
			joined = errors.Join(joined, res.statErr)
		}
	}
	return joined
}

func init() {
	gitStatusCmd.Flags().BoolVar(&gitStatusShort, "short", false, "also print the short file list per dirty worktree")
	rootCmd.AddCommand(gitStatusCmd)
}
