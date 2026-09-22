package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mytmlt/wrk3/internal/ports"
	"github.com/mytmlt/wrk3/internal/source"
)

var (
	addSelect   bool
	addLocal    bool
	addRemote   string
	addMine     bool
	addMyPRS    bool
	addCreate   bool
	addNoCreate bool
)

var addCmd = &cobra.Command{
	Use:               "add [branch...] | --select | --local | --remote <name> [--mine] [--myprs]",
	Short:             "worktree add + port assign + .env ensure (bare = select)",
	ValidArgsFunction: completeAddBranches,
	RunE: func(cmd *cobra.Command, args []string) error {
		r, err := resolveConfig()
		if err != nil {
			return err
		}
		logProxyEnsureForCmd(cmd, r)
		if addLocal && (addSelect || addRemote != "" || addMine || addMyPRS) {
			return fmt.Errorf("pass only one of --local, --select, --remote/--mine/--myprs")
		}
		if addSelect && (addRemote != "" || addMine || addMyPRS) {
			return fmt.Errorf("pass only one of --local, --select, --remote/--mine/--myprs")
		}
		if err := validateCreateFlags(args); err != nil {
			return err
		}
		if addLocal {
			return addLocalMode(cmd, r, args)
		}
		if addRemote != "" || addMine || addMyPRS {
			return addRemoteMode(cmd, r, args)
		}
		branches := args
		selectMode := addSelect
		if selectMode {
			if len(args) > 0 {
				return fmt.Errorf("pass either branch names or --select, not both")
			}
		} else if len(args) == 0 {
			// Bare `add` behaves like --select for fast interactive pick.
			selectMode = true
		}
		if selectMode {
			remote := resolveRemote(r, "")
			if err := r.src.Fetch(r.cfg.RepoPath(), remote); err != nil {
				return fmt.Errorf("fetch: %w", err)
			}
			refs, err := r.src.Refs(r.cfg.RepoPath(), remote)
			if err != nil {
				return fmt.Errorf("list refs: %w", err)
			}
			// Offer local-only branches too: addOne creates the
			// worktree from the local ref when no remote ref exists.
			if local, err := r.src.LocalBranches(r.cfg.RepoPath()); err == nil {
				refs = unionBranches(refs, local)
			}
			branches, err = selectBranches(cmd, refs)
			if err != nil {
				return err
			}
		}
		if len(branches) == 0 {
			return fmt.Errorf("pass branch names or --select")
		}
		if err := ensureBase(r.base); err != nil {
			return err
		}
		recs, err := loadState(r)
		if err != nil {
			return err
		}
		alloc := r.cfg.Allocator()
		remote := resolveRemote(r, "")
		byBranch := listLocalWorktrees(r)
		for _, branch := range branches {
			if branch == "" {
				return fmt.Errorf("branch name must not be empty")
			}
			if existing := findRecord(recs, branch); existing != nil {
				return fmt.Errorf("worktree for %q already exists at %s", branch, existing.AbsPath)
			}
			res, err := addAdoptOrCreateOne(cmd, r, &recs, &alloc, byBranch, branch, remote, false)
			if err != nil {
				return err
			}
			warnEnv(cmd, res.warns)
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s %s%s\n", res.verb, branch, res.note); err != nil {
				return fmt.Errorf("write output: %w", err)
			}
		}
		if err := touchProject(r.cfg); err != nil {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: register project: %v\n", err)
		}
		return nil
	},
}

