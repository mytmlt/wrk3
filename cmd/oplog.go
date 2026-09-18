package cmd

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mytmlt/wrk3/internal/ports"
	"github.com/mytmlt/wrk3/internal/runner"
	"github.com/mytmlt/wrk3/internal/source"
	"github.com/mytmlt/wrk3/internal/syslog"
)

// oplog persists one structured entry to the system log next to the state
// file (<worktreeBase>/.wrk3-log.jsonl). It is best-effort: failures are
// swallowed so logging never fails the operation it records. The dashboard
// keeps its brief in-memory log pane unchanged; everything that flows
// through here is the verbose counterpart (exact commands, cwd, duration,
// errors) for post-mortem debugging via `wrk3 log`.
func (r *resolved) oplog(e syslog.Entry) {
	if r == nil || r.stateP == "" {
		return
	}
	if e.Source == "" {
		e.Source = r.logSource
	}
	if e.Source == "" {
		e.Source = "cli"
	}
	_ = syslog.Append(r.stateP, e)
}

// logOpStart marks the beginning of a high-level operation (up/down/add/…)
// with its targets. Per-worktree command entries follow; logOpDone closes
// the span. Both are single brief lines in the persistent log.
func (r *resolved) logOpStart(op, msg string) {
	r.oplog(syslog.Entry{Level: syslog.LevelInfo, Op: op, Msg: msg})
}

// logOpDone marks the end of a high-level operation. A nil err logs info,
// otherwise error with the joined error text.
func (r *resolved) logOpDone(op, msg string, err error) {
	e := syslog.Entry{Level: syslog.LevelInfo, Op: op, Msg: msg}
	if err != nil {
		e.Level = syslog.LevelError
		e.Err = err.Error()
	}
	r.oplog(e)
}

// originOf returns the system log origin for r, defaulting to "cli".
func originOf(r *resolved) string {
	if r != nil && r.logSource != "" {
		return r.logSource
	}
	return "cli"
}

// wrapSource decorates src so every git command is persisted to the system
// log. Mutating commands (Fetch/Add/AddNew/Remove/Pull) log always;
// read-only probes (Refs/List/…) log only on error so dashboard polling
// does not flood the file.
func wrapSource(src source.Source, stateP, origin string) source.Source {
	if src == nil {
		return nil
	}
	return &loggedSource{inner: src, stateP: stateP, origin: origin}
}

// runnerFor builds the Runner backend for rec and decorates it so every
// executed command (entry strings via sh -c, compose up/down) is persisted
// to the system log. Status probes log only on error.
func (r *resolved) runnerFor(rec ports.WorktreeRecord) (runner.Runner, error) {
	raw, err := newRunner(r.cfg, rec.Slug)
	if err != nil {
		return nil, err
	}
	return &loggedRunner{
		inner:   raw,
		stateP:  r.stateP,
		origin:  r.logSource,
		branch:  rec.Branch,
		slug:    rec.Slug,
		project: rec.ComposeProject,
	}, nil
}

// logToSyslog appends e with origin defaulting, swallowing errors.
func logToSyslog(stateP, origin string, e syslog.Entry) {
	if stateP == "" {
		return
	}
	if e.Source == "" {
		e.Source = origin
	}
	if e.Source == "" {
		e.Source = "cli"
	}
	_ = syslog.Append(stateP, e)
}

// loggedRunner decorates a Runner with persistent system logging.
type loggedRunner struct {
	inner   runner.Runner
	stateP  string
	origin  string
	branch  string
	slug    string
	project string
}

var _ runner.Runner = (*loggedRunner)(nil)

func (l *loggedRunner) base() syslog.Entry {
	return syslog.Entry{Branch: l.branch, Slug: l.slug}
}

