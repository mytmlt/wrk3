package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/mytmlt/wrk3/internal/forge"
	"github.com/mytmlt/wrk3/internal/source"
)

// myprsTimeout bounds the `gh` PR query behind --myprs.
const myprsTimeout = 30 * time.Second

// myPRBranches gates --myprs on the forge hosting remote: only GitHub is
// implemented (via the `gh` CLI); other hosts error naming the remote and
// the supported set so users know why the flag refuses. GitHub detection
// comes from `git remote get-url`, never from config, so `-f` checkouts
// and per-project dashboard remotes each resolve correctly.
func myPRBranches(repoPath, remote string) ([]forge.PRBranch, error) {
	kind, err := forge.Detect(repoPath, remote)
	if err != nil {
		return nil, err
	}
	if kind != forge.KindGitHub {
		return nil, fmt.Errorf("--myprs supports GitHub remotes only (remote %q is %q; available forges: %v)",
			remote, kind, forge.Available())
	}
	f, err := forge.Resolve(kind)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), myprsTimeout)
	defer cancel()
	return f().MyPRBranches(ctx, repoPath, remote)
}

// dashboardBranchRefs lists refs for the dashboard branch pane honoring the
// mine/author/myprs filters. MyPRS applies last as an intersection, per
// project — non-GitHub projects error here so the pane shows why instead
// of silently unfiltered branches.
func dashboardBranchRefs(src source.Source, repoPath, remote string, mine bool, authors []string, myprs bool) ([]string, error) {
	var refs []string
	var err error
	if mine || len(authors) > 0 {
		refs, err = filterRefs(src, repoPath, remote, mine, authors)
	} else {
		refs, err = src.Refs(repoPath, remote)
	}
	if err != nil {
		return nil, err
	}
	if !myprs {
		return refs, nil
	}
	prs, err := myPRBranches(repoPath, remote)
	if err != nil {
		return nil, err
	}
	return intersectMyPRS(refs, prs), nil
}

// displayBranches lists branches for the dashboard branch pane. Unfiltered
// views union remote refs with local-only branches so a checkout can be
// created from either; filtered views (mine/author/myprs) stay remote-only
// since those filters are defined over remote tip metadata. Either way the
// result is ordered for the pane: prioritized branches first (open
// involving-me PRs when the forge answers, else tip-matching --mine
// branches; filtered views already are the priority set so they skip the
// lookup), then newest-first by tip committer date (git exposes no true
// branch creation date; local-only branches without a remote ref sort last
// alphabetically). All ordering inputs are best-effort and degrade to the
// underlying ref order, never an error.
func displayBranches(src source.Source, repoPath, remote string, mine bool, authors []string, myprs bool) ([]string, error) {
	refs, err := dashboardBranchRefs(src, repoPath, remote, mine, authors, myprs)
	if err != nil {
		return nil, err
	}
	filtered := mine || len(authors) > 0 || myprs
	if filtered {
		return orderDisplayBranches(src, repoPath, remote, refs, true), nil
	}
	local, err := src.LocalBranches(repoPath)
	if err != nil {
		return orderDisplayBranches(src, repoPath, remote, refs, false), nil
	}
	return orderDisplayBranches(src, repoPath, remote, unionBranches(refs, local), false), nil
}

// intersectMyPRS narrows refs to branches with an open involving-me PR.
// The PR list intersects the Source ref list (not the reverse), so
// fork-head PRs with no <remote>/<branch> ref drop out instead of
// failing `add`. Order follows refs (sorted upstream); PR metadata
// (number/title/url) is CLI-display only.
func intersectMyPRS(refs []string, prs []forge.PRBranch) []string {
	inPR := map[string]struct{}{}
	for _, pr := range prs {
		if pr.Branch != "" {
			inPR[pr.Branch] = struct{}{}
		}
	}
	var out []string
	for _, ref := range refs {
		if _, ok := inPR[ref]; ok {
			out = append(out, ref)
		}
	}
	return out
}
