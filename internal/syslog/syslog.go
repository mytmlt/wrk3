// Package syslog persists a JSONL operation log next to the state file.
//
// The dashboard keeps a brief in-memory log (last 200 lines for the log
// pane). The system log records everything that happens — every external
// command run (entry strings via sh -c, compose up/down, git fetch/pull/
// worktree add/remove), state transitions, warnings and errors — with
// timestamps, op/branch context, cwd, duration and error text.
//
// The log lives at <worktreeBase>/.wrk3-log.jsonl, next to
// <worktreeBase>/.wrk3-state.json, so deleting the worktree base removes
// both. Writes are best-effort and append-only (one JSON object per
// line); logging never fails the operation it records. Read-only probes
// (runner Status, git List/Refs) are logged only on error so background
// polling does not flood the file.
//
// Concurrency: Append serializes in-process writers with a mutex so
// parallel ops (and background dashboard probes) cannot interleave the
// check-rotate-write sequence. Cross-process appends (CLI vs dashboard)
// rely on O_APPEND single-write atomicity and stay best-effort: rotation
// may race there, never the operation.
package syslog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
	"unicode/utf8"
)

// LogFileName is the system log basename under worktreeBase, next to the
// state file (see internal/ports StateFileName).
const LogFileName = ".wrk3-log.jsonl"

// MaxLogBytes caps the log file size. On append past the cap the file is
// rotated to LogFileName+".1" (single generation) and started fresh, so
// the log stays bounded without losing the most recent history.
const MaxLogBytes = 5 << 20 // 5 MiB

// Field caps keep single entries small: runner/git errors embed command
// stderr, which can run to megabytes on a failed compose build. Oversized
// values are truncated with a marker on write; oversized lines still on
// disk (written before this cap existed, or by hand) are skipped on read.
const (
	maxCmdBytes  = 16 << 10 // 16 KiB
	maxErrBytes  = 64 << 10 // 64 KiB
	maxLineBytes = 8 << 20  // 8 MiB
)

// Levels for Entry.Level.
const (
	LevelInfo  = "info"
	LevelWarn  = "warn"
	LevelError = "error"
	LevelDebug = "debug"
)

// Entry is one persisted log line.
type Entry struct {
	Time       time.Time `json:"ts"`
	Level      string    `json:"level,omitempty"`
	Source     string    `json:"source,omitempty"`
	Op         string    `json:"op,omitempty"`
	Branch     string    `json:"branch,omitempty"`
	Slug       string    `json:"slug,omitempty"`
	Msg        string    `json:"msg,omitempty"`
	Cmd        string    `json:"cmd,omitempty"`
	Cwd        string    `json:"cwd,omitempty"`
	Err        string    `json:"err,omitempty"`
	DurationMs int64     `json:"duration_ms,omitempty"`
}

// LogPath returns the system log path sibling to the state file at
// statePath (<worktreeBase>/.wrk3-state.json). Empty statePath yields "".
func LogPath(statePath string) string {
	if statePath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(statePath), LogFileName)
}

// appendMu serializes in-process writers across the check-rotate-write
// sequence (see package doc).
var appendMu sync.Mutex

// Append records one entry to the log sibling to statePath. It is
// best-effort by convention: callers ignore the error so logging never
// fails the operation. A zero Time is set to now (UTC).
func Append(statePath string, e Entry) error {
	appendMu.Lock()
	defer appendMu.Unlock()
	logPath := LogPath(statePath)
	if logPath == "" {
		return fmt.Errorf("syslog: empty state path")
	}
	if e.Time.IsZero() {
		e.Time = time.Now().UTC()
	}
	e.Cmd = truncate(e.Cmd, maxCmdBytes)
	e.Err = truncate(e.Err, maxErrBytes)
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return fmt.Errorf("syslog: create log dir: %w", err)
	}
	rotateIfFull(logPath)
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("syslog: open log: %w", err)
	}
	defer func() { _ = f.Close() }()
	raw, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("syslog: encode entry: %w", err)
	}
	raw = append(raw, '\n')
	if _, err := f.Write(raw); err != nil {
		return fmt.Errorf("syslog: write entry: %w", err)
	}
	return nil
}

// truncate caps s at max bytes, marking the cut so readers know output
// was dropped. The cut backs off to a rune boundary so the marker never
// corrupts a trailing multi-byte character. Short strings pass through
// unchanged.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := s[:max]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut + "…[truncated]"
}

// rotateIfFull renames the log to LogFileName+".1" when it exceeds
// MaxLogBytes, starting the next append fresh. Failures are ignored:
// rotation is best-effort, the append below still proceeds.
func rotateIfFull(logPath string) {
	st, err := os.Stat(logPath)
	if err != nil || st.Size() <= MaxLogBytes {
		return
	}
	_ = os.Rename(logPath, logPath+".1")
}

// ReadLast returns up to n most recent entries from logPath (oldest
// first), including the rotated backup (logPath+".1") that rotation
// leaves behind: the backup's tail is prepended so a just-rotated log
// still shows continuous history. Malformed lines are skipped. A missing
// file yields no entries.
func ReadLast(logPath string, n int) ([]Entry, error) {
	if n <= 0 {
		return nil, nil
	}
	backup, err := readFile(logPath+".1", n)
	if err != nil {
		// The backup is best-effort: a stale unreadable .1 must not
		// hide the readable current log.
		backup = nil
	}
	cur, err := readFile(logPath, n)
	if err != nil {
		return nil, err
	}
	all := append(backup, cur...)
	if len(all) > n {
		all = all[len(all)-n:]
	}
	return all, nil
}

// readFile returns up to n most recent entries from one file, scanning
// newest-first so a huge file never materializes more than n entries.
// Lines over maxLineBytes are skipped (a single verbose failure must not
// blank the whole view); anything else malformed is skipped too.
func readFile(logPath string, n int) ([]Entry, error) {
	if n <= 0 {
		return nil, nil
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("syslog: read log %q: %w", logPath, err)
	}
	lines := bytes.Split(raw, []byte{'\n'})
	var rev []Entry
	for i := len(lines) - 1; i >= 0 && len(rev) < n; i-- {
		line := lines[i]
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		if len(line) > maxLineBytes {
			continue
		}
		var e Entry
		if err := json.Unmarshal(line, &e); err != nil {
			continue
		}
		rev = append(rev, e)
	}
	out := make([]Entry, len(rev))
	for i, e := range rev {
		out[len(rev)-1-i] = e
	}
	return out, nil
}
