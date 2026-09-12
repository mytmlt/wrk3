package source

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
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

// RefsDetailed lists remote branches with tip-commit authors via
// git for-each-ref (refs/remotes/<remote>, HEAD symref skipped).
func (g *GitSource) RefsDetailed(repoPath, remote string) ([]BranchRef, error) {
	remote = normalizeRemote(remote)
	out, err := g.run(repoPath, "for-each-ref",
		"--format=%(refname:short)%00%(authorname)%00%(authoremail)",
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

func parseRefsDetailed(out, remote string) []BranchRef {
	remote = normalizeRemote(remote)
	var refs []BranchRef
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		// Format is ref%00author%00<email>; branch names never
		// contain NUL so SplitN is exact.
		parts := strings.SplitN(line, "\x00", 3)
		if len(parts) != 3 {
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
