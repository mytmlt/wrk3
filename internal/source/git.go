package source

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// defaultGitTimeout bounds every git invocation.
const defaultGitTimeout = 60 * time.Second

// GitSource implements Source via stdlib os/exec git calls.
type GitSource struct {
	// Timeout bounds each git command. Zero means defaultGitTimeout.
	Timeout time.Duration
}

// NewGitSource returns a GitSource with the default timeout.
func NewGitSource() Source {
	return &GitSource{}
}

func init() {
	Register("git", NewGitSource)
}

func (g *GitSource) timeout() time.Duration {
	if g.Timeout > 0 {
		return g.Timeout
	}
	return defaultGitTimeout
}

// normalizeRemote returns the effective remote name (default "origin").
func normalizeRemote(remote string) string {
	if strings.TrimSpace(remote) == "" {
		return "origin"
	}
	return strings.TrimSpace(remote)
}

// Fetch runs git fetch <remote> --prune in repoPath.
func (g *GitSource) Fetch(repoPath, remote string) error {
	remote = normalizeRemote(remote)
	_, err := g.run(repoPath, "fetch", remote, "--prune")
	return err
}

// Refs lists remote branches via git branch -r (<remote>/*, HEAD symref skipped).
func (g *GitSource) Refs(repoPath, remote string) ([]string, error) {
	remote = normalizeRemote(remote)
	out, err := g.run(repoPath, "branch", "-r", "--format=%(refname:short)")
	if err != nil {
		return nil, err
	}
	return parseRefs(out, remote), nil
}

// LocalBranches lists local branches via git branch --format.
// Detached HEADs produce no ref line and are naturally absent.
func (g *GitSource) LocalBranches(repoPath string) ([]string, error) {
	out, err := g.run(repoPath, "branch", "--format=%(refname:short)")
	if err != nil {
		return nil, err
	}
	return parseLocalBranches(out), nil
}

// RefsDetailed lists remote branches with tip-commit authors, committers,
// and tip committer dates via git for-each-ref (refs/remotes/<remote>,
// HEAD symref skipped).
func (g *GitSource) RefsDetailed(repoPath, remote string) ([]BranchRef, error) {
	remote = normalizeRemote(remote)
	out, err := g.run(repoPath, "for-each-ref",
		"--format=%(refname:short)%00%(authorname)%00%(authoremail)%00%(committername)%00%(committeremail)%00%(committerdate:unix)",
		"refs/remotes/"+remote)
	if err != nil {
		return nil, err
	}
	return parseRefsDetailed(out, remote), nil
}

// Identity returns git config user.name/user.email (empty when unset).
func (g *GitSource) Identity(repoPath string) (string, string, error) {
	name, err := g.configValue(repoPath, "user.name")
	if err != nil {
		return "", "", err
	}
	email, err := g.configValue(repoPath, "user.email")
	if err != nil {
		return "", "", err
	}
	return name, email, nil
}

// DefaultBranch returns the short default-branch name for remote
// (e.g. "main"). It prefers refs/remotes/<remote>/HEAD, then probes
// main/master. Empty means unknown (callers fall back to tip-only).
func (g *GitSource) DefaultBranch(repoPath, remote string) (string, error) {
	remote = normalizeRemote(remote)
	if out, err := g.run(repoPath, "symbolic-ref", "--quiet", "refs/remotes/"+remote+"/HEAD"); err == nil {
		sym := strings.TrimSpace(out)
		// Symbolic value looks like refs/remotes/<remote>/<branch>.
		if rest, ok := strings.CutPrefix(sym, "refs/remotes/"+remote+"/"); ok && rest != "" && rest != "HEAD" {
			return rest, nil
		}
	}
	for _, cand := range []string{"main", "master"} {
		if g.refExists(repoPath, "refs/remotes/"+remote+"/"+cand) {
			return cand, nil
		}
	}
	return "", nil
}

