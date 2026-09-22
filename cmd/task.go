package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mytmlt/wrk3/internal/task"
)

var (
	taskFormat string
	taskSwarm  bool
)

var taskCmd = &cobra.Command{
	Use:   "task",
	Short: "Build an internal task definition from compose without changing source",
	Long: `Analyze the project's docker compose files into wrk3's internal task
definition, then render it for another environment.

Source compose files are never modified; output goes to stdout.

Formats:
  yaml        internal definition (default)
  json        same, as JSON
  compose     normalized compose YAML
  swarm       docker stack deploy YAML (Portainer swarm stacks)
  portainer   Portainer stack JSON (Name + StackFileContent)
  host        host process plan (compose → machine, and the reverse IR)

  wrk3 task
  wrk3 task --format swarm
  wrk3 task --format portainer --swarm
  wrk3 task --format host`,
	Args:              cobra.NoArgs,
	ValidArgsFunction: cobra.NoFileCompletions,
	RunE: func(cmd *cobra.Command, args []string) error {
		r, err := resolveConfig()
		if err != nil {
			return err
		}
		format, err := task.ParseFormat(taskFormat)
		if err != nil {
			return err
		}
		files := r.cfg.ComposeFiles()
		def, err := task.LoadCompose(r.cfg.RepoPath(), files)
		if err != nil {
			return fmt.Errorf("analyze compose: %w", err)
		}
		raw, err := def.Encode(format, taskSwarm)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), strings.TrimRight(string(raw), "\n"))
		return err
	},
}

func init() {
	taskCmd.Flags().StringVar(&taskFormat, "format", string(task.FormatYAML), "yaml|json|compose|swarm|portainer|host")
	taskCmd.Flags().BoolVar(&taskSwarm, "swarm", false, "Portainer payload uses a swarm stack file")
	_ = taskCmd.RegisterFlagCompletionFunc("format", cobra.FixedCompletions(task.Formats(), cobra.ShellCompDirectiveNoFileComp))
	rootCmd.AddCommand(taskCmd)
}
