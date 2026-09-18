package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var execCmd = &cobra.Command{
	Use:               "exec <branch> -- <cmd>",
	Short:             "run command inside worktree environment",
	Args:              cobra.MinimumNArgs(1),
	ValidArgsFunction: completeWorktreesFirstOnly,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) < 2 {
			return fmt.Errorf("usage: wrk3 exec <branch> -- <cmd> [args...]")
		}
		branch, rest := args[0], args[1:]
		r, err := resolveConfig()
		if err != nil {
			return err
		}
		recs, err := loadState(r)
		if err != nil {
			return err
		}
		rec, err := findRecordIncludingMain(r, recs, branch)
		if err != nil {
			return err
		}
		if rec == nil {
			return fmt.Errorf("unknown worktree %q (see status)", branch)
		}
		if warns, err := ensureWorktreeEnv(r, *rec); err != nil {
			return err
		} else {
			for _, w := range warns {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", w)
			}
		}
		rn, err := r.runnerFor(*rec)
		if err != nil {
			return err
		}
		r.logOpStart("exec", "exec "+rec.Branch+" -- "+strings.Join(rest, " "))
		if err := rn.Exec(cmd.Context(), rec.AbsPath, rest, envForWorktree(r.cfg, *rec)); err != nil {
			r.logOpDone("exec", "exec "+rec.Branch+" -- "+strings.Join(rest, " "), err)
			return fmt.Errorf("exec %q: %w", rec.Branch, err)
		}
		r.logOpDone("exec", "exec "+rec.Branch+" -- "+strings.Join(rest, " "), nil)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(execCmd)
}
