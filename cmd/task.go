package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mytmlt/wrk3/internal/task"
)

// taskInputFormats are the source formats Parse can ingest.
var taskInputFormats = []string{"compose", "machine"}

var (
	taskRenderTo      string
	taskRenderFrom    string
	taskRenderCompose []string
	taskRenderName    string
)

var taskCmd = &cobra.Command{
	Use:   "task",
	Short: "Analyze the task definition and render it for other environments",
	Long: `Analyze the project's docker compose stack into an internal task
definition and render it for another environment (compose, swarm,
portainer or machine) without editing the source.`,
}

var taskRenderCmd = &cobra.Command{
	Use:               "render",
	Short:             "Render the task definition for a target environment",
	Args:              cobra.NoArgs,
	ValidArgsFunction: cobra.NoFileCompletions,
	RunE:              runTaskRender,
}

func init() {
	taskRenderCmd.Flags().StringVar(&taskRenderFrom, "from", "compose",
		"input format: "+strings.Join(taskInputFormats, ", "))
	taskRenderCmd.Flags().StringVar(&taskRenderTo, "to", "compose",
		"target environment: "+strings.Join(task.AvailableTargets(), ", "))
	taskRenderCmd.Flags().StringArrayVar(&taskRenderCompose, "compose", nil,
		"input file to analyze (repeatable; default: runner composeFiles from config)")
	taskRenderCmd.Flags().StringVar(&taskRenderName, "name", "",
		"override the task/stack name")
	_ = taskRenderCmd.MarkFlagFilename("compose", "yaml", "yml")
	taskCmd.AddCommand(taskRenderCmd)
	rootCmd.AddCommand(taskCmd)
}

func runTaskRender(cmd *cobra.Command, _ []string) error {
	def, err := loadTaskDefinition()
	if err != nil {
		return err
	}
	if strings.TrimSpace(taskRenderName) != "" {
		def.Name = strings.TrimSpace(taskRenderName)
	}
	target, err := task.ResolveTarget(taskRenderTo)
	if err != nil {
		return err
	}
	out, err := target.Render(def)
	if err != nil {
		return err
	}
	_, err = cmd.OutOrStdout().Write(out)
	return err
}

// loadTaskDefinition loads and merges the compose files, either from the
// --compose flags (resolved against cwd) or the selected runner backend
// in wrk3.yaml (resolved against the repo root).
func loadTaskDefinition() (*task.Definition, error) {
	files := append([]string(nil), taskRenderCompose...)
	if len(files) == 0 && taskRenderFrom == "machine" {
		return nil, fmt.Errorf("--from machine requires --compose <script>")
	}
	if len(files) == 0 {
		r, err := resolveConfig()
		if err != nil {
			return nil, err
		}
		for _, f := range r.cfg.ComposeFiles() {
			if !filepath.IsAbs(f) {
				f = filepath.Join(r.cfg.RepoPath(), f)
			}
			files = append(files, f)
		}
		if len(files) == 0 {
			return nil, fmt.Errorf("runner.type %q has no compose files; pass --compose <file>", r.cfg.Runner.Type)
		}
	}
	var def *task.Definition
	for i, f := range files {
		cur, err := loadTaskFile(f)
		if err != nil {
			return nil, err
		}
		if i == 0 {
			def = cur
			continue
		}
		def.Merge(cur)
	}
	return def, nil
}

// loadTaskFile parses one input file according to --from.
func loadTaskFile(path string) (*task.Definition, error) {
	switch taskRenderFrom {
	case "", "compose":
		return task.LoadCompose(path)
	case "machine":
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read machine script %q: %w", path, err)
		}
		def, err := task.ParseRunCommands(raw)
		if err != nil {
			return nil, fmt.Errorf("parse machine script %q: %w", path, err)
		}
		return def, nil
	default:
		return nil, fmt.Errorf("unknown task input format %q (available: %v)", taskRenderFrom, taskInputFormats)
	}
}
