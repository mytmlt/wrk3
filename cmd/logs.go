package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

var logsFollow bool

var logsCmd = &cobra.Command{
	Use:   "logs <branch> [-f]",
	Short: "show worktree logs",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		r, err := resolveConfig()
		if err != nil {
			return err
		}
		recs, err := loadState(r)
		if err != nil {
			return err
		}
		rec := findRecord(recs, args[0])
		if rec == nil {
			return fmt.Errorf("unknown worktree %q (see status)", args[0])
		}
		rn, err := newRunner(r.cfg, rec.Slug)
		if err != nil {
			return err
		}
		out, err := rn.Logs(context.Background(), rec.AbsPath, logsFollow)
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