// BranchHistory lists up to limit branch-exclusive commits (newest first).
// With a non-empty base it runs git log <remote>/<branch> --not
// <remote>/<base>; with an empty base it logs the branch tip directly.
// Each entry carries the branch name plus one commit's author/committer.
func (g *GitSource) BranchHistory(repoPath, remote, branch, base string, limit int) ([]BranchRef, error) {
	remote = normalizeRemote(remote)
	if strings.TrimSpace(branch) == "" {
		return nil, fmt.Errorf("git log: empty branch")
	}
	if limit <= 0 {
		limit = MineHistoryLimit
	}
	target := remote + "/" + strings.TrimSpace(branch)
	base = strings.TrimSpace(base)
	branch = strings.TrimSpace(branch)
	// The default branch has no exclusive commits by definition.
	if base != "" && base == branch {
		return nil, nil
	}
	format := "%an%x00%ae%x00%cn%x00%ce"
	// --no-merges would drop your "Merge main into ..." commits, which
	// often carry your identity on cursor branches — keep merges.
	args := []string{"log", "--format=" + format, "-n", fmt.Sprintf("%d", limit), target}
	if base != "" {
		args = append(args, "--not", remote+"/"+base)
	}
	out, err := g.run(repoPath, args...)
	if err != nil {
		return nil, err
	}
	return parseBranchHistory(out, strings.TrimSpace(branch)), nil
}

// configValue reads one git config key; unset keys yield "" with nil error
// (the repo itself must exist — Fetch surfaces that failure first).
func (g *GitSource) configValue(repoPath, key string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), g.timeout())
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "config", "--get", key)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if strings.TrimSpace(stdout.String()) == "" {
			return "", nil
		}
		return "", fmt.Errorf("git config --get %s: %w: %s",
			key, err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// Add creates a worktree for branch at worktreePath.
// When the branch has no local ref but <remote>/<branch> exists, it creates
// a tracking branch: git worktree add --track -b <branch> <path> <remote>/<branch>.
func (g *GitSource) Add(repoPath, branch, worktreePath, remote string) error {
	if branch == "" {
		return fmt.Errorf("git worktree add: empty branch")
	}
	if worktreePath == "" {
		return fmt.Errorf("git worktree add: empty worktree path")
	}
	remote = normalizeRemote(remote)
	if g.refExists(repoPath, "refs/heads/"+branch) {
		_, err := g.run(repoPath, "worktree", "add", worktreePath, branch)
		return err
	}
	if g.refExists(repoPath, "refs/remotes/"+remote+"/"+branch) {
		_, err := g.run(repoPath, "worktree", "add", "--track", "-b", branch, worktreePath, remote+"/"+branch)
		return err
	}
	_, err := g.run(repoPath, "worktree", "add", worktreePath, branch)
	return err
}

// AddNew creates a worktree at worktreePath with a NEW local branch
// starting at base: git worktree add -b <branch> <path> <base>.
func (g *GitSource) AddNew(repoPath, branch, worktreePath, base string) error {
	if branch == "" {
		return fmt.Errorf("git worktree add: empty branch")
	}
	if worktreePath == "" {
		return fmt.Errorf("git worktree add: empty worktree path")
	}
	if strings.TrimSpace(base) == "" {
		return fmt.Errorf("git worktree add: empty base")
	}
	_, err := g.run(repoPath, "worktree", "add", "-b", branch, worktreePath, strings.TrimSpace(base))
	return err
}

// refExists reports whether ref resolves in repoPath (quiet, no output).
func (g *GitSource) refExists(repoPath, ref string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), g.timeout())
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "show-ref", "--verify", "--quiet", ref)
	return cmd.Run() == nil
}

// Remove deletes the worktree at worktreePath.
func (g *GitSource) Remove(repoPath, worktreePath string, force bool) error {
	if worktreePath == "" {
		return fmt.Errorf("git worktree remove: empty worktree path")
	}
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, worktreePath)
	_, err := g.run(repoPath, args...)
	return err
}

// Pull runs git pull inside the worktree at worktreePath (fast-forwards or
// merges the tracking branch). Rebase and FFOnly map to --rebase/--ff-only
// and are mutually exclusive.
func (g *GitSource) Pull(worktreePath string, opts PullOptions) error {
	if strings.TrimSpace(worktreePath) == "" {
		return fmt.Errorf("git pull: empty worktree path")
	}
	if opts.Rebase && opts.FFOnly {
		return fmt.Errorf("git pull: pass either --rebase or --ff-only, not both")
	}
	args := []string{"pull"}
	if opts.Rebase {
		args = append(args, "--rebase")
	}
	if opts.FFOnly {
		args = append(args, "--ff-only")
	}
	_, err := g.run(worktreePath, args...)
	return err
}

// List parses git worktree list --porcelain.
func (g *GitSource) List(repoPath string) ([]WorktreeInfo, error) {
	out, err := g.run(repoPath, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	return parseWorktreePorcelain(out), nil
}

func (g *GitSource) run(repoPath string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), g.timeout())
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repoPath}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s",
			strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

