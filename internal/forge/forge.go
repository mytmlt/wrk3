// Package forge answers "which remote branches have PRs involving me?"
// per hosting provider (GitHub first, GitLab/Gitea later).
//
// It sits beside source, not inside it: Source provisions worktrees from
// git refs (Fetch/Refs/Add/Remove/List) while Forge reports pull-request
// state from the host API. CLI commands intersect Forge results with
// Source.Refs, so fork-only PR heads with no local remote ref drop out
// naturally.
//
// There is no Go API client: the GitHub forge shells out to the `gh` CLI
// (auth reuse, no tokens in wrk3.yaml, static binary preserved). New
// forges register in registry.go; unknown hosts error listing available
// options.
package forge

import "context"

// PRBranch is one open PR head relevant to `involves:@me`.
type PRBranch struct {
	// Branch is the head ref name (headRefName).
	Branch string
	// Number is the PR number.
	Number int
	// Title is the PR title.
	Title string
	// URL is the PR web URL.
	URL string
}

// Forge lists involving-me PR branches for one repo checkout.
type Forge interface {
	// Name returns the registered forge name (e.g. "github").
	Name() string
	// MyPRBranches returns open PRs involving the authenticated user,
	// newest first. repoPath scopes `gh` to the right repo; remote picks
	// the ref namespace PR heads are matched against.
	MyPRBranches(ctx context.Context, repoPath, remote string) ([]PRBranch, error)
}