// addRemoteMode bulk-creates worktrees from a git remote:
// fetch <remote> --prune, then create every (or only --mine/--myprs)
// branch, skipping ones already registered. With explicit args (no
// --mine/--myprs) it creates just those branches, resolving remote-only
// names as tracking branches. No interactive picker here — bare
// `add --remote` means "all".
func addRemoteMode(cmd *cobra.Command, r *resolved, args []string) error {
	if (addMine || addMyPRS) && len(args) > 0 {
		return fmt.Errorf("pass either branch names or --mine/--myprs, not both")
	}
	logProxyEnsureForCmd(cmd, r)
	if err := validateCreateFlags(args); err != nil {
		return err
	}
	remote := resolveRemote(r, addRemote)
	if err := r.src.Fetch(r.cfg.RepoPath(), remote); err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	if err := ensureBase(r.base); err != nil {
		return err
	}
	recs, err := loadState(r)
	if err != nil {
		return err
	}
	alloc := r.cfg.Allocator()
	// Explicit names + --remote: fail fast on duplicates (typo safety),
	// like plain `add branch...`, but resolve via the given remote.
	// Existing on-disk worktrees are adopted, not re-created. Unknown
	// names fall through to the create prompt (refs are fresh: fetched
	// above, so refreshed=true skips a second fetch).
	if len(args) > 0 {
		byBranch := listLocalWorktrees(r)
		for _, branch := range args {
			if branch == "" {
				return fmt.Errorf("branch name must not be empty")
			}
			if existing := findRecord(recs, branch); existing != nil {
				return fmt.Errorf("worktree for %q already exists at %s", branch, existing.AbsPath)
			}
			res, err := addAdoptOrCreateOne(cmd, r, &recs, &alloc, byBranch, branch, remote, true)
			if err != nil {
				return err
			}
			warnEnv(cmd, res.warns)
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s %s%s\n", res.verb, branch, res.note); err != nil {
				return fmt.Errorf("write output: %w", err)
			}
		}
		if err := touchProject(r.cfg); err != nil {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: register project: %v\n", err)
		}
		return nil
	}
	var branches []string
	if addMine {
		branches, err = filterRefs(r.src, r.cfg.RepoPath(), remote, true, nil)
		if err != nil {
			return err
		}
		if len(branches) == 0 {
			return fmt.Errorf("no branches for --mine on remote %q (see fetch --mine)", remote)
		}
	} else {
		branches, err = r.src.Refs(r.cfg.RepoPath(), remote)
		if err != nil {
			return fmt.Errorf("list refs: %w", err)
		}
		if len(branches) == 0 {
			return fmt.Errorf("no remote branches on %q", remote)
		}
	}
	if addMyPRS {
		prs, err := myPRBranches(r.cfg.RepoPath(), remote)
		if err != nil {
			return err
		}
		branches = intersectMyPRS(branches, prs)
		if len(branches) == 0 {
			return fmt.Errorf("no branches with open PRs involving you on remote %q (see fetch --myprs)", remote)
		}
	}
	sort.Strings(branches)
	// Branches already checked out anywhere (e.g. main at the repo root)
	// cannot get a second worktree — skip them like already-registered.
	checkedOut := map[string]string{}
	if infos, err := r.src.List(r.cfg.RepoPath()); err == nil {
		for _, info := range infos {
			if info.Branch != "" {
				if _, ok := checkedOut[info.Branch]; !ok {
					checkedOut[info.Branch] = info.Path
				}
			}
		}
	}
	added := 0
	for _, branch := range branches {
		if existing := findRecord(recs, branch); existing != nil {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "skipped %s (already registered)\n", branch); err != nil {
				return fmt.Errorf("write output: %w", err)
			}
			continue
		}
		if path, ok := checkedOut[branch]; ok {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "skipped %s (already checked out at %s)\n", branch, path); err != nil {
				return fmt.Errorf("write output: %w", err)
			}
			continue
		}
		warns, err := addOne(r, &recs, &alloc, branch, remote)
		if err != nil {
			return err
		}
		warnEnv(cmd, warns)
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "added %s\n", branch); err != nil {
			return fmt.Errorf("write output: %w", err)
		}
		added++
	}
	if added == 0 {
		return fmt.Errorf("nothing to add (%d branches, all already registered or checked out)", len(branches))
	}
	if err := touchProject(r.cfg); err != nil {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: register project: %v\n", err)
	}
	return nil
}

