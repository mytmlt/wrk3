package cmd

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/mytmlt/wrk3/internal/config"
)

var lsProject string

var lsCmd = &cobra.Command{
	Use:               "ls",
	Short:             "List worktrees for local project (or --project NAME)",
	Args:              cobra.NoArgs,
	ValidArgsFunction: cobra.NoFileCompletions,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := resolveLsConfig(cmd)
		if err != nil {
			return err
		}
		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
		if _, err := fmt.Fprintln(w, "WORKTREE\tBRANCH\tSTATUS\tPORTS"); err != nil {
			return fmt.Errorf("write output: %w", err)
		}
		if err := printLsStatus(w, cfg); err != nil {
			return err
		}
		return w.Flush()
	},
}

// resolveLsConfig resolves the config for ls:
// --project NAME wins via the registry, otherwise cwd/-f discovery.
func resolveLsConfig(cmd *cobra.Command) (*config.Config, error) {
	if lsProject != "" {
		if fileFlag != "" {
			return nil, fmt.Errorf("pass either --project or -f/--file, not both")
		}
		store, err := newProjectStore()
		if err != nil {
			return nil, fmt.Errorf("open project registry: %w", err)
		}
		p, err := store.Get(lsProject)
		if err != nil {
			return nil, err
		}
		cfg, err := config.Load(p.ConfigPath)
		if err != nil {
			return nil, fmt.Errorf("load config %q: %w", p.ConfigPath, err)
		}
		return cfg, nil
	}
	r, err := resolveConfig()
	if err != nil {
		return nil, err
	}
	return r.cfg, nil
}

// printLsStatus prints minimal per-worktree rows (no compose project),
// including the implicit main worktree.
func printLsStatus(w *tabwriter.Writer, cfg *config.Config) error {
	recs, err := recordsForDisplay(cfg)
	if err != nil {
		return err
	}
	for _, rec := range recs {
		status, portText := rowFor(cfg, rec)
		if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			rec.Slug, rec.Branch, status, portText); err != nil {
			return fmt.Errorf("write output: %w", err)
		}
	}
	return nil
}

func init() {
	lsCmd.Flags().StringVar(&lsProject, "project", "", "project name from registry (see wrk3 project ls; default: local project in cwd)")
	_ = lsCmd.RegisterFlagCompletionFunc("project", completeProjectNames)
	rootCmd.AddCommand(lsCmd)
}
