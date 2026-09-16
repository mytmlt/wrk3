package cmd

import (
	"errors"
	"reflect"
	"testing"
)

func TestClipboardCandidates_ByOS(t *testing.T) {
	if got := clipboardCandidates("darwin"); !reflect.DeepEqual(got, [][]string{{"pbcopy"}}) {
		t.Errorf("darwin: got %v, want [[pbcopy]]", got)
	}
	if got := clipboardCandidates("windows"); !reflect.DeepEqual(got, [][]string{{"cmd", "/c", "clip"}}) {
		t.Errorf("windows: got %v, want [[cmd /c clip]]", got)
	}
	got := clipboardCandidates("linux")
	if len(got) != 3 || got[0][0] != "wl-copy" || got[1][0] != "xclip" || got[2][0] != "xsel" {
		t.Errorf("linux: got %v, want [wl-copy xclip xsel] order", got)
	}
}

func TestCopyTextToClipboard_Success(t *testing.T) {
	oldLook, oldFeed := clipboardLookPath, clipboardFeed
	t.Cleanup(func() { clipboardLookPath, clipboardFeed = oldLook, oldFeed })
	var fedName string
	var fedText string
	clipboardLookPath = func(string) (string, error) { return "/usr/bin/pbcopy", nil }
	clipboardFeed = func(name string, _ []string, text string) error {
		fedName, fedText = name, text
		return nil
	}
	if err := copyTextToClipboard("http://localhost:8000"); err != nil {
		t.Fatalf("copy: %v", err)
	}
	if fedText != "http://localhost:8000" {
		t.Errorf("fed text = %q, want the URL", fedText)
	}
	if fedName == "" {
		t.Error("no clipboard tool was invoked")
	}
}

func TestCopyTextToClipboard_NoTool(t *testing.T) {
	oldLook, oldFeed := clipboardLookPath, clipboardFeed
	t.Cleanup(func() { clipboardLookPath, clipboardFeed = oldLook, oldFeed })
	clipboardLookPath = func(string) (string, error) { return "", errors.New("not found") }
	clipboardFeed = func(string, []string, string) error {
		t.Error("feed must not run when no tool is installed")
		return nil
	}
	if err := copyTextToClipboard("http://localhost:8000"); err == nil {
		t.Error("want an error when no clipboard tool exists")
	}
}

func TestCopyTextToClipboard_FeedError(t *testing.T) {
	oldLook, oldFeed := clipboardLookPath, clipboardFeed
	t.Cleanup(func() { clipboardLookPath, clipboardFeed = oldLook, oldFeed })
	clipboardLookPath = func(string) (string, error) { return "/usr/bin/pbcopy", nil }
	clipboardFeed = func(string, []string, string) error { return errors.New("pbcopy failed") }
	if err := copyTextToClipboard("http://localhost:8000"); err == nil {
		t.Error("want the feeder error propagated")
	}
}
