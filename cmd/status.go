package cmd

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/mytmlt/wrk3/internal/config"
	"github.com/mytmlt/wrk3/internal/ports"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "WORKTREE/BRANCH/STATUS/PORTS/COMPOSE_PROJECT",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		r, err := resolveConfig()
		if err != nil {
			return err
		}
		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
		if _, err := fmt.Fprintln(w, "WORKTREE\tBRANCH\tSTATUS\tPORTS\tCOMPOSE_PROJECT"); err != nil {
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
		status, portText := rowFor(cfg, rec)
		if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			rec.Slug, rec.Branch, status, portText, rec.ComposeProject); err != nil {
			return fmt.Errorf("write output: %w", err)
		}
	}
	return nil
}

// rowFor derives display cells: "?" + "stale" when the dir is missing,
// otherwise the stored transitional/terminal status (setting up, stopping,
// failed) wins over the live runner probe so the table stays honest while
// entries are still executing; all other cases use live
// runner status with all allocated ports from the state file.
func rowFor(cfg *config.Config, rec ports.WorktreeRecord) (status, portText string) {
	if _, err := os.Stat(rec.AbsPath); err != nil {
		return "stale", "?"
	}
	portText = portsCell(rec.Ports)
	if ports.StoredStatusOverridesLive(rec.Status) {
		return rec.Status, portText
	}
	st, err := liveStatus(cfg, rec)
	if err != nil {
		if rec.Status != "" {
			return rec.Status, portText
		}
		return "unknown", portText
	}
	return st, portText
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

// portsCell renders every allocated port as a sorted name=value list with
// app first (e.g. "app=8000,db=5432,web=3000"). Returns "?" when empty.
func portsCell(m map[string]int) string {
	if len(m) == 0 {
		return "?"
	}
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	// Pin app first for scannability; rest stays alphabetical.
	ordered := make([]string, 0, len(names))
	if _, ok := m[ports.PortApp]; ok {
		ordered = append(ordered, ports.PortApp)
	}
	for _, name := range names {
		if name == ports.PortApp {
			continue
		}
		ordered = append(ordered, name)
	}
	parts := make([]string, 0, len(ordered))
	for _, name := range ordered {
		parts = append(parts, fmt.Sprintf("%s=%d", name, m[name]))
	}
	return strings.Join(parts, ",")
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
