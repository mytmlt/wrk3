package telemetry

import (
	"os"
	"strings"
	"testing"
)

func TestScrub_NilError(t *testing.T) {
	if got := Scrub(nil); got != "" {
		t.Errorf("Scrub(nil) = %q, want empty", got)
	}
}

func TestScrub_PlainMessage(t *testing.T) {
	err := errSentinel("something went wrong")
	got := Scrub(err)
	if got != "something went wrong" {
		t.Errorf("Scrub = %q, want %q", got, "something went wrong")
	}
}

func TestScrub_HomeDirectory(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home dir")
	}
	err = errSentinel("config file not found at " + home + "/.config/wrk3/config.yaml")
	got := Scrub(err)
	if strings.Contains(got, home) {
		t.Errorf("Scrub should replace home dir; got %q", got)
	}
	if !strings.Contains(got, "$HOME") {
		t.Errorf("Scrub should contain $HOME; got %q", got)
	}
}

func TestScrub_AbsolutePath(t *testing.T) {
	err := errSentinel("open /home/user/project/.worktrees/feature-foo/ports: no such file or directory")
	got := Scrub(err)
	if strings.Contains(got, "/home/user/project") {
		t.Errorf("Scrub should replace absolute paths; got %q", got)
	}
}

func TestScrub_Email(t *testing.T) {
	err := errSentinel("user developer@example.com not found")
	got := Scrub(err)
	if strings.Contains(got, "developer@example.com") {
		t.Errorf("Scrub should remove email; got %q", got)
	}
}

func TestScrub_IPv4(t *testing.T) {
	err := errSentinel("connection refused 192.168.1.1:8080")
	got := Scrub(err)
	if strings.Contains(got, "192.168.1.1") {
		t.Errorf("Scrub should remove IP; got %q", got)
	}
}

func TestScrub_UUID(t *testing.T) {
	err := errSentinel("worktree 550e8400-e29b-41d4-a716-446655440000 not found")
	got := Scrub(err)
	if strings.Contains(got, "550e8400") {
		t.Errorf("Scrub should remove UUID; got %q", got)
	}
}

func TestScrub_HexHash(t *testing.T) {
	err := errSentinel("commit abc123def456 not found")
	got := Scrub(err)
	if strings.Contains(got, "abc123def456") {
		t.Errorf("Scrub should remove hex hash; got %q", got)
	}
}

func TestScrub_QuotedWorktreeName(t *testing.T) {
	err := errSentinel(`unknown worktree "chore/bake-sentry-dsn" (see status)`)
	got := Scrub(err)
	if strings.Contains(got, "chore/bake-sentry-dsn") {
		t.Errorf("Scrub should remove quoted worktree name; got %q", got)
	}
	if !strings.Contains(got, "unknown worktree") {
		t.Errorf("Scrub should keep error class; got %q", got)
	}
	if !strings.Contains(got, `"<name>"`) {
		t.Errorf("Scrub should replace quoted name with placeholder; got %q", got)
	}
}

func TestScrub_QuotedHyphenatedSlug(t *testing.T) {
	err := errSentinel(`unknown worktree "feature-foo" (see status)`)
	got := Scrub(err)
	if strings.Contains(got, "feature-foo") {
		t.Errorf("Scrub should remove hyphenated slug; got %q", got)
	}
	if !strings.Contains(got, `"<name>"`) {
		t.Errorf("Scrub should replace quoted name with placeholder; got %q", got)
	}
}

func TestScrub_QuotedNameInOtherError(t *testing.T) {
	err := errSentinel(`branch "feat/redact-names" already exists`)
	got := Scrub(err)
	if strings.Contains(got, "feat/redact-names") {
		t.Errorf("Scrub should remove quoted name; got %q", got)
	}
	if !strings.Contains(got, `"<name>"`) {
		t.Errorf("Scrub should replace quoted name with placeholder; got %q", got)
	}
}

func TestScrub_QuotedNameWithEscapedQuote(t *testing.T) {
	err := errSentinel(`unknown worktree "foo\"bar" (see status)`)
	got := Scrub(err)
	if strings.Contains(got, `foo\"bar`) || strings.Contains(got, `foo"bar`) {
		t.Errorf("Scrub should remove quoted name with escaped quote; got %q", got)
	}
	if !strings.Contains(got, `"<name>"`) {
		t.Errorf("Scrub should replace quoted name with placeholder; got %q", got)
	}
}

func TestScrub_CapLength(t *testing.T) {
	long := strings.Repeat("a", 2000)
	err := errSentinel(long)
	got := Scrub(err)
	if len(got) > 1024 {
		t.Errorf("Scrub should cap at 1024 bytes, got %d", len(got))
	}
}

func TestScrub_EmptyMessage(t *testing.T) {
	err := errSentinel("")
	got := Scrub(err)
	if got != "" {
		t.Errorf("Scrub of empty error should be empty; got %q", got)
	}
}

type errSentinel string

func (e errSentinel) Error() string { return string(e) }