// addLocalMode adopts branches that already have local git worktrees
// (git worktree list) into wrk3 state: port assign + .env write, without
// fetching from origin or creating new worktrees.
//
// With explicit args only those branches are adopted (each must have a
// worktree, else error). Bare `add --local` adopts every unregistered
// local worktree, skipping ones already in state.
func addLocalMode(cmd *cobra.Command, r *resolved, args []string) error {
	infos, err := r.src.List(r.cfg.RepoPath())
	if err != nil {
		return fmt.Errorf("list worktrees: %w", err)
	}
	logProxyEnsureForCmd(cmd, r)
	byBranch := localWorktreesByBranch(r, infos)
	if len(args) > 0 {
		recs, err := loadState(r)
		if err != nil {
			return err
		}
		alloc := r.cfg.Allocator()
		for _, branch := range args {
			if branch == "" {
				return fmt.Errorf("branch name must not be empty")
			}
			// Accept branch names and slugs interchangeably.
			resolved := lookupLocalBranch(byBranch, branch)
			if resolved == "" {
				return fmt.Errorf("no local worktree for %q (see git worktree list)", branch)
			}
			branch = resolved
			path := byBranch[branch]
			if existing := findRecord(recs, branch); existing != nil {
				return fmt.Errorf("worktree for %q already exists at %s", branch, existing.AbsPath)
			}
			warns, err := adoptOne(r, &recs, &alloc, branch, path)
			if err != nil {
				return err
			}
			warnEnv(cmd, warns)
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "added %s\n", branch); err != nil {
				return fmt.Errorf("write output: %w", err)
			}
		}
		if err := touchProject(r.cfg); err != nil {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: register project: %v\n", err)
		}
		return nil
	}
	recs, err := loadState(r)
	if err != nil {
		return err
	}
	alloc := r.cfg.Allocator()
	added := 0
	// Sorted for deterministic port index assignment.
	branches := make([]string, 0, len(byBranch))
	for branch := range byBranch {
		branches = append(branches, branch)
	}
	sort.Strings(branches)
	for _, branch := range branches {
		path := byBranch[branch]
		if existing := findRecord(recs, branch); existing != nil {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "skipped %s (already registered)\n", branch); err != nil {
				return fmt.Errorf("write output: %w", err)
			}
			continue
		}
		warns, err := adoptOne(r, &recs, &alloc, branch, path)
		if err != nil {
			return err
		}
		warnEnv(cmd, warns)
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "added %s\n", branch); err != nil {
			return fmt.Errorf("write output: %w", err)
		}
		added++
	}
	if added == 0 {
		return fmt.Errorf("no unregistered local worktrees found (see git worktree list)")
	}
	if err := touchProject(r.cfg); err != nil {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: register project: %v\n", err)
	}
	return nil
}

// localWorktreesByBranch indexes existing worktrees by branch name,
// skipping the main checkout (repo root), bare repos, and detached HEADs
// (no branch). Paths are cleaned for stable comparison.
func localWorktreesByBranch(r *resolved, infos []source.WorktreeInfo) map[string]string {
	out := map[string]string{}
	repoRoot := filepath.Clean(r.cfg.RepoPath())
	for _, info := range infos {
		if info.Branch == "" || info.Bare {
			continue
		}
		// sameRepoRoot tolerates macOS /var -> /private/var symlinks: git
		// reports the resolved path while RepoPath keeps the logical form.
		if sameRepoRoot(info.Path, repoRoot) {
			continue
		}
		if _, ok := out[info.Branch]; !ok {
			out[info.Branch] = filepath.Clean(info.Path)
		}
	}
	return out
}

// lookupLocalBranch resolves a branch-or-slug name to the branch key in
// byBranch, or "" when there is no match.
func lookupLocalBranch(byBranch map[string]string, branchOrSlug string) string {
	if _, ok := byBranch[branchOrSlug]; ok {
		return branchOrSlug
	}
	for branch := range byBranch {
		if source.Slugify(branch) == branchOrSlug {
			return branch
		}
	}
	return ""
}

// listLocalWorktrees indexes existing on-disk worktrees by branch.
// List failures degrade to empty (callers fall back to creating).
func listLocalWorktrees(r *resolved) map[string]string {
	infos, err := r.src.List(r.cfg.RepoPath())
	if err != nil {
		return map[string]string{}
	}
	return localWorktreesByBranch(r, infos)
}

