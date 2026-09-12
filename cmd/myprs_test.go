package cmd

import (
	"os/exec"
	"testing"

	"github.com/mytmlt/wrk3/internal/forge"
)

// initRepoWithRemote creates a temp git repo with remote pointing at url
// (no network: Detect only reads `git remote get-url`).
func initRepoWithRemote(t *testing.T, url string) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", dir},
		{"-C", dir, "remote", "add", "origin", url},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	return dir
}

func TestIntersectMyPRS(t *testing.T) {
	refs := []string{"a", "b", "c"}
	prs := []forge.PRBranch{
		{Branch: "b", Number: 2},
		{Branch: "c", Number: 3},
		{Branch: "fork-only", Number: 9},
	}
	got := intersectMyPRS(refs, prs)
	if len(got) != 2 || got[0] != "b" || got[1] != "c" {
		t.Fatalf("got %v, want [b c] in refs order (fork-only drops out)", got)
	}
	if got := intersectMyPRS(refs, nil); len(got) != 0 {
		t.Fatalf("empty PRs => empty, got %v", got)
	}
}

func TestMyPRBranchesNonGitHubGate(t *testing.T) {
	dir := initRepoWithRemote(t, "git@gitlab.example.com:owner/repo.git")
	_, err := myPRBranches(dir, "origin")
	if err == nil {
		t.Fatal("non-GitHub remote: want gate error")
	}
	want := "--myprs supports GitHub remotes only"
	if got := err.Error(); len(got) < len(want) || got[:len(want)] != want {
		t.Fatalf("err = %q, want prefix %q", got, want)
	}
}

func TestDashboardBranchRefsNoMyPRS(t *testing.T) {
	s := &stubSource{refs: []string{"a", "b"}}
	got, err := dashboardBranchRefs(s, ".", "origin", false, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %v, want unfiltered refs", got)
	}
}

func TestDashboardBranchRefsMyPRSNonGitHub(t *testing.T) {
	dir := initRepoWithRemote(t, "https://example.com/owner/repo.git")
	s := &stubSource{refs: []string{"a"}}
	if _, err := dashboardBranchRefs(s, dir, "origin", false, nil, true); err == nil {
		t.Fatal("non-GitHub project with myprs: want gate error for the pane")
	}
}
