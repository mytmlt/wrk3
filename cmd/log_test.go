package cmd

import (
	"strings"
	"testing"

	"github.com/mytmlt/wrk3/internal/syslog"
)

func TestLogCommandPrintsPersistedEntries(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	stateP := cfg.StatePath()
	if err := syslog.Append(stateP, syslog.Entry{Op: "up", Branch: "a", Msg: "up a"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := syslog.Append(stateP, syslog.Entry{Level: syslog.LevelError, Op: "exec", Branch: "a", Cmd: "sh -c false", Err: "exit 1"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	oldTail, oldJSON := logTail, logJSON
	defer func() { logTail, logJSON = oldTail, oldJSON }()
	out, _, err := executeCmd("-f", cfg.ConfigPath(), "log", "-n", "10")
	if err != nil {
		t.Fatalf("log: %v", err)
	}
	if !strings.Contains(out, "[up]") || !strings.Contains(out, "up a") {
		t.Errorf("log output missing up entry: %q", out)
	}
	if !strings.Contains(out, "cmd=sh -c false") || !strings.Contains(out, "err=exit 1") {
		t.Errorf("log output missing exec details: %q", out)
	}
}

func TestLogCommandJSON(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	if err := syslog.Append(cfg.StatePath(), syslog.Entry{Op: "fetch", Msg: "fetch origin"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	oldTail, oldJSON := logTail, logJSON
	defer func() { logTail, logJSON = oldTail, oldJSON }()
	out, _, err := executeCmd("-f", cfg.ConfigPath(), "log", "--json")
	if err != nil {
		t.Fatalf("log --json: %v", err)
	}
	if !strings.Contains(out, `"op":"fetch"`) {
		t.Errorf("json output missing entry: %q", out)
	}
}

func TestLogCommandEmpty(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	oldTail, oldJSON := logTail, logJSON
	defer func() { logTail, logJSON = oldTail, oldJSON }()
	out, _, err := executeCmd("-f", cfg.ConfigPath(), "log")
	if err != nil {
		t.Fatalf("log on empty: %v", err)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("empty log should print nothing, got %q", out)
	}
}

func TestLogCommandRejectsNonPositiveTail(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	oldTail, oldJSON := logTail, logJSON
	defer func() { logTail, logJSON = oldTail, oldJSON }()
	if _, _, err := executeCmd("-f", cfg.ConfigPath(), "log", "-n", "0"); err == nil {
		t.Errorf("log -n 0 should fail")
	}
}

func TestFormatSyslogEntrySingleLine(t *testing.T) {
	out := formatSyslogEntry(syslog.Entry{Op: "exec", Branch: "a", Msg: "line1\nline2", Cmd: "sh -c x", Err: "fail\n\x1b[31mred\x1b[0m\nsecond"})
	if strings.Contains(out, "\n") || strings.Contains(out, "\x1b") {
		t.Errorf("entry must render as one clean line: %q", out)
	}
	for _, want := range []string{"line1 line2", "fail", "second"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q: %q", want, out)
		}
	}
}