// Up runs compose up -d --build, persisting command, cwd, duration, error.
func (l *loggedRunner) Up(ctx context.Context, worktreePath string, env map[string]string) error {
	start := time.Now()
	err := l.inner.Up(ctx, worktreePath, env)
	e := l.base()
	e.Op = "compose-up"
	e.Cmd = fmt.Sprintf("compose up -d --build (project=%s)", l.project)
	e.Cwd = worktreePath
	e.DurationMs = time.Since(start).Milliseconds()
	if err != nil {
		e.Level = syslog.LevelError
		e.Err = err.Error()
		e.Msg = fmt.Sprintf("[%s] compose up failed", l.slug)
	} else {
		e.Level = syslog.LevelInfo
		e.Msg = fmt.Sprintf("[%s] compose up", l.slug)
	}
	logToSyslog(l.stateP, l.origin, e)
	return err
}

// Down runs compose down, persisting command, cwd, duration, error.
func (l *loggedRunner) Down(ctx context.Context, worktreePath string, env map[string]string) error {
	start := time.Now()
	err := l.inner.Down(ctx, worktreePath, env)
	e := l.base()
	e.Op = "compose-down"
	e.Cmd = fmt.Sprintf("compose down (project=%s)", l.project)
	e.Cwd = worktreePath
	e.DurationMs = time.Since(start).Milliseconds()
	if err != nil {
		e.Level = syslog.LevelError
		e.Err = err.Error()
		e.Msg = fmt.Sprintf("[%s] compose down failed", l.slug)
	} else {
		e.Level = syslog.LevelInfo
		e.Msg = fmt.Sprintf("[%s] compose down", l.slug)
	}
	logToSyslog(l.stateP, l.origin, e)
	return err
}

// Exec runs cmd as a host process, persisting the exact argv, cwd,
// duration and error. Entry strings arrive as ["sh" "-c" "<string>"].
func (l *loggedRunner) Exec(ctx context.Context, worktreePath string, cmd []string, env map[string]string) error {
	start := time.Now()
	err := l.inner.Exec(ctx, worktreePath, cmd, env)
	e := l.base()
	e.Op = "exec"
	e.Cmd = quoteArgv(cmd)
	e.Cwd = worktreePath
	e.DurationMs = time.Since(start).Milliseconds()
	if err != nil {
		e.Level = syslog.LevelError
		e.Err = err.Error()
		e.Msg = fmt.Sprintf("[%s] exec failed: %s", l.slug, quoteArgv(cmd))
	} else {
		e.Level = syslog.LevelInfo
		e.Msg = fmt.Sprintf("[%s] exec: %s", l.slug, quoteArgv(cmd))
	}
	logToSyslog(l.stateP, l.origin, e)
	return err
}

// quoteArgv renders argv for the log preserving boundaries: args with
// whitespace or quotes are double-quoted (strconv.Quote), bare words
// stay bare, so ["a b" "c"] and ["a" "b c"] never render identically.
func quoteArgv(cmd []string) string {
	out := make([]string, 0, len(cmd))
	for _, a := range cmd {
		if strings.ContainsAny(a, " \t\n\r\"'\\") {
			out = append(out, strconv.Quote(a))
		} else {
			out = append(out, a)
		}
	}
	return strings.Join(out, " ")
}

// Logs runs compose logs; user-initiated, so it logs always.
func (l *loggedRunner) Logs(ctx context.Context, worktreePath string, follow bool) (string, error) {
	start := time.Now()
	out, err := l.inner.Logs(ctx, worktreePath, follow)
	e := l.base()
	e.Op = "container-logs"
	e.Cmd = "compose logs"
	if follow {
		e.Cmd = "compose logs -f"
	}
	e.Cwd = worktreePath
	e.DurationMs = time.Since(start).Milliseconds()
	if err != nil {
		e.Level = syslog.LevelError
		e.Err = err.Error()
		e.Msg = fmt.Sprintf("[%s] logs failed", l.slug)
	} else {
		e.Level = syslog.LevelInfo
		e.Msg = fmt.Sprintf("[%s] logs", l.slug)
	}
	logToSyslog(l.stateP, l.origin, e)
	return out, err
}