// validateCreateFlags restricts --create/--no-create to explicit branch
// names: they never apply to --local, --select, or bulk --remote/--mine/
// --myprs modes (bare `add` counts as --select).
func validateCreateFlags(args []string) error {
	if addCreate && addNoCreate {
		return fmt.Errorf("pass either --create or --no-create, not both")
	}
	if addCreate || addNoCreate {
		if addLocal || addSelect || addMine || addMyPRS {
			return fmt.Errorf("--create/--no-create only work with explicit branch names")
		}
		if len(args) == 0 {
			return fmt.Errorf("pass branch names with --create/--no-create")
		}
	}
	return nil
}

// addResult describes how one branch was registered for output.
type addResult struct {
	warns []string
	verb  string // "added" or "adopted"
	note  string // e.g. " (new branch from origin/main)", empty otherwise
}

// addAdoptOrCreateOne adopts an existing on-disk worktree when the branch
// (or slug) already has one, checks out a known branch via addOne, or
// creates a new branch from the remote default when the name matches
// nothing. refreshed reports whether refs were just fetched (the --remote
// explicit path), skipping a second fetch on the unknown-name path.
func addAdoptOrCreateOne(cmd *cobra.Command, r *resolved, recs *[]ports.WorktreeRecord, alloc *ports.Allocator, byBranch map[string]string, branch, remote string, refreshed bool) (addResult, error) {
	if resolved := lookupLocalBranch(byBranch, branch); resolved != "" {
		warns, err := adoptOne(r, recs, alloc, resolved, byBranch[resolved])
		if err != nil {
			return addResult{}, err
		}
		return addResult{warns: warns, verb: "adopted"}, nil
	}
	if branchKnown(r, branch, remote) {
		warns, err := addOne(r, recs, alloc, branch, remote)
		if err != nil {
			return addResult{}, err
		}
		return addResult{warns: warns, verb: "added"}, nil
	}
	if !refreshed {
		// Explicit `add` does not fetch up front, so a remote branch
		// that simply hasn't been fetched yet looks "new". Refresh
		// once before offering creation; fetch failures degrade to a
		// warning so offline creation still works from cached refs.
		if err := r.src.Fetch(r.cfg.RepoPath(), remote); err != nil {
			warnf(cmd, "fetch %s failed: %v; using cached refs", remote, err)
		} else if branchKnown(r, branch, remote) {
			warns, err := addOne(r, recs, alloc, branch, remote)
			if err != nil {
				return addResult{}, err
			}
			return addResult{warns: warns, verb: "added"}, nil
		}
	}
	startPoint, baseDesc := resolveCreateBase(cmd, r, remote)
	if addNoCreate {
		return addResult{}, fmt.Errorf("branch %q not found locally or on %q (use --create to create it)", branch, remote)
	}
	if !addCreate {
		ok, err := confirmCreate(cmd, branch, remote, baseDesc)
		if err != nil {
			return addResult{}, err
		}
		if !ok {
			return addResult{}, fmt.Errorf("cancelled: not creating branch %q (use --create to skip this prompt)", branch)
		}
	}
	warns, err := addNewOne(r, recs, alloc, branch, startPoint)
	if err != nil {
		return addResult{}, err
	}
	return addResult{warns: warns, verb: "added", note: " (new branch from " + baseDesc + ")"}, nil
}

// branchKnown reports whether branch exists locally or on remote. List
// failures degrade to false (callers fetch, then fall through to the
// create prompt).
func branchKnown(r *resolved, branch, remote string) bool {
	if local, err := r.src.LocalBranches(r.cfg.RepoPath()); err == nil {
		for _, b := range local {
			if b == branch {
				return true
			}
		}
	}
	if refs, err := r.src.Refs(r.cfg.RepoPath(), remote); err == nil {
		for _, b := range refs {
			if b == branch {
				return true
			}
		}
	}
	return false
}

