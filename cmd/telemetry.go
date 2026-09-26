package cmd

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/mytmlt/wrk3/internal/telemetry"
)

var telemetryCmd = &cobra.Command{
	Use:     "telemetry",
	Aliases: []string{"tel"},
	Short:   "Manage anonymous error reporting",
	Long: `Manage anonymous opt-in error reporting.

Error reports help maintainers see crash types without collecting repos,
branches, secrets, or personal data. Reporting is off by default.

Subcommands:
  enable   Turn on reporting and print what is and is not sent
  disable  Turn off reporting
  status   Show current reporting configuration

Environment:
  WRK3_NO_TELEMETRY=1  Hard kill-switch that wins over the preferences file
  WRK3_SENTRY_DSN      Override the built-in Sentry project DSN (forks/self-builds)`,
	ValidArgsFunction: cobra.NoFileCompletions,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var telemetryEnableCmd = &cobra.Command{
	Use:               "enable",
	Short:             "Enable anonymous error reporting",
	ValidArgsFunction: cobra.NoFileCompletions,
	RunE: func(cmd *cobra.Command, args []string) error {
		if telemetry.CheckDisabled() {
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "error: reporting disabled by WRK3_NO_TELEMETRY")
			return nil
		}
		if err := telemetry.SavePrefs(true, true); err != nil {
			return fmt.Errorf("save preferences: %w", err)
		}
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Anonymous error reporting enabled.")
		_, _ = fmt.Fprintln(cmd.OutOrStdout())
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "The following data is sent when an error occurs:")
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "  - Error type (Go type name)")
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "  - Scrubbed error message (no paths, branches, IPs, emails, or secrets)")
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "  - Scrubbed stacktrace (module, function, line; no locals or source)")
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "  - Command name (e.g. up, never arguments or flags)")
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "  - wrk3 version, OS, and architecture")
		_, _ = fmt.Fprintln(cmd.OutOrStdout())
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Expected, user-actionable errors (e.g. refusing to remove a dirty")
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "worktree without --force) are excluded and never reported.")
		_, _ = fmt.Fprintln(cmd.OutOrStdout())
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "The following is NEVER sent:")
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "  - Repository paths, branch names, or slugs")
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "  - Port numbers, .env content, or compose files")
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "  - Hostname, username, home directory, IP addresses, or emails")
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "  - Command arguments, flags, or configuration values")
		_, _ = fmt.Fprintln(cmd.OutOrStdout())
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Data is sent to a Sentry project owned by the wrk3 maintainers.")
		return nil
	},
}

var telemetryDisableCmd = &cobra.Command{
	Use:               "disable",
	Short:             "Disable anonymous error reporting",
	ValidArgsFunction: cobra.NoFileCompletions,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := telemetry.SavePrefs(false, true); err != nil {
			return fmt.Errorf("save preferences: %w", err)
		}
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Anonymous error reporting disabled.")
		return nil
	},
}

var telemetryStatusCmd = &cobra.Command{
	Use:               "status",
	Short:             "Show anonymous error reporting status",
	ValidArgsFunction: cobra.NoFileCompletions,
	Args:              cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		enabled, prompted := telemetry.LoadPrefs()
		killSwitch := telemetry.CheckDisabled()
		dsnOk := telemetry.DSNConfigured()
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Enabled:       %v\n", enabled)
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Prompted:      %v\n", prompted)
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Kill-switch:   %v\n", killSwitch)
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "DSN configured: %v\n", dsnOk)
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Version:       %s\n", Version)
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Platform:      %s/%s\n", runtime.GOOS, runtime.GOARCH)
		if killSwitch {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Reporting is disabled by WRK3_NO_TELEMETRY environment variable.")
		} else if !dsnOk {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No Sentry DSN configured — events will not be sent even if enabled.")
		} else if !enabled {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Reporting is off. Use 'wrk3 telemetry enable' to turn it on.")
		}
		return nil
	},
}

func init() {
	telemetryCmd.AddCommand(telemetryEnableCmd)
	telemetryCmd.AddCommand(telemetryDisableCmd)
	telemetryCmd.AddCommand(telemetryStatusCmd)
	rootCmd.AddCommand(telemetryCmd)
}
