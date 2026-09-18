package cmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mytmlt/wrk3/internal/ports"
	"github.com/mytmlt/wrk3/internal/runner"
	"github.com/mytmlt/wrk3/internal/source"
	"github.com/mytmlt/wrk3/internal/syslog"
)

// stubRunner is a controllable Runner for oplog tests.
type stubRunner struct {
	execErr   error
	upErr     error
	downErr   error
	statusErr error
	execCmds  [][]string
}

func (s *stubRunner) Up(ctx context.Context, worktreePath string, env map[string]string) error {
	return s.upErr
}
func (s *stubRunner) Down(ctx context.Context, worktreePath string, env map[string]string) error {
	return s.downErr
}
func (s *stubRunner) Logs(ctx context.Context, worktreePath string, follow bool) (string, error) {
	return "logs-output", nil
}
func (s *stubRunner) Exec(ctx context.Context, worktreePath string, cmd []string, env map[string]string) error {
	s.execCmds = append(s.execCmds, cmd)
	return s.execErr
}
func (s *stubRunner) Status(ctx context.Context, worktreePath string) (runner.Status, error) {
	if s.statusErr != nil {
		return runner.Status{State: runner.StateUnknown}, s.statusErr
	}
	return runner.Status{State: runner.StateStopped}, nil
}

func oplogStateP(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), ".wrk3-state.json")
}

func readOps(t *testing.T, stateP string) []syslog.Entry {
	t.Helper()
	entries, err := syslog.ReadLast(syslog.LogPath(stateP), 100)
	if err != nil {
		t.Fatalf("ReadLast: %v", err)
	}
	return entries
}

func TestLoggedRunnerExecPersistsCmd(t *testing.T) {
	stateP := oplogStateP(t)
	l := &loggedRunner{inner: &stubRunner{}, stateP: stateP, origin: "cli", branch: "feat/a", slug: "feat-a", project: "p-feat-a"}
	if err := l.Exec(context.Background(), "/tmp/wt", []string{"sh", "-c", "make test"}, nil); err != nil {
		t.Fatalf("Exec: %v", err)
	}
	entries := readOps(t, stateP)
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	e := entries[0]
	if e.Op != "exec" || e.Cmd != `sh -c "make test"` || e.Cwd != "/tmp/wt" {
		t.Fatalf("entry = %+v", e)
	}
	if e.Branch != "feat/a" || e.Slug != "feat-a" || e.Source != "cli" {
		t.Fatalf("context = %+v", e)
	}
	if e.Level != syslog.LevelInfo || e.Err != "" {
		t.Fatalf("level/err = %+v", e)
	}
}

func TestLoggedRunnerExecErrorPersists(t *testing.T) {
	stateP := oplogStateP(t)
	boom := errors.New("exit 1")
	l := &loggedRunner{inner: &stubRunner{execErr: boom}, stateP: stateP, branch: "b", slug: "b"}
	if err := l.Exec(context.Background(), "/tmp/wt", []string{"sh", "-c", "false"}, nil); err == nil {
		t.Fatalf("expected error")
	}
	entries := readOps(t, stateP)
	if len(entries) != 1 || entries[0].Level != syslog.LevelError {
		t.Fatalf("entries = %+v", entries)
	}
	if !strings.Contains(entries[0].Err, "exit 1") {
		t.Fatalf("err text = %q", entries[0].Err)
	}
}

func TestLoggedRunnerStatusQuietOnSuccess(t *testing.T) {
	stateP := oplogStateP(t)
	l := &loggedRunner{inner: &stubRunner{}, stateP: stateP, branch: "b", slug: "b"}
	if _, err := l.Status(context.Background(), "/tmp/wt"); err != nil {
		t.Fatalf("Status: %v", err)
	}
	if _, err := os.Stat(syslog.LogPath(stateP)); !os.IsNotExist(err) {
		t.Fatalf("successful probe must not write the log")
	}
}

