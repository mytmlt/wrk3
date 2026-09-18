package cmd

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mytmlt/wrk3/internal/ports"
	"github.com/mytmlt/wrk3/internal/source"
)

func newGitStatusTestResolved(t *testing.T, src *stubSource) *resolved {
	t.Helper()
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	return &resolved{cfg: cfg, src: src, base: cfg.AbsWorktreeBase(), stateP: cfg.StatePath()}
}

func TestGitStatusOne_Success(t *testing.T) {
	dir := t.TempDir()
	src := &stubSource{gitStatus: map[string]source.WorktreeStatus{
		dir: {Branch: "main", Clean: false, Staged: 1, Files: []string{"M  a.txt"}},
	}}
	r := newGitStatusTestResolved(t, src)
	rec := ports.WorktreeRecord{Branch: "main", Slug: "main", AbsPath: dir}
	res := gitStatusOne(r, rec)
	if res.statErr != nil {
		t.Fatalf("statErr = %v", res.statErr)
	}
	if res.status.Staged != 1 || res.status.Clean {
		t.Errorf("status = %+v, want staged 1 dirty", res.status)
	}
}

func TestGitStatusOne_StaleErrors(t *testing.T) {
	src := &stubSource{}
	r := newGitStatusTestResolved(t, src)
	missing := filepath.Join(t.TempDir(), "gone")
	rec := ports.WorktreeRecord{Branch: "feature-a", Slug: "feature-a", AbsPath: missing}
	res := gitStatusOne(r, rec)
	if res.statErr == nil || !strings.Contains(res.statErr.Error(), "stale") {
		t.Fatalf("err = %v, want stale", res.statErr)
	}
}

func TestGitStatusOne_GitErrorWrapsBranch(t *testing.T) {
	dir := t.TempDir()
	src := &stubSource{gitErr: map[string]error{dir: errors.New("boom")}}
	r := newGitStatusTestResolved(t, src)
	rec := ports.WorktreeRecord{Branch: "feature-a", Slug: "feature-a", AbsPath: dir}
	res := gitStatusOne(r, rec)
	if res.statErr == nil || !strings.Contains(res.statErr.Error(), `"feature-a"`) || !strings.Contains(res.statErr.Error(), "boom") {
		t.Fatalf("err = %v, want branch + boom", res.statErr)
	}
}

func TestRunGitStatusTargets_PreservesOrderAndRunsAll(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	src := &stubSource{
		gitStatus: map[string]source.WorktreeStatus{
			dirA: {Branch: "a", Clean: true},
		},
		gitErr: map[string]error{dirB: errors.New("nope")},
	}
	r := newGitStatusTestResolved(t, src)
	targets := []ports.WorktreeRecord{
		{Branch: "a", Slug: "a", AbsPath: dirA},
		{Branch: "b", Slug: "b", AbsPath: dirB},
	}
	results := runGitStatusTargets(r, targets)
	if len(results) != 2 {
		t.Fatalf("got %d results", len(results))
	}
	if results[0].statErr != nil || results[0].rec.Branch != "a" {
		t.Errorf("first result wrong: %+v", results[0])
	}
	if results[1].statErr == nil || !strings.Contains(results[1].statErr.Error(), `"b"`) {
		t.Errorf("second should carry branch b error: %+v", results[1])
	}
	if err := joinGitStatusErrors(results); err == nil || !strings.Contains(err.Error(), `"b"`) {
		t.Errorf("joined err = %v, want branch b", err)
	}
}

func TestJoinGitStatusErrors_NilWhenClean(t *testing.T) {
	results := []gitStatusResult{
		{rec: ports.WorktreeRecord{Branch: "a"}, status: source.WorktreeStatus{Clean: true}},
	}
	if err := joinGitStatusErrors(results); err != nil {
		t.Errorf("err = %v, want nil", err)
	}
}

func TestGitStatus_RealRepoEndToEnd(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	r := &resolved{cfg: cfg, src: &source.GitSource{}, base: cfg.AbsWorktreeBase(), stateP: cfg.StatePath()}
	rec := ports.WorktreeRecord{Branch: "main", Slug: "main", AbsPath: repo}
	res := gitStatusOne(r, rec)
	if res.statErr != nil {
		t.Fatalf("statErr = %v", res.statErr)
	}
	// writeTestConfig drops an untracked wrk3.yaml into the fresh repo,
	// so the checkout reports one untracked file (never clean here).
	if res.status.Branch != "main" {
		t.Errorf("Branch = %q, want main: %+v", res.status.Branch, res.status)
	}
	if res.status.Untracked != 1 {
		t.Errorf("Untracked = %d, want 1 (wrk3.yaml): %+v", res.status.Untracked, res.status)
	}
}
