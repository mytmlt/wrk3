// Package cmd implements the thin wrk3 cobra CLI.
//
// Phase 0: stub subcommands only — each prints "not implemented yet".
// cmd depends on internal/* interfaces only, never on concrete
// git/docker implementations (those land in Phases 2/4).
package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/mytmlt/wrk3/internal/config"
	"github.com/mytmlt/wrk3/internal/ports"
	"github.com/mytmlt/wrk3/internal/project"
	"github.com/mytmlt/wrk3/internal/runner"
	"github.com/mytmlt/wrk3/internal/source"
)

var (
	projectFlag string
	configFlag  string
)

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

var _ = project.Project{}
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
}

// ResolveProject resolves which project to operate on.
// Precedence: --config one-shot path > --project flag >
// $WRK3_PROJECT env > current project > cwd scan for wrk3.yaml.
func ResolveProject() (*project.Project, error) {
	if configFlag != "" {
		abs, err := filepath.Abs(configFlag)
		if err != nil {
			return nil, fmt.Errorf("resolve --config %q: %w", configFlag, err)
		}
		return &project.Project{Name: "(config-flag)", ConfigPath: filepath.Clean(abs)}, nil
	}
	store, err := newProjectStore()
	if err != nil {
		return nil, fmt.Errorf("open project registry: %w", err)
	}
	p, err := store.Resolve(projectFlag)
	if err != nil {
		return nil, err
	}
	return p, nil
}

func init() {
	rootCmd.Version = Version
	// NOTE: cobra's version template only sees .Version, so bake
	// commit/date into the template string at init time (ldflags
	// values are already applied before package inits run).
	rootCmd.SetVersionTemplate(fmt.Sprintf("wrk3 %s (commit %s built %s)\n", Version, Commit, Date))
	rootCmd.PersistentFlags().StringVar(&projectFlag, "project", "", "project name to operate on (overrides $WRK3_PROJECT and current project)")
	rootCmd.PersistentFlags().StringVar(&configFlag, "config", "", "one-shot config path override")

	rootCmd.AddCommand(projectCmd)
	rootCmd.AddCommand(versionCmd)
}