func TestLoggedRunnerStatusErrorPersistsDebug(t *testing.T) {
	stateP := oplogStateP(t)
	l := &loggedRunner{inner: &stubRunner{statusErr: errors.New("daemon down")}, stateP: stateP, branch: "b", slug: "b"}
	if _, err := l.Status(context.Background(), "/tmp/wt"); err == nil {
		t.Fatalf("expected error")
	}
	entries := readOps(t, stateP)
	if len(entries) != 1 || entries[0].Op != "status" || entries[0].Level != syslog.LevelDebug {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestLoggedRunnerUpDownPersist(t *testing.T) {
	stateP := oplogStateP(t)
	l := &loggedRunner{inner: &stubRunner{}, stateP: stateP, branch: "b", slug: "b", project: "p-b"}
	if err := l.Up(context.Background(), "/tmp/wt", nil); err != nil {
		t.Fatalf("Up: %v", err)
	}
	if err := l.Down(context.Background(), "/tmp/wt", nil); err != nil {
		t.Fatalf("Down: %v", err)
	}
	entries := readOps(t, stateP)
	if len(entries) != 2 || entries[0].Op != "compose-up" || entries[1].Op != "compose-down" {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestLoggedSourceFetchPullPersist(t *testing.T) {
	stateP := oplogStateP(t)
	l := wrapSource(&stubSource{}, stateP, "dashboard")
	if err := l.Fetch("/repo", "origin"); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if err := l.Pull("/repo/wt", source.PullOptions{Rebase: true}); err != nil {
		t.Fatalf("Pull: %v", err)
	}
	entries := readOps(t, stateP)
	if len(entries) != 2 {
		t.Fatalf("entries = %+v", entries)
	}
	if entries[0].Op != "git-fetch" || entries[0].Source != "dashboard" {
		t.Fatalf("fetch entry = %+v", entries[0])
	}
	if entries[1].Op != "git-pull" || entries[1].Cmd != "git pull --rebase" {
		t.Fatalf("pull entry = %+v", entries[1])
	}
}

func TestLoggedSourceProbesQuietOnSuccess(t *testing.T) {
	stateP := oplogStateP(t)
	l := wrapSource(&stubSource{refs: []string{"a"}}, stateP, "cli")
	if _, err := l.Refs("/repo", "origin"); err != nil {
		t.Fatalf("Refs: %v", err)
	}
	if _, err := l.List("/repo"); err != nil {
		t.Fatalf("List: %v", err)
	}
	if _, err := os.Stat(syslog.LogPath(stateP)); !os.IsNotExist(err) {
		t.Fatalf("successful probes must not write the log")
	}
}

func TestWrapSourceNil(t *testing.T) {
	if wrapSource(nil, "/tmp/state.json", "cli") != nil {
		t.Fatalf("wrapSource(nil) must stay nil")
	}
}

func TestOplogNilSafe(t *testing.T) {
	var r *resolved
	r.oplog(syslog.Entry{Op: "up"})
	(&resolved{}).oplog(syslog.Entry{Op: "up"})
}

func TestRunPullTargetsLogsOpSpan(t *testing.T) {
	dir := t.TempDir()
	stateP := filepath.Join(dir, ".wrk3-state.json")
	r := &resolved{src: wrapSource(&stubSource{}, stateP, "cli"), stateP: stateP, logSource: "cli"}
	targets := []ports.WorktreeRecord{{Branch: "a", Slug: "a", AbsPath: dir}}
	var lines []string
	logf := func(format string, a ...any) { lines = append(lines, "x") }
	if err := runPullTargets(r, targets, source.PullOptions{}, logf); err != nil {
		t.Fatalf("runPullTargets: %v", err)
	}
	entries := readOps(t, stateP)
	if len(entries) != 3 {
		t.Fatalf("entries = %+v", entries)
	}
	if entries[0].Op != "pull" || !strings.HasPrefix(entries[0].Msg, "pull ") {
		t.Fatalf("start = %+v", entries[0])
	}
	if entries[1].Op != "git-pull" {
		t.Fatalf("middle = %+v", entries[1])
	}
	if entries[2].Op != "pull" || entries[2].Level != syslog.LevelInfo {
		t.Fatalf("done = %+v", entries[2])
	}
}

func TestQuoteArgvPreservesBoundaries(t *testing.T) {
	a := quoteArgv([]string{"a b", "c"})
	b := quoteArgv([]string{"a", "b c"})
	if a == b {
		t.Fatalf("ambiguous rendering: %q", a)
	}
	if quoteArgv([]string{"sh", "-c", "make test"}) != `sh -c "make test"` {
		t.Fatalf("got %q", quoteArgv([]string{"sh", "-c", "make test"}))
	}
	if quoteArgv([]string{"go", "test"}) != "go test" {
		t.Fatalf("bare words must stay bare: %q", quoteArgv([]string{"go", "test"}))
	}
}

func TestFormatSyslogEntry(t *testing.T) {
	brief := formatSyslogEntry(syslog.Entry{Op: "up", Branch: "a", Msg: "up a"})
	if !strings.Contains(brief, "[up]") || !strings.Contains(brief, "up a") {
		t.Fatalf("brief = %q", brief)
	}
	if strings.Contains(brief, "cmd=") {
		t.Fatalf("brief must omit empty fields: %q", brief)
	}
	full := formatSyslogEntry(syslog.Entry{Op: "exec", Branch: "a", Msg: "m", Cmd: "sh -c make", Cwd: "/w", DurationMs: 7, Err: "boom"})
	for _, want := range []string{"cmd=sh -c make", "cwd=/w", "dur=7ms", "err=boom"} {
		if !strings.Contains(full, want) {
			t.Fatalf("full missing %q: %q", want, full)
		}
	}
}
