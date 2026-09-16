package cmd

import (
	"sort"
	"time"

	"github.com/mytmlt/wrk3/internal/forge"
	"github.com/mytmlt/wrk3/internal/source"
)

// dashboardMyPRs lists open involving-me PR branches for the dashboard's
// priority ordering. It is a variable so tests can stub the `gh` network
// call; production code always uses myPRBranches (GitHub-only, best-effort,
// callers fall back to --mine matching when it errors).
var dashboardMyPRs = myPRBranches

// tipMineSet matches detailed remote refs against the local git identity
// tip-only (no history scan, so it stays cheap enough for the dashboard
// poll path). It mirrors the --mine fast path in matchesIdentity.
func tipMineSet(detailed []source.BranchRef, name, email string) map[string]bool {
	if name == "" && email == "" {
		return nil
	}
	out := map[string]bool{}
	for _, ref := range detailed {
		if matchesIdentity(ref, name, email) {
			out[ref.Name] = true
		}
	}
	return out
}

// dashboardPrioritySet resolves which displayed branches sort first: open
// involving-me PR branches when the forge answers (GitHub + `gh`), else
// tip-matching --mine branches. Only names present in displayed are kept.
// Empty (nil) means no prioritization — date order applies to everything.
// Detailed is the already-fetched RefsDetailed result so the mine fallback
// costs only the Identity lookup (tip-only matching, no history scan).
func dashboardPrioritySet(src source.Source, repoPath, remote string, displayed []string, detailed []source.BranchRef) map[string]bool {
	inDisplayed := make(map[string]bool, len(displayed))
	for _, b := range displayed {
		inDisplayed[b] = true
	}
	if prs, err := dashboardMyPRs(repoPath, remote); err == nil && len(prs) > 0 {
		out := map[string]bool{}
		for _, pr := range prs {
			if pr.Branch != "" && inDisplayed[pr.Branch] {
				out[pr.Branch] = true
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	name, email, err := src.Identity(repoPath)
	if err != nil || (name == "" && email == "") {
		return nil
	}
	prioritized := map[string]bool{}
	for branch := range tipMineSet(detailed, name, email) {
		if inDisplayed[branch] {
			prioritized[branch] = true
		}
	}
	if len(prioritized) == 0 {
		return nil
	}
	return prioritized
}

// orderBranches sorts branch names for the dashboard pane: prioritized
// (my PR, else mine) branches first, then everything else. Each group is
// newest-first by tip committer date, alphabetical on ties; branches with
// unknown dates (zero, e.g. local-only) sort last within their group.
func orderBranches(names []string, dates map[string]time.Time, prioritized map[string]bool) []string {
	out := append([]string(nil), names...)
	sort.Slice(out, func(i, j int) bool {
		pi, pj := prioritized[out[i]], prioritized[out[j]]
		if pi != pj {
			return pi
		}
		di, dj := dates[out[i]], dates[out[j]]
		if !di.Equal(dj) {
			return di.After(dj)
		}
		return out[i] < out[j]
	})
	return out
}

// orderDisplayBranches applies the dashboard branch-pane ordering to an
// already-filtered branch list: priority set (my PRs, else mine) first,
// then newest-first by tip committer date. All data sources are
// best-effort — failures degrade to the input order, never an error.
// Filtered views (mine/author/myprs flags on) skip the priority lookup:
// the displayed list already is the priority set, so date order applies.
func orderDisplayBranches(src source.Source, repoPath, remote string, names []string, filtered bool) []string {
	if len(names) < 2 {
		return names
	}
	// One for-each-ref feeds both the recency map and the tip-only mine
	// fallback, so the poll path costs at most one extra git call plus
	// the `gh` PR attempt on unfiltered views.
	detailed, err := src.RefsDetailed(repoPath, remote)
	if err != nil {
		return names
	}
	dates := make(map[string]time.Time, len(detailed))
	for _, ref := range detailed {
		if ref.Name == "" || ref.CommitterDate.IsZero() {
			continue
		}
		dates[ref.Name] = ref.CommitterDate
	}
	var prioritized map[string]bool
	if !filtered {
		prioritized = dashboardPrioritySet(src, repoPath, remote, names, detailed)
	}
	// Without dates and without priorities there is nothing to reorder
	// (keeps stub/legacy sources byte-identical to input order).
	if len(dates) == 0 && len(prioritized) == 0 {
		return names
	}
	return orderBranches(names, dates, prioritized)
}

// forgePRBranches adapts []forge.PRBranch for tests of the priority path.
func forgePRBranches(branches ...string) []forge.PRBranch {
	out := make([]forge.PRBranch, 0, len(branches))
	for _, b := range branches {
		out = append(out, forge.PRBranch{Branch: b})
	}
	return out
}
