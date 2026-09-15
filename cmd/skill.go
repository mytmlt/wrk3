package cmd

import (
	_ "embed"
	"fmt"

	"github.com/spf13/cobra"
)

// skillGuide is the bundled agent setup guide printed by `wrk3 skill`.
// It is embedded so the command works offline with no config file.
//
//go:embed skill.md
var skillGuide string

var skillCmd = &cobra.Command{
	Use:   "skill",
	Short: "Print the agent setup guide for authoring wrk3.yaml",
	Long: `Print the bundled agent guide for setting up wrk3.yaml.

The guide covers the local-setup discovery flow (learn the developer
onboarding first, ask when unsure), the compatibility triage, the
wrk3.yaml template and field rules, and the validation step, so an
agent can configure wrk3 for any app. It needs no config file and
prints markdown to stdout.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if _, err := fmt.Fprint(cmd.OutOrStdout(), skillGuide); err != nil {
			return fmt.Errorf("write skill guide: %w", err)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(skillCmd)
}