// resolveCreateBase returns the start point for a new branch: the remote
// default (<remote>/<base>) when known, else HEAD with a warning.
func resolveCreateBase(cmd *cobra.Command, r *resolved, remote string) (startPoint, desc string) {
	if base, err := r.src.DefaultBranch(r.cfg.RepoPath(), remote); err != nil {
		warnf(cmd, "cannot determine default branch on %s: %v; creating from HEAD", remote, err)
		return "HEAD", "HEAD"
	} else if strings.TrimSpace(base) != "" {
		base = strings.TrimSpace(base)
		return remote + "/" + base, remote + "/" + base
	}
	warnf(cmd, "default branch on %s unknown; creating from HEAD", remote)
	return "HEAD", "HEAD"
}

// confirmCreate prompts to create an unknown branch. Empty input, EOF
// (pipes/CI), or anything but y/yes aborts.
func confirmCreate(cmd *cobra.Command, branch, remote, baseDesc string) (bool, error) {
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "branch %q not found locally or on %q. Create new branch from %s? [y/N]: ", branch, remote, baseDesc); err != nil {
		return false, fmt.Errorf("write prompt: %w", err)
	}
	sc := bufio.NewScanner(cmd.InOrStdin())
	if !sc.Scan() {
		if err := sc.Err(); err != nil {
			return false, fmt.Errorf("read confirmation: %w", err)
		}
		return false, nil
	}
	ans := strings.ToLower(strings.TrimSpace(sc.Text()))
	return ans == "y" || ans == "yes", nil
}

// addOne creates one worktree: git add + copy includes + port assign +
// state + .env ensure. The new .env inherits non-managed keys (secrets)
// from the repo-root checkout's .env when present; managed port keys are
// always written to this worktree's allocation. Remote-only
// branches are created as tracking branches (--track -b).
func addOne(r *resolved, recs *[]ports.WorktreeRecord, alloc *ports.Allocator, branch, remote string) ([]string, error) {
	return createWorktreeRecord(r, recs, alloc, branch, func(path string) error {
		return r.src.Add(r.cfg.RepoPath(), branch, path, remote)
	})
}

// addNewOne creates one worktree with a NEW local branch starting at
// startPoint (remote default or HEAD): git worktree add -b + copy
// includes + port assign + state + .env ensure.
func addNewOne(r *resolved, recs *[]ports.WorktreeRecord, alloc *ports.Allocator, branch, startPoint string) ([]string, error) {
	return createWorktreeRecord(r, recs, alloc, branch, func(path string) error {
		return r.src.AddNew(r.cfg.RepoPath(), branch, path, startPoint)
	})
}

// createWorktreeRecord allocates slug/path/ports, runs create (git worktree
// add), then copy includes + .env ensure + state append. Shared by addOne
// (existing branch) and addNewOne (new branch from a base). Index stays a
// monotonic id (max+1); ports come from the lowest free range allocation
// (gap reuse, OS-aware).
func createWorktreeRecord(r *resolved, recs *[]ports.WorktreeRecord, alloc *ports.Allocator, branch string, create func(path string) error) ([]string, error) {
	slug := source.Slugify(branch)
	path := worktreePath(r.base, slug)
	if existing := findRecord(*recs, slug); existing != nil && existing.Branch != branch {
		return nil, fmt.Errorf("slug %q for branch %q collides with branch %q", slug, branch, existing.Branch)
	}
	if infos, err := r.src.List(r.cfg.RepoPath()); err == nil {
		for _, info := range infos {
			if info.Bare || info.Branch == "" {
				continue
			}
			if info.Branch == branch {
				return nil, fmt.Errorf("branch %q is already checked out at %s (use add --local to adopt it into state)", branch, info.Path)
			}
		}
	}
	idx := nextIndex(*recs)
	envAlloc, err := assignAllocation(r, *recs)
	if err != nil {
		return nil, fmt.Errorf("add worktree %q: %w", branch, err)
	}
	composeProject := r.cfg.ComposeOptions(slug).ProjectName()
	if err := create(path); err != nil {
		return nil, fmt.Errorf("add worktree %q: %w", branch, err)
	}
	var warns []string
	if copyWarns, err := source.CopyIncluded(r.cfg.RepoPath(), path, r.cfg.Source.Git.Copy); err != nil {
		_ = r.src.Remove(r.cfg.RepoPath(), path, true)
		return nil, fmt.Errorf("add worktree %q: %w", branch, err)
	} else {
		warns = append(warns, copyWarns...)
	}
	if err := ensureWorktreeEnv(r, ports.WorktreeRecord{
		Branch: branch, Slug: slug, AbsPath: path, Ports: envAlloc.Ports, Urls: envAlloc.URLs,
	}, r.cfg.URLSpecs()); err != nil {
		return nil, err
	}
	*recs = append(*recs, ports.WorktreeRecord{
		Branch:         branch,
		Slug:           slug,
		AbsPath:        path,
		Index:          idx,
		Ports:          envAlloc.Ports,
		Urls:           envAlloc.URLs,
		ComposeProject: composeProject,
		Status:         ports.StatusStopped,
	})
	if err := saveState(r, *recs); err != nil {
		r.logOpDone("add", "add "+branch, err)
		return nil, err
	}
	r.logOpDone("add", fmt.Sprintf("added %s (index %d)", branch, idx), nil)
	return warns, nil
}

