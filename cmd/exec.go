package cmd

import (
	"context"
	"fmt"

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
		rec := findRecord(recs, branch)
		if rec == nil {
			return fmt.Errorf("unknown worktree %q (see status)", branch)
		}
		rn, err := newRunner(r.cfg, rec.Slug)
		if err != nil {
			return err
		}
		if err := rn.Exec(context.Background(), rec.AbsPath, rest, envFromPorts(rec.Ports)); err != nil {
			return fmt.Errorf("exec %q: %w", rec.Branch, err)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(execCmd)
}
