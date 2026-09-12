package forge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// defaultGitHubTimeout bounds every `gh` invocation.
const defaultGitHubTimeout = 30 * time.Second

// myPRSearch mirrors the pulls filter
// is:pr state:open involves:<me>, with @me resolved by `gh` so no
// username needs configuring.
const myPRSearch = "is:open involves:@me"

// GitHubForge implements Forge via the `gh` CLI (auth reuse, no tokens
// in wrk3 config). `gh` resolves the repo from cwd, so repoPath scopes
// the query and no owner/repo flags are needed.
type GitHubForge struct {
	// Timeout bounds each `gh` call. Zero means defaultGitHubTimeout.
	Timeout time.Duration
	// LookPath and Run are seams for tests (default: exec.LookPath, real exec).
	LookPath func(string) (string, error)
	Run      func(ctx context.Context, dir, name string, args ...string) (string, error)
}

var _ Forge = (*GitHubForge)(nil)

// NewGitHubForge returns a GitHubForge with default exec behavior.
func NewGitHubForge() Forge {
	return &GitHubForge{}
}

func init() {
	Register(KindGitHub, NewGitHubForge)
}

// Name returns the registered forge name.
func (g *GitHubForge) Name() string { return KindGitHub }

func (g *GitHubForge) timeout() time.Duration {
	if g.Timeout > 0 {
		return g.Timeout
	}
	return defaultGitHubTimeout
}

// ghPR is one row of `gh pr list --json`.
type ghPR struct {
	Number      int    `json:"number"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	HeadRefName string `json:"headRefName"`
}

// MyPRBranches lists open PRs involving the authenticated `gh` user.
// Missing `gh` or failed auth return guidance errors naming the fix
// (`gh auth login`); wrk3 never sees tokens.
func (g *GitHubForge) MyPRBranches(ctx context.Context, repoPath, remote string) ([]PRBranch, error) {
	look := g.LookPath
	if look == nil {
		look = exec.LookPath
	}
	if _, err := look("gh"); err != nil {
		return nil, fmt.Errorf("github forge needs the gh CLI (https://cli.github.com): install it, then run `gh auth login`")
	}
	run := g.Run
	if run == nil {
		run = runGH
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(ctx, g.timeout())
	defer cancel()
	// --repo is omitted on purpose: cwd pins the query to this checkout,
	// including worktrees and `-f` configs outside the repo.
	out, err := run(ctx, repoPath, "gh", "pr", "list",
		"--search", myPRSearch, "--state", "open",
		"--json", "number,title,url,headRefName", "--limit", "100")
	if err != nil {
		if isAuthErr(err.Error()) {
			return nil, fmt.Errorf("gh is not authenticated: run `gh auth login`, then retry: %w", err)
		}
		return nil, fmt.Errorf("gh pr list: %w", err)
	}
	return parseGitHubPRs(out)
}

// runGH executes name with args in dir, capturing stderr into the error.
func runGH(ctx context.Context, dir, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// parseGitHubPRs decodes `gh pr list --json` output. Rows with an empty
// headRefName are skipped (deleted-head edge); ordering is newest first
// as returned by `gh`.
func parseGitHubPRs(out string) ([]PRBranch, error) {
	if strings.TrimSpace(out) == "" {
		return nil, nil
	}
	var rows []ghPR
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		return nil, fmt.Errorf("decode gh pr list output: %w", err)
	}
	var prs []PRBranch
	for _, r := range rows {
		if strings.TrimSpace(r.HeadRefName) == "" {
			continue
		}
		prs = append(prs, PRBranch{
			Branch: strings.TrimSpace(r.HeadRefName),
			Number: r.Number,
			Title:  r.Title,
			URL:    r.URL,
		})
	}
	return prs, nil
}

// isAuthErr spots `gh` not-logged-in output so the error can point at
// `gh auth login` instead of dumping raw CLI text.
func isAuthErr(s string) bool {
	l := strings.ToLower(s)
	for _, needle := range []string{
		"not logged into any github hosts",
		"not authenticated",
		"authentication required",
		"gh auth login",
	} {
		if strings.Contains(l, needle) {
			return true
		}
	}
	return false
}
