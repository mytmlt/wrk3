package cmd

import (
	"errors"
	"testing"
	"time"

	"github.com/mytmlt/wrk3/internal/forge"
	"github.com/mytmlt/wrk3/internal/source"
)

// stubPRs swaps the `gh` PR lookup for the duration of a test.
func stubPRs(t *testing.T, prs []forge.PRBranch, err error) {
	t.Helper()
	old := dashboardMyPRs
	t.Cleanup(func() { dashboardMyPRs = old })
	dashboardMyPRs = func(repoPath, remote string) ([]forge.PRBranch, error) {
		return prs, err
	}
}

func dateFixture() (old, mid, new time.Time) {
	old = time.Unix(1700000000, 0)
	mid = time.Unix(1750000000, 0)
	new = time.Unix(1780000000, 0)
	return old, mid, new
}

func TestOrderBranches_PriorityFirstThenNewest(t *testing.T) {
	old, mid, new := dateFixture()
	dates := map[string]time.Time{"a": old, "b": new, "c": mid, "mine-old": old}
	got := orderBranches(
		[]string{"a", "b", "c", "mine-old"},
		dates,
		map[string]bool{"mine-old": true},
	)
	// Prioritized branch first even with the oldest date; the rest newest-first.
	want := []string{"mine-old", "b", "c", "a"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestOrderBranches_UnknownDatesLastAlphabetical(t *testing.T) {
	_, _, new := dateFixture()
	dates := map[string]time.Time{"b": new}
	got := orderBranches([]string{"c", "a", "b", "d"}, dates, nil)
	// Dated branch first, dateless (e.g. local-only) last alphabetically.
	want := []string{"b", "a", "c", "d"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestOrderBranches_PriorityGroupSortsNewestFirst(t *testing.T) {
	old, mid, new := dateFixture()
	dates := map[string]time.Time{"p1": old, "p2": new, "p3": mid, "other": new}
	got := orderBranches(
		[]string{"other", "p1", "p2", "p3"},
		dates,
		map[string]bool{"p1": true, "p2": true, "p3": true},
	)
	want := []string{"p2", "p3", "p1", "other"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestDashboardPrioritySet_PrefersPRsOverMine(t *testing.T) {
	stubPRs(t, forgePRBranches("b", "fork-only"), nil)
	s := &stubSource{
		name:  "Alice",
		email: "alice@example.com",
		detailed: []source.BranchRef{
			{Name: "a", AuthorName: "Alice", AuthorEmail: "alice@example.com"},
		},
	}
	got := dashboardPrioritySet(s, ".", "origin", []string{"a", "b", "c"}, s.detailed)
	// PR branch b wins even though a matches --mine; fork-only PRs with
	// no displayed ref drop out.
	if len(got) != 1 || !got["b"] {
		t.Fatalf("got %v, want only {b}", got)
	}
}

func TestDashboardPrioritySet_FallsBackToMine(t *testing.T) {
	stubPRs(t, nil, errors.New("no gh"))
	s := &stubSource{
		name:  "Alice",
		email: "alice@example.com",
		detailed: []source.BranchRef{
			{Name: "a", AuthorName: "Alice", AuthorEmail: "alice@example.com"},
			{Name: "b", AuthorName: "Bob", AuthorEmail: "bob@example.com"},
		},
	}
	got := dashboardPrioritySet(s, ".", "origin", []string{"a", "b"}, s.detailed)
	if len(got) != 1 || !got["a"] {
		t.Fatalf("got %v, want only {a}", got)
	}
}

func TestDashboardPrioritySet_NoIdentityNoPriority(t *testing.T) {
	stubPRs(t, nil, errors.New("no gh"))
	s := &stubSource{detailed: detailedFixture()}
	if got := dashboardPrioritySet(s, ".", "origin", []string{"a"}, s.detailed); len(got) != 0 {
		t.Fatalf("got %v, want empty (no git identity)", got)
	}
}

func TestDisplayBranches_UnfilteredOrdersMineFirstByDate(t *testing.T) {
	stubPRs(t, nil, errors.New("no gh"))
	old, mid, new := dateFixture()
	s := &stubSource{
		refs:  []string{"old-other", "new-other"},
		local: []string{},
		name:  "Alice",
		email: "alice@example.com",
		detailed: []source.BranchRef{
			{Name: "old-other", CommitterDate: old},
			{Name: "new-other", CommitterDate: new},
			{Name: "my-feat", AuthorName: "Alice", AuthorEmail: "alice@example.com", CommitterDate: mid},
		},
	}
	// Refs only carry two branches; add the mine branch to refs so the
	// union contains all three.
	s.refs = []string{"old-other", "new-other", "my-feat"}
	got, err := displayBranches(s, ".", "origin", false, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	// Mine branch first despite the middle date, then newest-first.
	want := []string{"my-feat", "new-other", "old-other"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestDisplayBranches_FilteredSortsByDateSkippingPRLookup(t *testing.T) {
	called := false
	old := dashboardMyPRs
	t.Cleanup(func() { dashboardMyPRs = old })
	dashboardMyPRs = func(repoPath, remote string) ([]forge.PRBranch, error) {
		called = true
		return nil, errors.New("must not be called for filtered views")
	}
	oldT, _, newT := dateFixture()
	s := &stubSource{
		refs: []string{"older", "newer"},
		detailed: []source.BranchRef{
			{Name: "older", AuthorName: "T", AuthorEmail: "t@t", CommitterDate: oldT},
			{Name: "newer", AuthorName: "T", AuthorEmail: "t@t", CommitterDate: newT},
		},
		name: "t", email: "t@t",
	}
	got, err := displayBranches(s, ".", "origin", true, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Error("filtered view must skip the PR lookup (it already is the priority set)")
	}
	if len(got) != 2 || got[0] != "newer" || got[1] != "older" {
		t.Errorf("got %v, want [newer older] newest-first", got)
	}
}

func TestDisplayBranches_LocalOnlyBranchesSortLast(t *testing.T) {
	stubPRs(t, nil, errors.New("no gh"))
	_, _, new := dateFixture()
	s := &stubSource{
		refs:  []string{"remote"},
		local: []string{"aaa-local-only", "remote"},
		detailed: []source.BranchRef{
			{Name: "remote", CommitterDate: new},
		},
	}
	got, err := displayBranches(s, ".", "origin", false, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	// Dated remote ref first; dateless local-only branch last even though
	// alphabetically earlier.
	if len(got) != 2 || got[0] != "remote" || got[1] != "aaa-local-only" {
		t.Errorf("got %v, want [remote aaa-local-only]", got)
	}
}
