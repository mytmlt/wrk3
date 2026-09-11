package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mytmlt/wrk3/internal/source"
)

var (
	fetchMine   bool
	fetchAuthor []string
)

var fetchCmd = &cobra.Command{
	Use:   "fetch [--mine] [--author <name-or-email>...]",
	Short: "git fetch origin --prune, list origin/* refs",
	Long: `git fetch origin --prune, then list origin/* refs (one per line).

  wrk3 fetch                          list every remote branch
  wrk3 fetch --mine                   only branches whose tip commit author
                                      matches git config user.name/user.email
  wrk3 fetch --author alice           substring match (case-insensitive)
                                      against author name and email;
                                      repeatable, matches any
  wrk3 fetch --mine --author alice    intersection of both filters`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		r, err := resolveConfig()
		if err != nil {
			return err
		}
		if err := r.src.Fetch(r.cfg.RepoPath()); err != nil {
			return fmt.Errorf("fetch: %w", err)
		}
		names, err := filterRefs(r.src, r.cfg.RepoPath(), fetchMine, fetchAuthor)
		if err != nil {
			return err
		}
		for _, ref := range names {
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), ref); err != nil {
				return fmt.Errorf("write output: %w", err)
			}
		}
		return nil
	},
}

// filterRefs fetches branch names, optionally narrowed to the local user's
// branches (--mine) and/or an author substring filter (--author).
func filterRefs(src source.Source, repoPath string, mine bool, authors []string) ([]string, error) {
	patterns := normalizePatterns(authors)
	if !mine && len(patterns) == 0 {
		refs, err := src.Refs(repoPath)
		if err != nil {
			return nil, fmt.Errorf("list refs: %w", err)
		}
		return refs, nil
	}
	detailed, err := src.RefsDetailed(repoPath)
	if err != nil {
		return nil, fmt.Errorf("list refs: %w", err)
	}
	var meName, meEmail string
	if mine {
		meName, meEmail, err = src.Identity(repoPath)
		if err != nil {
			return nil, fmt.Errorf("read git identity: %w", err)
		}
		if strings.TrimSpace(meName) == "" && strings.TrimSpace(meEmail) == "" {
			return nil, fmt.Errorf("no git identity configured (set git config user.name/user.email or use --author)")
		}
	}
	var out []string
	for _, ref := range detailed {
		if mine && !matchesIdentity(ref, meName, meEmail) {
			continue
		}
		if len(patterns) > 0 && !matchesAuthor(ref, patterns) {
			continue
		}
		out = append(out, ref.Name)
	}
	return out, nil
}

// normalizePatterns trims, drops empties, and splits comma-separated values
// so --author "alice,bob" works like --author alice --author bob.
func normalizePatterns(authors []string) []string {
	var out []string
	for _, a := range authors {
		for _, p := range strings.Split(a, ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

// matchesIdentity reports whether ref's tip author equals the local git
// identity (name or email, case-insensitive exact match).
func matchesIdentity(ref source.BranchRef, name, email string) bool {
	if name != "" && strings.EqualFold(strings.TrimSpace(ref.AuthorName), strings.TrimSpace(name)) {
		return true
	}
	if email != "" && strings.EqualFold(strings.TrimSpace(ref.AuthorEmail), strings.TrimSpace(email)) {
		return true
	}
	return false
}

// matchesAuthor reports whether any pattern is a case-insensitive substring
// of the ref's author name, email, or "name <email>" combination.
func matchesAuthor(ref source.BranchRef, patterns []string) bool {
	haystacks := []string{
		strings.ToLower(ref.AuthorName),
		strings.ToLower(ref.AuthorEmail),
		strings.ToLower(strings.TrimSpace(ref.AuthorName + " <" + ref.AuthorEmail + ">")),
	}
	for _, p := range patterns {
		needle := strings.ToLower(p)
		for _, h := range haystacks {
			if h != "" && strings.Contains(h, needle) {
				return true
			}
		}
	}
	return false
}

func init() {
	fetchCmd.Flags().BoolVar(&fetchMine, "mine", false, "only branches whose tip commit author matches git config user.name/user.email")
	fetchCmd.Flags().StringSliceVar(&fetchAuthor, "author", nil, "only branches whose tip author matches <name-or-email> (substring, case-insensitive; repeatable)")
	rootCmd.AddCommand(fetchCmd)
}
