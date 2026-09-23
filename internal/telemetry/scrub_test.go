package telemetry

import (
	"os"
	"strings"
	"testing"

	"github.com/getsentry/sentry-go"
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

func TestScrub_CobraUnknownCommand(t *testing.T) {
	msg := `unknown command "foobar" for "wrk3"`

	err := errSentinel(msg)
	got := Scrub(err)
	if !strings.Contains(got, `unknown command "foobar" for "wrk3"`) {
		t.Errorf("Scrub should keep quoted command and executable; got %q", got)
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

func TestScrubFrame_RedactsPIIKeepsLocation(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home dir")
	}
	f := sentry.Frame{
		Filename:    home + "/src/user@example.com/192.168.1.1/550e8400-e29b-41d4-a716-446655440000/abc123def456/foo.go",
		AbsPath:     home + "/src/foo.go",
		Module:      "github.com/mytmlt/wrk3/internal/telemetry",
		Function:    "ReportIfEnabled",
		Package:     home + "/pkg",
		Lineno:      49,
		Colno:       12,
		InApp:       true,
		Vars:        map[string]interface{}{"secret": "value"},
		ContextLine: "var secret = 1",
		PreContext:  []string{"before"},
		PostContext: []string{"after"},
	}
	scrubFrame(&f)
	if strings.Contains(f.Filename, home) || strings.Contains(f.AbsPath, home) || strings.Contains(f.Package, home) {
		t.Errorf("home dir still present: filename=%q abs=%q pkg=%q", f.Filename, f.AbsPath, f.Package)
	}
	if !strings.Contains(f.Filename, "$HOME") {
		t.Errorf("filename missing $HOME: %q", f.Filename)
	}
	for _, tok := range []string{"user@example.com", "192.168.1.1", "550e8400-e29b-41d4-a716-446655440000", "abc123def456"} {
		if strings.Contains(f.Filename, tok) {
			t.Errorf("filename still contains %q: %q", tok, f.Filename)
		}
	}
	if f.Module != "github.com/mytmlt/wrk3/internal/telemetry" {
		t.Errorf("Module = %q, want kept", f.Module)
	}
	if f.Function != "ReportIfEnabled" {
		t.Errorf("Function = %q, want kept", f.Function)
	}
	if f.Lineno != 49 {
		t.Errorf("Lineno = %d, want 49", f.Lineno)
	}
	if f.Vars != nil {
		t.Errorf("Vars = %#v, want nil", f.Vars)
	}
	if f.ContextLine != "" || f.PreContext != nil || f.PostContext != nil {
		t.Errorf("source context not cleared: line=%q pre=%v post=%v", f.ContextLine, f.PreContext, f.PostContext)
	}
}

type errSentinel string

func (e errSentinel) Error() string { return string(e) }
