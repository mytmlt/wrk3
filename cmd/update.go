package cmd

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/mytmlt/wrk3/internal/update"
)

var (
	updateVersion string
	updateCheck   bool
)

var updateCmd = &cobra.Command{
	Use:   "update [--version vX.Y.Z] [--check]",
	Short: "Update wrk3 to the latest release",
	Long: `Download the latest wrk3 release and replace the running binary.

  wrk3 update              install the latest release over the current binary
  wrk3 update --check      print the latest release without installing
  wrk3 update --version v0.2.0   install a pinned version

Downloads the matching prebuilt asset from GitHub releases, verifies its
sha256 checksum, and atomically replaces the current binary. No Go
toolchain required. Set WRK3_NO_UPDATE_CHECK=1 to silence the daily
"new version available" notice (explicit "wrk3 update" always runs).`,
	Args:              cobra.NoArgs,
	ValidArgsFunction: cobra.NoFileCompletions,
	RunE: func(cmd *cobra.Command, args []string) error {
		if updateCheck {
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			latest, err := update.FetchLatest(ctx)
			if err != nil {
				return fmt.Errorf("check latest release: %w", err)
			}
			if update.IsNewer(Version, latest) {
				_, err := fmt.Fprintf(cmd.OutOrStdout(),
					"A new version of wrk3 is available: %s (you have %s). Run \"wrk3 update\" to update.\n",
					update.NormalizeVersion(latest), update.NormalizeVersion(Version))
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "wrk3 is up to date (%s; latest %s)\n",
				update.NormalizeVersion(Version), update.NormalizeVersion(latest))
			return err
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 3*time.Minute)
		defer cancel()
		installed, err := update.UpdateTo(ctx, Version, updateVersion)
		if errors.Is(err, update.ErrAlreadyUpToDate) {
			_, ferr := fmt.Fprintf(cmd.OutOrStdout(), "wrk3 is already up to date (%s)\n",
				update.NormalizeVersion(installed))
			return ferr
		}
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "updated wrk3 %s -> %s\n",
			update.NormalizeVersion(Version), update.NormalizeVersion(installed))
		return err
	},
}

func init() {
	updateCmd.Flags().StringVar(&updateVersion, "version", "", "install a pinned version (e.g. v0.2.0) instead of latest")
	updateCmd.Flags().BoolVar(&updateCheck, "check", false, "only print the latest release, do not install")
	rootCmd.AddCommand(updateCmd)
}
