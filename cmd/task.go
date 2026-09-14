package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mytmlt/wrk3/internal/task"
)

var (
	taskFrom string
	taskTo   string
	taskDir  string
)

var taskCmd = &cobra.Command{
	Use:   "task [file...]",
	Short: "Analyze a stack into a portable task definition",
	Long: `Analyze docker compose (or a host plan) into wrk3's internal task
definition and project it onto compose, swarm, Portainer, or the host.

Source files are never modified. Default output is the canonical internal
YAML. Pass --to to render another environment.

  wrk3 task                     # discover compose in cwd, print internal YAML
  wrk3 task --to swarm          # swarm stack file on stdout
  wrk3 task --to portainer
  wrk3 task --to host           # docker run / process plan
  wrk3 task --from host plan.yaml --to compose
`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := taskDir
		if strings.TrimSpace(dir) == "" {
			dir = "."
		}
		t, notes, err := task.Analyze(dir, task.AnalyzeOptions{
			From:  taskFrom,
			Files: args,
		})
		if err != nil {
			return err
		}
		env, err := task.ParseEnvironment(taskTo)
		if err != nil {
			return err
		}
		body, notes, err := task.Render(*t, env, notes)
		for _, n := range notes {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "%s: %s: %s\n", n.Level, noteSvc(n), n.Message)
		}
		if err != nil {
			return err
		}
		if _, err := cmd.OutOrStdout().Write(body); err != nil {
			return fmt.Errorf("write task: %w", err)
		}
		return nil
	},
}

func noteSvc(n task.Note) string {
	if n.Service == "" {
		return n.Code
	}
	if n.Code == "" {
		return n.Service
	}
	return n.Service + "/" + n.Code
}

func init() {
	taskCmd.Flags().StringVar(&taskFrom, "from", "", "source kind: compose or host (default: discover compose)")
	taskCmd.Flags().StringVar(&taskTo, "to", "internal", "target environment: internal, compose, swarm, portainer, host")
	taskCmd.Flags().StringVar(&taskDir, "dir", ".", "directory to analyze")
	rootCmd.AddCommand(taskCmd)
}