// adoptOne registers one pre-existing worktree path: port assign + state +
// .env ensure. A missing .env is seeded with non-managed keys from the
// repo-root checkout's .env; managed port keys are overwritten to this
// worktree's allocation. No git worktree add — the checkout
// already exists. Index stays monotonic; ports are the lowest free range
// allocation (gap reuse, OS-aware).
func adoptOne(r *resolved, recs *[]ports.WorktreeRecord, alloc *ports.Allocator, branch, path string) ([]string, error) {
	slug := source.Slugify(branch)
	if existing := findRecord(*recs, slug); existing != nil && existing.Branch != branch {
		return nil, fmt.Errorf("slug %q for branch %q collides with branch %q", slug, branch, existing.Branch)
	}
	if st, err := os.Stat(path); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("adopt worktree %q: path %s missing or not a directory", branch, path)
	}
	idx := nextIndex(*recs)
	envAlloc, err := assignAllocation(r, *recs)
	if err != nil {
		return nil, fmt.Errorf("adopt worktree %q: %w", branch, err)
	}
	composeProject := r.cfg.ComposeOptions(slug).ProjectName()
	if err := ensureWorktreeEnv(r, ports.WorktreeRecord{
		Branch: branch, Slug: slug, AbsPath: path, Ports: envAlloc.Ports, Urls: envAlloc.URLs,
	}, r.cfg.URLSpecs()); err != nil {
		return nil, err
	}
	*recs = append(*recs, ports.WorktreeRecord{
		Branch:         branch,
		Slug:           slug,
		AbsPath:        path,
		Index:          idx,
		Ports:          envAlloc.Ports,
		Urls:           envAlloc.URLs,
		ComposeProject: composeProject,
		Status:         ports.StatusStopped,
	})
	if err := saveState(r, *recs); err != nil {
		r.logOpDone("adopt", "adopt "+branch, err)
		return nil, err
	}
	r.logOpDone("adopt", fmt.Sprintf("adopted %s (index %d)", branch, idx), nil)
	return nil, nil
}

func init() {
	addCmd.Flags().BoolVar(&addSelect, "select", false, "interactive branch select")
	addCmd.Flags().BoolVar(&addLocal, "local", false, "adopt existing local worktrees (no fetch)")
	addCmd.Flags().StringVar(&addRemote, "remote", "", "create worktrees from remote branches (default: source.git.remote, else origin)")
	addCmd.Flags().BoolVar(&addMine, "mine", false, "with --remote: only your branches (tip or branch-exclusive history matches git config user)")
	addCmd.Flags().BoolVar(&addMyPRS, "myprs", false, "with --remote: only branches with an open PR involving you (GitHub remotes only, via gh)")
	addCmd.Flags().BoolVar(&addCreate, "create", false, "with explicit branch names: create a new branch from the remote default when the name matches nothing (skip the prompt)")
	addCmd.Flags().BoolVar(&addNoCreate, "no-create", false, "with explicit branch names: fail fast on unknown names instead of prompting (for scripts/CI)")
	_ = addCmd.RegisterFlagCompletionFunc("remote", completeRemotes)
	rootCmd.AddCommand(addCmd)
}
