// Package cmd implements the thin wrk3 cobra CLI.
//
// cmd depends on internal/* interfaces only, never on concrete
// git/docker implementations.
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/mytmlt/wrk3/internal/config"
	"github.com/mytmlt/wrk3/internal/ports"
	"github.com/mytmlt/wrk3/internal/runner"
	"github.com/mytmlt/wrk3/internal/source"
	"github.com/mytmlt/wrk3/internal/update"
)

var fileFlag string

// Build metadata, injected via -ldflags at release time:
//
//	go build -ldflags "-X github.com/mytmlt/wrk3/cmd.Version=v0.1.0 \
//	  -X github.com/mytmlt/wrk3/cmd.Commit=abc123 \
//	  -X github.com/mytmlt/wrk3/cmd.Date=2026-09-11"
//
// Defaults keep local/dev builds working without ldflags.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

var _ = config.Config{}
var _ = ports.Allocator{}
var _ source.Source
var _ runner.Runner

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

var rootCmd = &cobra.Command{
	Use:   "wrk3",
	Short: "Run multiple branches of the same repo in parallel",
	Long: `wrk3 runs multiple versions (branches) of the same repo in parallel
as git worktrees, each isolated with its own ports and container project.`,
	Version: Version,
	// Daily update notice: best-effort, never blocks the command.
	PersistentPreRun: func(cmd *cobra.Command, _ []string) {
		maybePrintUpdateNotice(cmd)
	},
}

// ResolveConfigPath resolves the wrk3.yaml/wrk3.yml to operate on.
// Precedence: -f/--file flag > upward scan from cwd.
func ResolveConfigPath() (string, error) {
	found, err := config.DiscoverFile(fileFlag, "")
	if err != nil {
		return "", err
	}
	if found == "" {
		if fileFlag != "" {
			// DiscoverFile with explicit never returns empty; defensive.
			return "", fmt.Errorf("config file %q not found", fileFlag)
		}
		return "", fmt.Errorf("no wrk3.yaml found (walked up from cwd); pass -f <path>")
	}
	return found, nil
}

func init() {
	rootCmd.Version = Version
	// NOTE: cobra's version template only sees .Version, so bake
	// commit/date into the template string at init time (ldflags
	// values are already applied before package inits run).
	rootCmd.SetVersionTemplate(fmt.Sprintf("wrk3 %s (commit %s built %s)\n", Version, Commit, Date))
	rootCmd.PersistentFlags().StringVarP(&fileFlag, "file", "f", "", "config file path (default: find wrk3.yaml/wrk3.yml upwards from cwd)")
	_ = rootCmd.MarkPersistentFlagFilename("file", "yaml", "yml")

	rootCmd.AddCommand(versionCmd)
}

// maybePrintUpdateNotice prints the "new version available" hint to stderr.
// Best-effort and silent on failure: dev builds, help/version output,
// and the update/version/completion commands themselves never nag.
func maybePrintUpdateNotice(cmd *cobra.Command) {
	switch cmd.Name() {
	case "update", "version", "completion":
		return
	}
	for _, a := range os.Args[1:] {
		switch a {
		case "-h", "--help", "-V", "--version":
			return
		}
	}
	if msg := update.Notice(Version); msg != "" {
		_, _ = fmt.Fprintln(cmd.ErrOrStderr(), msg)
	}
}