// Status probes the runtime; read-only poll path, so it logs only errors.
func (l *loggedRunner) Status(ctx context.Context, worktreePath string) (runner.Status, error) {
	st, err := l.inner.Status(ctx, worktreePath)
	if err != nil {
		e := l.base()
		e.Level = syslog.LevelDebug
		e.Op = "status"
		e.Cmd = "compose ps -q"
		e.Cwd = worktreePath
		e.Err = err.Error()
		e.Msg = fmt.Sprintf("[%s] status probe failed", l.slug)
		logToSyslog(l.stateP, l.origin, e)
	}
	return st, err
}

// loggedSource decorates a Source with persistent system logging.
type loggedSource struct {
	inner  source.Source
	stateP string
	origin string
}

var _ source.Source = (*loggedSource)(nil)

func (l *loggedSource) logErr(op, msg, cwd, cmd string, err error) {
	if err == nil {
		return
	}
	logToSyslog(l.stateP, l.origin, syslog.Entry{
		Level: syslog.LevelDebug, Op: op, Msg: msg, Cwd: cwd, Cmd: cmd, Err: err.Error(),
	})
}

// Fetch runs git fetch <remote> --prune.
func (l *loggedSource) Fetch(repoPath, remote string) error {
	start := time.Now()
	err := l.inner.Fetch(repoPath, remote)
	e := syslog.Entry{
		Op: "git-fetch", Cwd: repoPath,
		Cmd:        fmt.Sprintf("git fetch %s --prune", remote),
		DurationMs: time.Since(start).Milliseconds(),
	}
	if err != nil {
		e.Level = syslog.LevelError
		e.Err = err.Error()
		e.Msg = fmt.Sprintf("fetch %s failed", remote)
	} else {
		e.Level = syslog.LevelInfo
		e.Msg = fmt.Sprintf("fetch %s", remote)
	}
	logToSyslog(l.stateP, l.origin, e)
	return err
}

// Add creates a worktree for branch, logging the semantic git command.
func (l *loggedSource) Add(repoPath, branch, worktreePath, remote string) error {
	start := time.Now()
	err := l.inner.Add(repoPath, branch, worktreePath, remote)
	e := syslog.Entry{
		Op: "git-add", Branch: branch, Cwd: repoPath,
		Cmd:        fmt.Sprintf("git worktree add %s %s (remote=%s)", branch, worktreePath, remote),
		DurationMs: time.Since(start).Milliseconds(),
	}
	if err != nil {
		e.Level = syslog.LevelError
		e.Err = err.Error()
		e.Msg = fmt.Sprintf("add worktree %s failed", branch)
	} else {
		e.Level = syslog.LevelInfo
		e.Msg = fmt.Sprintf("add worktree %s", branch)
	}
	logToSyslog(l.stateP, l.origin, e)
	return err
}

// AddNew creates a worktree with a new branch from base.
func (l *loggedSource) AddNew(repoPath, branch, worktreePath, base string) error {
	start := time.Now()
	err := l.inner.AddNew(repoPath, branch, worktreePath, base)
	e := syslog.Entry{
		Op: "git-add-new", Branch: branch, Cwd: repoPath,
		Cmd:        fmt.Sprintf("git worktree add -b %s %s %s", branch, worktreePath, base),
		DurationMs: time.Since(start).Milliseconds(),
	}
	if err != nil {
		e.Level = syslog.LevelError
		e.Err = err.Error()
		e.Msg = fmt.Sprintf("add new branch %s failed", branch)
	} else {
		e.Level = syslog.LevelInfo
		e.Msg = fmt.Sprintf("add new branch %s from %s", branch, base)
	}
	logToSyslog(l.stateP, l.origin, e)
	return err
}

