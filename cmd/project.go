package cmd

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/mytmlt/wrk3/internal/project"
)

// newProjectStore builds the default registry store.
// Variable (not func) so tests can stub it without touching disk.
var newProjectStore = func() (project.Store, error) {
	return project.NewFileStore()
}

// projectCmd groups the project registry subcommands.
var projectCmd = &cobra.Command{
	Use:   "project",
	Short: "Manage project registry (add|list|use|remove|show)",
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var projectAddCmd = &cobra.Command{
	Use:   "add <name> --config <path>",
	Short: "Register a project name -> wrk3.yaml path",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		if configFlag == "" {
			return fmt.Errorf("project add %q: --config <path> is required", name)
		}
		store, err := newProjectStore()
		if err != nil {
			return fmt.Errorf("open project registry: %w", err)
		}
		if err := store.Add(name, configFlag); err != nil {
			return fmt.Errorf("project add %q: %w", name, err)
		}
		p, err := store.Get(name)
		if err != nil {
			return fmt.Errorf("project add %q: %w", name, err)
		}
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "added project %q -> %s\n", p.Name, p.ConfigPath); err != nil {
			return fmt.Errorf("write output: %w", err)
		}
		return nil
	},
}

var projectListCmd = &cobra.Command{
	Use:   "list",
	Short: "List registered projects",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := newProjectStore()
		if err != nil {
			return fmt.Errorf("open project registry: %w", err)
		}
		projects, err := store.List()
		if err != nil {
			return fmt.Errorf("list projects: %w", err)
		}
		current, _ := store.CurrentName()
		sort.Slice(projects, func(i, j int) bool { return projects[i].Name < projects[j].Name })
		for _, p := range projects {
			marker := " "
			if p.Name == current {
				marker = "*"
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s %s\t%s\n", marker, p.Name, p.ConfigPath); err != nil {
				return fmt.Errorf("write output: %w", err)
			}
		}
		return nil
	},
}

var projectUseCmd = &cobra.Command{
	Use:   "use <name>",
	Short: "Set the current project",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		store, err := newProjectStore()
		if err != nil {
			return fmt.Errorf("open project registry: %w", err)
		}
		if err := store.SetCurrent(name); err != nil {
			return fmt.Errorf("project use %q: %w", name, err)
		}
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "using project %q\n", name); err != nil {
			return fmt.Errorf("write output: %w", err)
		}
		return nil
	},
}

var projectRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove a project from the registry",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		store, err := newProjectStore()
		if err != nil {
			return fmt.Errorf("open project registry: %w", err)
		}
		if err := store.Remove(name); err != nil {
			return fmt.Errorf("project remove %q: %w", name, err)
		}
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "removed project %q\n", name); err != nil {
			return fmt.Errorf("write output: %w", err)
		}
		return nil
	},
}

var projectShowCmd = &cobra.Command{
	Use:   "show [name]",
	Short: "Show current project (or named project)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := newProjectStore()
		if err != nil {
			return fmt.Errorf("open project registry: %w", err)
		}
		var p *project.Project
		switch {
		case len(args) == 1:
			p, err = store.Get(args[0])
			if err != nil {
				return fmt.Errorf("project show %q: %w", args[0], err)
			}
		case projectFlag != "":
			p, err = store.Get(projectFlag)
			if err != nil {
				return fmt.Errorf("project show %q: %w", projectFlag, err)
			}
		default:
			p, err = store.Current()
			if err != nil {
				return fmt.Errorf("project show: %w", err)
			}
		}
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "name: %s\nconfig: %s\nadded: %s\n",
			p.Name, p.ConfigPath, p.AddedAt.Format("2006-01-02T15:04:05Z07:00")); err != nil {
			return fmt.Errorf("write output: %w", err)
		}
		return nil
	},
}

func init() {
	projectCmd.AddCommand(projectAddCmd, projectListCmd, projectUseCmd, projectRemoveCmd, projectShowCmd)
}
