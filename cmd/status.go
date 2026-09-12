package cmd

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/mytmlt/wrk3/internal/config"
	"github.com/mytmlt/wrk3/internal/ports"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "WORKTREE/BRANCH/STATUS/APP/COMPOSE_PROJECT",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		r, err := resolveConfig()
		if err != nil {
			return err
		}
		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
		if _, err := fmt.Fprintln(w, "WORKTREE\tBRANCH\tSTATUS\tAPP\tCOMPOSE_PROJECT"); err != nil {
			return fmt.Errorf("write output: %w", err)
		}
		if err := printResolvedStatus(w, r.cfg); err != nil {
			return err
		}
		return w.Flush()
	},
}

// printResolvedStatus prints state rows plus the implicit main worktree.
func printResolvedStatus(w *tabwriter.Writer, cfg *config.Config) error {
	recs, err := recordsForDisplay(cfg)
	if err != nil {
		return err
	}
	if len(recs) == 0 {
		return nil
	}
	for _, rec := range recs {
		status, app := rowFor(cfg, rec)
		if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			rec.Slug, rec.Branch, status, app, rec.ComposeProject); err != nil {
			return fmt.Errorf("write output: %w", err)
		}
	}
	return nil
}

// rowFor derives display cells: "?" + "stale" when the dir is missing,
// otherwise live runner status with the app port from the state file.
func rowFor(cfg *config.Config, rec ports.WorktreeRecord) (status, app string) {
	if _, err := os.Stat(rec.AbsPath); err != nil {
		return "stale", "?"
	}
	app = portCell(rec.Ports, ports.PortApp)
	st, err := liveStatus(cfg, rec)
	if err != nil {
		if rec.Status != "" {
			return rec.Status, app
		}
		return "unknown", app
	}
	return st, app
}

// liveStatus queries the runner for running/stopped/unknown.
func liveStatus(cfg *config.Config, rec ports.WorktreeRecord) (string, error) {
	rn, err := newRunner(cfg, rec.Slug)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	st, err := rn.Status(ctx, rec.AbsPath)
	if err != nil {
		return string(st.State), err
	}
	return string(st.State), nil
}

func portCell(m map[string]int, name string) string {
	if m == nil {
		return "?"
	}
	v, ok := m[name]
	if !ok {
		return "?"
	}
	return fmt.Sprintf("%d", v)
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
