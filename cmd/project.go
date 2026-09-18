package cmd

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/mytmlt/wrk3/internal/config"
	"github.com/mytmlt/wrk3/internal/ports"
)

// projectCmd groups project registry commands.
var projectCmd = &cobra.Command{
	Use:               "project",
	Short:             "List auto-registered projects",
	ValidArgsFunction: cobra.NoFileCompletions,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var projectLsCmd = &cobra.Command{
	Use:               "ls",
	Short:             "List known projects (NAME/CONFIG/WORKTREES)",
	Aliases:           []string{"list"},
	Args:              cobra.NoArgs,
	ValidArgsFunction: cobra.NoFileCompletions,
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := newProjectStore()
		if err != nil {
			return fmt.Errorf("open project registry: %w", err)
		}
		projects, err := store.List()
		if err != nil {
			return fmt.Errorf("list projects: %w", err)
		}
		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
		if _, err := fmt.Fprintln(w, "NAME\tCONFIG\tWORKTREES"); err != nil {
			return fmt.Errorf("write output: %w", err)
		}
		for _, p := range projects {
			count := projectWorktreeCount(p.ConfigPath)
			if _, err := fmt.Fprintf(w, "%s\t%s\t%s\n", p.Name, p.ConfigPath, count); err != nil {
				return fmt.Errorf("write output: %w", err)
			}
		}
		return w.Flush()
	},
}

// projectWorktreeCount returns the worktree count for a config path
// as a display cell: "?" when the config/state is unreadable.
func projectWorktreeCount(configPath string) string {
	cfg, err := config.Load(configPath)
	if err != nil {
		return "?"
	}
	recs, err := ports.Load(cfg.StatePath())
	if err != nil {
		return "?"
	}
	return fmt.Sprintf("%d", len(recs))
}

func init() {
	projectCmd.AddCommand(projectLsCmd)
	rootCmd.AddCommand(projectCmd)
}