// Remove deletes the worktree at worktreePath.
func (l *loggedSource) Remove(repoPath, worktreePath string, force bool) error {
	start := time.Now()
	err := l.inner.Remove(repoPath, worktreePath, force)
	cmd := fmt.Sprintf("git worktree remove %s", worktreePath)
	if force {
		cmd = fmt.Sprintf("git worktree remove --force %s", worktreePath)
	}
	e := syslog.Entry{
		Op: "git-remove", Cwd: repoPath, Cmd: cmd,
		DurationMs: time.Since(start).Milliseconds(),
	}
	if err != nil {
		e.Level = syslog.LevelError
		e.Err = err.Error()
		e.Msg = fmt.Sprintf("remove worktree %s failed", worktreePath)
	} else {
		e.Level = syslog.LevelInfo
		e.Msg = fmt.Sprintf("remove worktree %s", worktreePath)
	}
	logToSyslog(l.stateP, l.origin, e)
	return err
}

// Pull runs git pull inside the worktree.
func (l *loggedSource) Pull(worktreePath string, opts source.PullOptions) error {
	start := time.Now()
	err := l.inner.Pull(worktreePath, opts)
	cmd := "git pull"
	if opts.Rebase {
		cmd += " --rebase"
	}
	if opts.FFOnly {
		cmd += " --ff-only"
	}
	e := syslog.Entry{
		Op: "git-pull", Cwd: worktreePath, Cmd: cmd,
		DurationMs: time.Since(start).Milliseconds(),
	}
	if err != nil {
		e.Level = syslog.LevelError
		e.Err = err.Error()
		e.Msg = "pull failed"
	} else {
		e.Level = syslog.LevelInfo
		e.Msg = "pull"
	}
	logToSyslog(l.stateP, l.origin, e)
	return err
}

// Read-only probes log only on error (debug) so polling stays quiet.

func (l *loggedSource) Refs(repoPath, remote string) ([]string, error) {
	out, err := l.inner.Refs(repoPath, remote)
	l.logErr("git-refs", "list refs failed", repoPath, "git branch -r", err)
	return out, err
}

func (l *loggedSource) LocalBranches(repoPath string) ([]string, error) {
	out, err := l.inner.LocalBranches(repoPath)
	l.logErr("git-branches", "list local branches failed", repoPath, "git branch", err)
	return out, err
}

func (l *loggedSource) RefsDetailed(repoPath, remote string) ([]source.BranchRef, error) {
	out, err := l.inner.RefsDetailed(repoPath, remote)
	l.logErr("git-refs", "list detailed refs failed", repoPath, "git for-each-ref", err)
	return out, err
}

func (l *loggedSource) DefaultBranch(repoPath, remote string) (string, error) {
	out, err := l.inner.DefaultBranch(repoPath, remote)
	l.logErr("git-default-branch", "resolve default branch failed", repoPath, "git symbolic-ref", err)
	return out, err
}

func (l *loggedSource) BranchHistory(repoPath, remote, branch, base string, limit int) ([]source.BranchRef, error) {
	out, err := l.inner.BranchHistory(repoPath, remote, branch, base, limit)
	l.logErr("git-history", "branch history failed", repoPath, "git log", err)
	return out, err
}

func (l *loggedSource) Identity(repoPath string) (string, string, error) {
	name, email, err := l.inner.Identity(repoPath)
	l.logErr("git-identity", "read git identity failed", repoPath, "git config", err)
	return name, email, err
}

func (l *loggedSource) List(repoPath string) ([]source.WorktreeInfo, error) {
	out, err := l.inner.List(repoPath)
	l.logErr("git-list", "list worktrees failed", repoPath, "git worktree list", err)
	return out, err
}

// GitStatus probes git status inside the worktree (read-only: logs only
// on error so status polling stays quiet).
func (l *loggedSource) GitStatus(worktreePath string) (source.WorktreeStatus, error) {
	out, err := l.inner.GitStatus(worktreePath)
	l.logErr("git-status", "git status failed", worktreePath, "git status --porcelain=v1 -b", err)
	return out, err
}