func parseRefs(out, remote string) []string {
	remote = normalizeRemote(remote)
	prefix := remote + "/"
	seen := map[string]struct{}{}
	var refs []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(line, "->") {
			continue
		}
		// `git branch -r --format=%(refname:short)` renders the
		// <remote>/HEAD symref as bare "<remote>" (no "->" marker).
		// Skip it: only <remote>/* entries are real branches.
		// Other remotes are listed too — keep only the requested one.
		if line == remote || !strings.HasPrefix(line, prefix) {
			continue
		}
		name := strings.TrimPrefix(line, prefix)
		if name == "" || name == "HEAD" {
			continue
		}
		if _, ok := seen[name]; !ok {
			seen[name] = struct{}{}
			refs = append(refs, name)
		}
	}
	return refs
}

func parseLocalBranches(out string) []string {
	seen := map[string]struct{}{}
	var branches []string
	for _, line := range strings.Split(out, "\n") {
		name := strings.TrimSpace(line)
		if name == "" || strings.ContainsAny(name, " \t()*") {
			continue
		}
		if _, ok := seen[name]; !ok {
			seen[name] = struct{}{}
			branches = append(branches, name)
		}
	}
	return branches
}

func parseRefsDetailed(out, remote string) []BranchRef {
	remote = normalizeRemote(remote)
	var refs []BranchRef
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		// Format is ref%00author%00<email>%00committer%00<email>%00unixtime;
		// branch names never contain NUL so SplitN is exact.
		// Older output with only 3 fields (no committer) or 5 fields
		// (no date) is still accepted, with missing fields left zero.
		parts := strings.SplitN(line, "\x00", 6)
		if len(parts) != 3 && len(parts) != 5 && len(parts) != 6 {
			continue
		}
		short := strings.TrimSpace(parts[0])
		if short == "" || strings.Contains(short, "->") {
			continue
		}
		// refs/remotes/<remote>/HEAD renders as bare "<remote>".
		if short == remote {
			continue
		}
		name := strings.TrimPrefix(short, remote+"/")
		if name == "" || name == "HEAD" {
			continue
		}
		// for-each-ref is scoped to refs/remotes/<remote> so every
		// short ref should carry the prefix; skip anything else.
		if short == name {
			continue
		}
		refs = append(refs, BranchRef{
			Name:        name,
			AuthorName:  strings.TrimSpace(parts[1]),
			AuthorEmail: strings.Trim(strings.TrimSpace(parts[2]), "<>"),
			CommitterName: func() string {
				if len(parts) > 3 {
					return strings.TrimSpace(parts[3])
				}
				return ""
			}(),
			CommitterEmail: func() string {
				if len(parts) > 4 {
					return strings.Trim(strings.TrimSpace(parts[4]), "<>")
				}
				return ""
			}(),
			CommitterDate: func() time.Time {
				if len(parts) > 5 {
					if unix, err := strconv.ParseInt(strings.TrimSpace(parts[5]), 10, 64); err == nil && unix > 0 {
						return time.Unix(unix, 0)
					}
				}
				return time.Time{}
			}(),
		})
	}
	return refs
}

// parseBranchHistory parses `git log --format=%an%00%ae%00%cn%00%ce`
// output (one commit per line, NUL-separated fields) into BranchRefs.
// Every entry carries branch as Name so cmd matching helpers apply.
func parseBranchHistory(out, branch string) []BranchRef {
	var refs []BranchRef
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, "\x00", 4)
		if len(parts) != 4 {
			continue
		}
		refs = append(refs, BranchRef{
			Name:           branch,
			AuthorName:     strings.TrimSpace(parts[0]),
			AuthorEmail:    strings.Trim(strings.TrimSpace(parts[1]), "<>"),
			CommitterName:  strings.TrimSpace(parts[2]),
			CommitterEmail: strings.Trim(strings.TrimSpace(parts[3]), "<>"),
		})
	}
	return refs
}

func parseWorktreePorcelain(out string) []WorktreeInfo {
	var infos []WorktreeInfo
	var cur *WorktreeInfo
	flush := func() {
		if cur != nil && cur.Path != "" {
			infos = append(infos, *cur)
		}
		cur = nil
	}
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			cur = &WorktreeInfo{Path: strings.TrimPrefix(line, "worktree ")}
		case cur != nil && strings.HasPrefix(line, "HEAD "):
			cur.Commit = strings.TrimPrefix(line, "HEAD ")
		case cur != nil && strings.HasPrefix(line, "branch "):
			cur.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		case cur != nil && strings.TrimSpace(line) == "bare":
			cur.Bare = true
		case strings.TrimSpace(line) == "":
			flush()
		}
	}
	flush()
	return infos
}
