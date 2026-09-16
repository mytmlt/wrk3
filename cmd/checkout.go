package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// directiveCDFileEnv is set by the shell wrapper installed via
// `wrk3 shell-init` (eval'd in ~/.bashrc and friends). A child process
// can never cd its parent shell, so checkout writes the target worktree
// path to this file (raw path, never parsed as shell) and the wrapper
// cds the calling shell after wrk3 exits — the worktrunk-style split
// directive pattern. Without the wrapper, checkout degrades to printing
// the path, so `cd "$(wrk3 checkout <branch>)"` always works.
const directiveCDFileEnv = "WRK3_DIRECTIVE_CD_FILE"

var checkoutPrint bool

var checkoutCmd = &cobra.Command{
	Use:     "checkout <branch>",
	Aliases: []string{"co", "switch"},
	Short:   "cd to a worktree (prints its path without shell integration)",
	Long: `Print the absolute path of a worktree, accepting branch names or slugs
interchangeably (including the implicit main checkout).

With shell integration installed, checkout cds the calling shell instead
of printing — worktrees may live in nested or otherwise awkward paths,
so you never type the path yourself:

  eval "$(wrk3 shell-init bash)"   # once, in ~/.bashrc (zsh/fish/powershell too)
  wrk3 checkout feature-a          # cd to the worktree

Without integration the path goes to stdout, so this always works:

  cd "$(wrk3 checkout feature-a)"`,
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
		// --print is the script-safe form: pure stdout, never cds,
		// even when the wrapper is active.
		if cdFile := os.Getenv(directiveCDFileEnv); cdFile != "" && !checkoutPrint {
			if err := os.WriteFile(cdFile, []byte(rec.AbsPath), 0o644); err != nil {
				return fmt.Errorf("write directive file %q: %w", cdFile, err)
			}
			return nil
		}
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), rec.AbsPath); err != nil {
			return fmt.Errorf("write output: %w", err)
		}
		if os.Getenv(directiveCDFileEnv) == "" {
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(),
				"hint: shell integration not active — `cd` to the path above, or `eval \"$(wrk3 shell-init bash)\"` once in your rc file so `wrk3 checkout` cds directly")
		}
		return nil
	},
}

func init() {
	checkoutCmd.Flags().BoolVar(&checkoutPrint, "print", false, "print the path to stdout even when shell integration is active (script-safe, never cds)")
	rootCmd.AddCommand(checkoutCmd)
}
