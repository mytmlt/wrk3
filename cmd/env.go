package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var envPrint bool

var envCmd = &cobra.Command{
	Use:   "env <branch>",
	Short: "Open a worktree .env in your editor",
	Long: `Open the .env file of a worktree in your editor.

Uses $VISUAL, then $EDITOR (with args supported, e.g. "code --wait"),
then nvim/vim/nano/vi. The .env is ensured first (managed port keys
overwritten to the allocation; user-owned keys left intact) so the
file always exists. With --print the file is written to stdout instead
of opening an editor.`,
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeWorktrees,
	RunE: func(cmd *cobra.Command, args []string) error {
		r, err := resolveConfig()
		if err != nil {
			return err
		}
		recs, err := loadState(r)
		if err != nil {
			return err
		}
		rec, err := findRecordIncludingMain(r, recs, args[0])
		if err != nil {
			return err
		}
		if rec == nil {
			return fmt.Errorf("unknown worktree %q (see status)", args[0])
		}
		if st, err := os.Stat(rec.AbsPath); err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("worktree %q is stale (directory %s missing); run `wrk3 remove --force %s` to clean state, or re-add", rec.Branch, rec.AbsPath, rec.Branch)
			}
			return fmt.Errorf("stat worktree %q path %q: %w", rec.Branch, rec.AbsPath, err)
		} else if !st.IsDir() {
			return fmt.Errorf("worktree %q path %q is not a directory", rec.Branch, rec.AbsPath)
		}
		if err := ensureWorktreeEnv(r, *rec); err != nil {
			return err
		}
		path := envFilePath(rec.AbsPath)
		if envPrint {
			raw, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("read .env %q: %w", path, err)
			}
			if _, err := fmt.Fprint(cmd.OutOrStdout(), string(raw)); err != nil {
				return fmt.Errorf("write output: %w", err)
			}
			return nil
		}
		if err := openEnvInEditor(path); err != nil {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), path)
			return err
		}
		return nil
	},
}

func init() {
	envCmd.Flags().BoolVar(&envPrint, "print", false, "print the .env to stdout instead of opening an editor")
	rootCmd.AddCommand(envCmd)
}
