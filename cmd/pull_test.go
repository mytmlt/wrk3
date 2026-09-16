package cmd

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mytmlt/wrk3/internal/ports"
	"github.com/mytmlt/wrk3/internal/source"
)

// pullTestSource records Pull calls for pull-command tests.
type pullTestSource struct {
	stubSource
	mu        sync.Mutex
	pulled    []string
	pullOpts  []source.PullOptions
	pullErr   map[string]error
	defaultEr error
}

func (s *pullTestSource) Pull(worktreePath string, opts source.PullOptions) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pulled = append(s.pulled, worktreePath)
	s.pullOpts = append(s.pullOpts, opts)
	if s.pullErr != nil {
		if err, ok := s.pullErr[worktreePath]; ok {
			return err
		}
	}
	return s.defaultEr
}

func (s *pullTestSource) calls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.pulled...)
}

func newPullTestResolved(t *testing.T, src *pullTestSource) *resolved {
	t.Helper()
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	return &resolved{cfg: cfg, src: src, base: cfg.AbsWorktreeBase(), stateP: cfg.StatePath()}
}

func TestPullOne_SuccessLogs(t *testing.T) {
	src := &pullTestSource{}
	r := newPullTestResolved(t, src)
	dir := t.TempDir()
	rec := ports.WorktreeRecord{Branch: "feature-a", Slug: "feature-a", AbsPath: dir}
	var logs []string
	logf := func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }
	if err := pullOne(r, rec, source.PullOptions{}, logf); err != nil {
		t.Fatalf("pullOne = %v", err)
	}
	if got := src.calls(); len(got) != 1 || got[0] != dir {
		t.Errorf("pulled = %v, want [%s]", got, dir)
	}
	joined := strings.Join(logs, "\n")
	if !strings.Contains(joined, "[feature-a] pull") || !strings.Contains(joined, "[feature-a] pulled") {
		t.Errorf("logs missing pull lines: %v", logs)
	}
}

func TestPullOne_PassesOptions(t *testing.T) {
	src := &pullTestSource{}
	r := newPullTestResolved(t, src)
	dir := t.TempDir()
	rec := ports.WorktreeRecord{Branch: "a", Slug: "a", AbsPath: dir}
	if err := pullOne(r, rec, source.PullOptions{Rebase: true}, func(string, ...any) {}); err != nil {
		t.Fatal(err)
	}
	src.mu.Lock()
	defer src.mu.Unlock()
	if len(src.pullOpts) != 1 || !src.pullOpts[0].Rebase {
		t.Errorf("opts = %+v, want Rebase=true", src.pullOpts)
	}
}

func TestPullOne_StaleErrors(t *testing.T) {
	src := &pullTestSource{}
	r := newPullTestResolved(t, src)
	missing := filepath.Join(t.TempDir(), "gone")
	rec := ports.WorktreeRecord{Branch: "feature-a", Slug: "feature-a", AbsPath: missing}
	err := pullOne(r, rec, source.PullOptions{}, func(string, ...any) {})
	if err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("err = %v, want stale", err)
	}
	if got := src.calls(); len(got) != 0 {
		t.Errorf("stale pull must not call source: %v", got)
	}
}

func TestPullOne_PullErrorWrapsBranch(t *testing.T) {
	src := &pullTestSource{defaultEr: errors.New("boom")}
	r := newPullTestResolved(t, src)
	dir := t.TempDir()
	rec := ports.WorktreeRecord{Branch: "feature-a", Slug: "feature-a", AbsPath: dir}
	err := pullOne(r, rec, source.PullOptions{}, func(string, ...any) {})
	if err == nil || !strings.Contains(err.Error(), `"feature-a"`) || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v, want branch + boom", err)
	}
}

func TestRunPullTargets_ParallelJoinsErrors(t *testing.T) {
	dirOK := t.TempDir()
	dirFail := t.TempDir()
	src := &pullTestSource{pullErr: map[string]error{dirFail: errors.New("nope")}}
	r := newPullTestResolved(t, src)
	targets := []ports.WorktreeRecord{
		{Branch: "a", Slug: "a", AbsPath: dirOK},
		{Branch: "b", Slug: "b", AbsPath: dirFail},
	}
	var logs []string
	var mu sync.Mutex
	logf := func(f string, a ...any) {
		mu.Lock()
		defer mu.Unlock()
		logs = append(logs, fmt.Sprintf(f, a...))
	}
	err := runPullTargets(r, targets, source.PullOptions{}, logf)
	if err == nil || !strings.Contains(err.Error(), `"b"`) {
		t.Fatalf("err = %v, want failing branch b", err)
	}
	if got := src.calls(); len(got) != 2 {
		t.Errorf("both targets must run, got %v", got)
	}
	joined := strings.Join(logs, "\n")
	if !strings.Contains(joined, "[a] pulled") {
		t.Errorf("success target must still log pulled: %v", logs)
	}
}
