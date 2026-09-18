package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var logsFollow bool

var logsCmd = &cobra.Command{
	Use:               "logs <branch> [-f]",
	Short:             "show worktree logs",
	Args:              cobra.ExactArgs(1),
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
		rec, err := findRecordIncludingMain(r, recs, args[0])
		if err != nil {
			return err
		}
		if rec == nil {
			return fmt.Errorf("unknown worktree %q (see status)", args[0])
		}
		rn, err := r.runnerFor(*rec)
		if err != nil {
			return err
		}
		if s := r.cfg.Entry.Logs; s != "" && !logsFollow {
			if err := rn.Exec(cmd.Context(), rec.AbsPath, shellCmd(s), envForWorktree(r.cfg, *rec)); err != nil {
				return fmt.Errorf("logs %q: %w", rec.Branch, err)
			}
			return nil
		}
		out, err := rn.Logs(cmd.Context(), rec.AbsPath, logsFollow)
		if err != nil {
			return fmt.Errorf("logs %q: %w", rec.Branch, err)
		}
		if _, err := fmt.Fprint(cmd.OutOrStdout(), out); err != nil {
			return fmt.Errorf("write output: %w", err)
		}
		return nil
	},
}

func init() {
	logsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", false, "follow logs")
	rootCmd.AddCommand(logsCmd)
}
