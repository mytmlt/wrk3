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
	Use:               "status",
	Short:             "WORKTREE/BRANCH/STATUS/PORTS/COMPOSE_PROJECT",
	Args:              cobra.NoArgs,
	ValidArgsFunction: cobra.NoFileCompletions,
	RunE: func(cmd *cobra.Command, args []string) error {
		r, err := resolveConfig()
		if err != nil {
			return err
		}
		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
		if r.cfg.Proxy.Enabled {
			if _, err := fmt.Fprintln(w, "WORKTREE\tBRANCH\tSTATUS\tPORTS\tCOMPOSE_PROJECT\tURL"); err != nil {
				return fmt.Errorf("write output: %w", err)
			}
		} else if _, err := fmt.Fprintln(w, "WORKTREE\tBRANCH\tSTATUS\tPORTS\tCOMPOSE_PROJECT"); err != nil {
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
	for _, cell := range probeStatusCells(cfg, recs) {
		if cfg.Proxy.Enabled {
			if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
				cell.slug, cell.branch, cell.status, cell.ports, cell.project, cfg.ProxyURL(cell.slug)); err != nil {
				return fmt.Errorf("write output: %w", err)
			}
			continue
		}
		if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			cell.slug, cell.branch, cell.status, cell.ports, cell.project); err != nil {
			return fmt.Errorf("write output: %w", err)
		}
	}
	return nil
}

// rowFor derives display cells: "?" + "stale" when the dir is missing,
// otherwise live "running" always wins over stored state (out-of-band up
// clears stale stopped/failed); stored transitional/terminal states
// (setting up, stopping, failed) win over a non-running probe so the table
// stays honest while entries are still executing. A running lifecycle
// gains a health suffix (e.g. "running (healthy)") when health checks or
// compose container health report data; anything else stays bare.
func rowFor(cfg *config.Config, rec ports.WorktreeRecord) (status, portText string) {
	if _, err := os.Stat(rec.AbsPath); err != nil {
		return "stale", "?"
	}
	portText = portsCell(rec.Ports)
	st, err := liveStatus(cfg, rec)
	return withHealthSuffix(cfg, rec, ports.ResolveDisplayStatus(rec.Status, st, err)), portText
}

// liveStatus queries the runner for running/stopped/unknown.
// Probe errors are persisted to the system log (debug) via the logged
// runner; successes stay quiet so status polling does not flood it.
func liveStatus(cfg *config.Config, rec ports.WorktreeRecord) (string, error) {
	return liveStatusWithOrigin(cfg, cfg.StatePath(), "cli", rec)
}

// liveStatusWithOrigin is liveStatus with an explicit system log origin
// (the dashboard passes "dashboard" so its background probes attribute
// correctly).
func liveStatusWithOrigin(cfg *config.Config, stateP, origin string, rec ports.WorktreeRecord) (string, error) {
	raw, err := newRunner(cfg, rec.Slug)
	if err != nil {
		return "", err
	}
	rn := &loggedRunner{inner: raw, stateP: stateP, origin: origin, branch: rec.Branch, slug: rec.Slug, project: rec.ComposeProject}
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
