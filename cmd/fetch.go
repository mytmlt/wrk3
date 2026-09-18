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
	fetchRemote string
	fetchMyPRS  bool
)

var fetchCmd = &cobra.Command{
	Use:   "fetch [--remote <name>] [--mine] [--author <name-or-email>...] [--myprs]",
	Short: "git fetch <remote> --prune, list <remote>/* refs",
	Long: `git fetch <remote> --prune, then list <remote>/* refs (one per line).

  wrk3 fetch                          list every remote branch
  wrk3 fetch --remote upstream        list branches on upstream (default: source.git.remote, else origin)
  wrk3 fetch --mine                   only your branches: tip author/committer
                                      matches git config user.name/user.email,
                                      else any of the last 100 branch-exclusive
                                      commits does (bot/cursor tips you pushed)
  wrk3 fetch --author alice           substring match (case-insensitive)
                                      against author/committer name and email,
                                      tip or branch-exclusive history;
                                      repeatable, matches any
  wrk3 fetch --myprs                  only branches with an open PR involving
                                      you (GitHub remotes only, via the gh CLI;
                                      like pulls?q=is:pr+state:open+involves:@me)
   wrk3 fetch --mine --author alice    intersection of both filters
   wrk3 fetch --myprs --author alice   PR branches also matching the author filter`,
	Args:              cobra.NoArgs,
	ValidArgsFunction: cobra.NoFileCompletions,
	RunE: func(cmd *cobra.Command, args []string) error {
		r, err := resolveConfig()
		if err != nil {
			return err
		}
		remote := resolveRemote(r, fetchRemote)
		if err := r.src.Fetch(r.cfg.RepoPath(), remote); err != nil {
			return fmt.Errorf("fetch: %w", err)
		}
		names, err := filterRefs(r.src, r.cfg.RepoPath(), remote, fetchMine, fetchAuthor)
		if err != nil {
			return err
		}
		if fetchMyPRS {
			prs, err := myPRBranches(r.cfg.RepoPath(), remote)
			if err != nil {
				return err
			}
			names = intersectMyPRS(names, prs)
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
//
// Tip-only matching misses bot/cursor branches you pushed: the tip author
// and committer are both the bot (e.g. Cursor Agent) even though GitHub's
// "Yours" lists the branch under you. So when the tip doesn't match, we
// scan up to source.MineHistoryLimit branch-exclusive commits
// (<remote>/<branch> --not <remote>/<base>) for your identity/pattern.
// The base comes from Source.DefaultBranch; when it is unknown we stay
// tip-only (scanning without a base would match mainline commits and flag
// every branch).
func filterRefs(src source.Source, repoPath, remote string, mine bool, authors []string) ([]string, error) {
	patterns := normalizePatterns(authors)
	if !mine && len(patterns) == 0 {
		refs, err := src.Refs(repoPath, remote)
		if err != nil {
			return nil, fmt.Errorf("list refs: %w", err)
		}
		return refs, nil
	}
	detailed, err := src.RefsDetailed(repoPath, remote)
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
	// Resolve once for the whole filter pass; unknown base disables the
	// history fallback (tip-only) rather than failing the command.
	base, _ := src.DefaultBranch(repoPath, remote)
	var out []string
	for _, ref := range detailed {
		if matchesWithHistory(src, repoPath, remote, base, ref, mine, meName, meEmail, patterns) {
			out = append(out, ref.Name)
		}
	}
	return out, nil
}

// matchesWithHistory reports whether ref satisfies the mine/author filters.
// The tip commit is checked first (fast path, no extra git calls); only
// when the tip misses and a base is known do we scan branch-exclusive
// history. Mine and author filters intersect: each must match on tip or
// history (possibly on different commits).
func matchesWithHistory(src source.Source, repoPath, remote, base string, ref source.BranchRef, mine bool, meName, meEmail string, patterns []string) bool {
	mineOK := !mine || matchesIdentity(ref, meName, meEmail)
	authorOK := len(patterns) == 0 || matchesAuthor(ref, patterns)
	if mineOK && authorOK {
		return true
	}
	if strings.TrimSpace(base) == "" {
		return false
	}
	history, err := src.BranchHistory(repoPath, remote, ref.Name, base, source.MineHistoryLimit)
	if err != nil {
		return false
	}
	if !mineOK && mine {
		for _, h := range history {
			if matchesIdentity(h, meName, meEmail) {
				mineOK = true
				break
			}
		}
	}
	if !mineOK {
		return false
	}
	if !authorOK && len(patterns) > 0 {
		for _, h := range history {
			if matchesAuthor(h, patterns) {
				authorOK = true
				break
			}
		}
	}
	return mineOK && authorOK
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

// matchesIdentity reports whether ref's tip author or committer equals the
// local git identity (name or email, case-insensitive exact match).
// Committer is included so bot/cursor branches pushed by the user still
// match --mine even when the tip author is the bot.
func matchesIdentity(ref source.BranchRef, name, email string) bool {
	if name != "" && strings.EqualFold(strings.TrimSpace(ref.AuthorName), strings.TrimSpace(name)) {
		return true
	}
	if email != "" && strings.EqualFold(strings.TrimSpace(ref.AuthorEmail), strings.TrimSpace(email)) {
		return true
	}
	if name != "" && strings.EqualFold(strings.TrimSpace(ref.CommitterName), strings.TrimSpace(name)) {
		return true
	}
	if email != "" && strings.EqualFold(strings.TrimSpace(ref.CommitterEmail), strings.TrimSpace(email)) {
		return true
	}
	return false
}

// matchesAuthor reports whether any pattern is a case-insensitive substring
// of the ref's author/committer name, email, or "name <email>" combination.
func matchesAuthor(ref source.BranchRef, patterns []string) bool {
	haystacks := []string{
		strings.ToLower(ref.AuthorName),
		strings.ToLower(ref.AuthorEmail),
		strings.ToLower(strings.TrimSpace(ref.AuthorName + " <" + ref.AuthorEmail + ">")),
		strings.ToLower(ref.CommitterName),
		strings.ToLower(ref.CommitterEmail),
		strings.ToLower(strings.TrimSpace(ref.CommitterName + " <" + ref.CommitterEmail + ">")),
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
	fetchCmd.Flags().StringVar(&fetchRemote, "remote", "", "remote to fetch/list (default: source.git.remote, else origin)")
	fetchCmd.Flags().BoolVar(&fetchMine, "mine", false, "only your branches (tip or last 100 branch-exclusive commits match git config user.name/user.email)")
	fetchCmd.Flags().StringSliceVar(&fetchAuthor, "author", nil, "only branches whose tip or branch-exclusive history matches <name-or-email> (substring, case-insensitive; repeatable)")
	fetchCmd.Flags().BoolVar(&fetchMyPRS, "myprs", false, "only branches with an open PR involving you (GitHub remotes only, via gh)")
	_ = fetchCmd.RegisterFlagCompletionFunc("remote", completeRemotes)
	rootCmd.AddCommand(fetchCmd)
}
