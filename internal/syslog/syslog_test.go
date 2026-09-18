package syslog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

func TestLogPathSiblingToState(t *testing.T) {
	got := LogPath(filepath.Join("/tmp", "base", ".wrk3-state.json"))
	want := filepath.Join("/tmp", "base", LogFileName)
	if got != want {
		t.Fatalf("LogPath = %q, want %q", got, want)
	}
	if LogPath("") != "" {
		t.Fatalf("LogPath(\"\") = %q, want empty", LogPath(""))
	}
}

func TestAppendReadLastRoundtrip(t *testing.T) {
	dir := t.TempDir()
	stateP := filepath.Join(dir, ".wrk3-state.json")
	if err := Append(stateP, Entry{Op: "up", Branch: "a", Msg: "first"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := Append(stateP, Entry{Level: LevelError, Op: "down", Branch: "b", Cmd: "docker compose down", Cwd: dir, Err: "boom", DurationMs: 12}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	entries, err := ReadLast(LogPath(stateP), 10)
	if err != nil {
		t.Fatalf("ReadLast: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	if entries[0].Op != "up" || entries[0].Branch != "a" || entries[0].Msg != "first" {
		t.Fatalf("first entry = %+v", entries[0])
	}
	if entries[1].Cmd != "docker compose down" || entries[1].Err != "boom" || entries[1].DurationMs != 12 {
		t.Fatalf("second entry = %+v", entries[1])
	}
	if entries[0].Time.IsZero() {
		t.Fatalf("zero Time not defaulted")
	}
}

func TestReadLastTailsAndSkipsBadLines(t *testing.T) {
	dir := t.TempDir()
	logP := filepath.Join(dir, LogFileName)
	for i := 0; i < 5; i++ {
		if err := Append(filepath.Join(dir, ".wrk3-state.json"), Entry{Op: "op", Msg: string(rune('a' + i))}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	f, err := os.OpenFile(logP, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	_, _ = f.WriteString("not json\n")
	_ = f.Close()
	got, err := ReadLast(logP, 2)
	if err != nil {
		t.Fatalf("ReadLast: %v", err)
	}
	if len(got) != 2 || got[0].Msg != "d" || got[1].Msg != "e" {
		t.Fatalf("tail = %+v, want d,e", got)
	}
}

func TestReadLastIncludesRotatedBackup(t *testing.T) {
	dir := t.TempDir()
	stateP := filepath.Join(dir, ".wrk3-state.json")
	logP := LogPath(stateP)
	if err := Append(stateP, Entry{Op: "op", Msg: "current"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	// Simulate a rotation: current becomes the backup, fresh current follows.
	if err := os.Rename(logP, logP+".1"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if err := Append(stateP, Entry{Op: "op", Msg: "fresh"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	got, err := ReadLast(logP, 10)
	if err != nil {
		t.Fatalf("ReadLast: %v", err)
	}
	if len(got) != 2 || got[0].Msg != "current" || got[1].Msg != "fresh" {
		t.Fatalf("history across rotation = %+v", got)
	}
	got, err = ReadLast(logP, 1)
	if err != nil || len(got) != 1 || got[0].Msg != "fresh" {
		t.Fatalf("tail across rotation = %+v %v", got, err)
	}
}
func TestAppendTruncatesHugeFields(t *testing.T) {
	dir := t.TempDir()
	stateP := filepath.Join(dir, ".wrk3-state.json")
	huge := strings.Repeat("x", maxErrBytes+100)
	if err := Append(stateP, Entry{Op: "exec", Err: huge}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	got, err := ReadLast(LogPath(stateP), 10)
	if err != nil || len(got) != 1 {
		t.Fatalf("ReadLast: %+v %v", got, err)
	}
	if len(got[0].Err) > maxErrBytes+50 || !strings.HasSuffix(got[0].Err, "…[truncated]") {
		t.Fatalf("err not truncated: len=%d", len(got[0].Err))
	}
}

func TestReadLastSkipsOversizedLines(t *testing.T) {
	dir := t.TempDir()
	stateP := filepath.Join(dir, ".wrk3-state.json")
	if err := Append(stateP, Entry{Op: "op", Msg: "good"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	logP := LogPath(stateP)
	f, err := os.OpenFile(logP, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// A valid-JSON line over the read cap must not blank the view.
	big, _ := json.Marshal(Entry{Op: "op", Msg: strings.Repeat("y", maxLineBytes+10)})
	_, _ = f.Write(append(big, '\n'))
	_ = f.Close()
	got, err := ReadLast(logP, 10)
	if err != nil {
		t.Fatalf("ReadLast: %v", err)
	}
	if len(got) != 1 || got[0].Msg != "good" {
		t.Fatalf("oversized line should be skipped: %+v", got)
	}
}

func TestAppendConcurrentSafe(t *testing.T) {
	dir := t.TempDir()
	stateP := filepath.Join(dir, ".wrk3-state.json")
	var wg sync.WaitGroup
	for g := 0; g < 20; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				_ = Append(stateP, Entry{Op: "op", Msg: fmt.Sprintf("g%d-%d", g, i)})
			}
		}(g)
	}
	wg.Wait()
	got, err := ReadLast(LogPath(stateP), 2000)
	if err != nil {
		t.Fatalf("ReadLast: %v", err)
	}
	if len(got) != 1000 {
		t.Fatalf("got %d entries, want 1000 (torn lines would parse-short)", len(got))
	}
}

func TestTruncateKeepsRuneBoundary(t *testing.T) {
	s := strings.Repeat("a", 10) + "é" + strings.Repeat("b", 10)
	got := truncate(s, 11)
	if !strings.HasSuffix(got, "…[truncated]") || !utf8.ValidString(got) {
		t.Fatalf("invalid truncation: %q", got)
	}
}

func TestReadLastSkipsUnreadableBackup(t *testing.T) {
	dir := t.TempDir()
	stateP := filepath.Join(dir, ".wrk3-state.json")
	if err := Append(stateP, Entry{Op: "op", Msg: "current"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	// A directory at the backup path makes it unreadable; the current
	// log must still come through.
	if err := os.Mkdir(LogPath(stateP)+".1", 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	got, err := ReadLast(LogPath(stateP), 10)
	if err != nil {
		t.Fatalf("ReadLast: %v", err)
	}
	if len(got) != 1 || got[0].Msg != "current" {
		t.Fatalf("backup failure hid current log: %+v", got)
	}
}

func TestReadLastMissingFile(t *testing.T) {
	got, err := ReadLast(filepath.Join(t.TempDir(), "missing.jsonl"), 10)
	if err != nil || len(got) != 0 {
		t.Fatalf("missing file: got %+v err %v", got, err)
	}
}

func TestAppendEmptyStatePathErrors(t *testing.T) {
	if err := Append("", Entry{Op: "up"}); err == nil {
		t.Fatalf("expected error for empty state path")
	}
}

func TestLogFileIsJSONLines(t *testing.T) {
	dir := t.TempDir()
	stateP := filepath.Join(dir, ".wrk3-state.json")
	start := time.Now().Add(-time.Minute).UTC()
	if err := Append(stateP, Entry{Op: "fetch", Source: "cli"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	raw, err := os.ReadFile(LogPath(stateP))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.HasSuffix(string(raw), "\n") {
		t.Fatalf("log line missing trailing newline: %q", raw)
	}
	entries, err := ReadLast(LogPath(stateP), 1)
	if err != nil || len(entries) != 1 {
		t.Fatalf("ReadLast: %+v %v", entries, err)
	}
	if entries[0].Time.Before(start) {
		t.Fatalf("timestamp not set: %+v", entries[0])
	}
}
