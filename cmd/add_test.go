package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mytmlt/wrk3/internal/config"
	"github.com/mytmlt/wrk3/internal/ports"
	"github.com/mytmlt/wrk3/internal/source"
)

// localWorktreesByBranch must skip the main checkout even when git reports
// the resolved path (/private/var/...) while RepoPath keeps the logical
// symlinked form (/var/...), as on macOS with TMPDIR under /var.
func TestLocalWorktreesByBranch_SkipsMainThroughSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink behavior differs on windows")
	}
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestConfig(t, repo)
	link := filepath.Join(dir, "link")
	if err := os.Symlink(repo, link); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(filepath.Join(link, "wrk3.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	r := &resolved{cfg: cfg}
	infos := []source.WorktreeInfo{
		{Path: repo, Branch: "main"}, // git reports the resolved path
		{Path: filepath.Join(dir, "wt1"), Branch: "feature-foo"},
	}
	byBranch := localWorktreesByBranch(r, infos)
	if _, ok := byBranch["main"]; ok {
		t.Errorf("main checkout via symlink was not skipped: %v", byBranch)
	}
	if got := byBranch["feature-foo"]; got == "" {
		t.Errorf("feature-foo worktree missing: %v", byBranch)
	}
}

// adoptListStub serves a fixed worktree list for addOne/addOrAdoptOne tests.
type adoptListStub struct {
	stubSource
	infos []source.WorktreeInfo
}

func (s *adoptListStub) List(repoPath string) ([]source.WorktreeInfo, error) {
	return s.infos, nil
}

// addOrAdoptOne must adopt an existing on-disk worktree instead of
// re-creating it (orphan missing from state after state-file loss).
func TestAddOrAdoptOne_AdoptsOrphan(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	wt := filepath.Join(repo, ".worktrees", "feature-foo")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	r := &resolved{cfg: cfg, src: &adoptListStub{}, base: cfg.AbsWorktreeBase(), stateP: cfg.StatePath()}
	var recs []ports.WorktreeRecord
	alloc := cfg.Allocator()
	byBranch := map[string]string{"feature-foo": wt}
	warns, adopted, err := addOrAdoptOne(r, &recs, &alloc, byBranch, "feature-foo", "origin")
	if err != nil {
		t.Fatal(err)
	}
	if !adopted {
		t.Error("adopted = false, want true for existing worktree")
	}
	if len(warns) != 0 {
		t.Errorf("warns = %v, want none", warns)
	}
	if len(recs) != 1 || recs[0].Branch != "feature-foo" || recs[0].AbsPath != filepath.Clean(wt) {
		t.Errorf("recs = %+v, want adopted feature-foo at %s", recs, wt)
	}
	if recs[0].Index != 0 || recs[0].Ports["app"] != 8000 {
		t.Errorf("recs[0] = %+v, want index 0 app 8000", recs[0])
	}
}

// addOne must hint at adoption instead of surfacing a raw git failure
// when the branch is already checked out somewhere.
func TestAddOne_HintsAdoptWhenCheckedOut(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	r := &resolved{
		cfg:    cfg,
		src:    &adoptListStub{infos: []source.WorktreeInfo{{Path: "/elsewhere/wt", Branch: "feature-foo"}}},
		base:   cfg.AbsWorktreeBase(),
		stateP: cfg.StatePath(),
	}
	var recs []ports.WorktreeRecord
	alloc := cfg.Allocator()
	if _, err := addOne(r, &recs, &alloc, "feature-foo", "origin"); err == nil {
		t.Fatal("expected checked-out error")
	} else if !strings.Contains(err.Error(), "add --local") {
		t.Errorf("error should hint at add --local, got: %v", err)
	}
}
