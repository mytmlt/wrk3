package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/mytmlt/wrk3/internal/config"
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
