package cmd

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"github.com/spf13/cobra"

	"github.com/mytmlt/wrk3/internal/syslog"
)

var (
	logTail int
	logJSON bool
)

var logCmd = &cobra.Command{
	Use:   "log",
	Short: "show persistent system log (commands run, state changes, errors)",
	Long: `Print the persistent system log kept next to the state file
(<worktreeBase>/.wrk3-log.jsonl).

While the dashboard log pane stays brief, the system log records
everything that happens: every command run (entry strings via sh -c,
compose up/down, git fetch/pull/worktree add/remove), state transitions,
warnings and errors, with timestamps, cwd, duration and error text.`,
	Args:              cobra.NoArgs,
	ValidArgsFunction: cobra.NoFileCompletions,
	RunE: func(cmd *cobra.Command, args []string) error {
		if logTail <= 0 {
			return fmt.Errorf("--tail must be > 0")
		}
		r, err := resolveConfig()
		if err != nil {
			return err
		}
		entries, err := syslog.ReadLast(syslog.LogPath(r.stateP), logTail)
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		if logJSON {
			for _, e := range entries {
				raw, err := json.Marshal(e)
				if err != nil {
					return fmt.Errorf("encode log entry: %w", err)
				}
				if _, err := fmt.Fprintln(out, string(raw)); err != nil {
					return fmt.Errorf("write output: %w", err)
				}
			}
			return nil
		}
		for _, e := range entries {
			if _, err := fmt.Fprintln(out, formatSyslogEntry(e)); err != nil {
				return fmt.Errorf("write output: %w", err)
			}
		}
		return nil
	},
}

// formatSyslogEntry renders one entry as a single human-readable line:
// "2006-01-02 15:04:05 [op] branch: msg (cmd=… cwd=… dur=…s err=…)".
// Empty fields are omitted so brief entries stay brief. Every field is
// flattened to one line first: command stderr embedded in Err can carry
// newlines (and ANSI escapes), which must never forge extra terminal
// lines in the viewer output.
func formatSyslogEntry(e syslog.Entry) string {
	var b strings.Builder
	b.WriteString(e.Time.Local().Format("2006-01-02 15:04:05"))
	if e.Op != "" {
		b.WriteString(" [" + oneLine(e.Op) + "]")
	}
	if e.Branch != "" {
		b.WriteString(" " + oneLine(e.Branch) + ":")
	}
	if e.Msg != "" {
		b.WriteString(" " + oneLine(e.Msg))
	}
	var details []string
	if e.Cmd != "" {
		details = append(details, "cmd="+oneLine(e.Cmd))
	}
	if e.Cwd != "" {
		details = append(details, "cwd="+oneLine(e.Cwd))
	}
	if e.DurationMs > 0 {
		details = append(details, fmt.Sprintf("dur=%dms", e.DurationMs))
	}
	if e.Err != "" {
		details = append(details, "err="+oneLine(e.Err))
	}
	if len(details) > 0 {
		b.WriteString(" (" + strings.Join(details, " ") + ")")
	}
	return b.String()
}

// oneLine flattens s to a single line: newlines/tabs become spaces and
// other control characters (including ESC, so ANSI escapes cannot forge
// terminal output) are dropped.
func oneLine(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\r', '\t':
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}

func init() {
	logCmd.Flags().IntVarP(&logTail, "tail", "n", 50, "show last N entries")
	logCmd.Flags().BoolVar(&logJSON, "json", false, "print raw JSONL entries")
	rootCmd.AddCommand(logCmd)
}
