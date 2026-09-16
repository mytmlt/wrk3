package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// clipboardCandidates lists the OS clipboard feeders to try in order.
// Each entry is a command line: binary first, then args. The text is
// always fed via stdin.
func clipboardCandidates(goos string) [][]string {
	switch goos {
	case "darwin":
		return [][]string{{"pbcopy"}}
	case "windows":
		return [][]string{{"cmd", "/c", "clip"}}
	default: // linux and other unixes: wayland first, then X11
		return [][]string{
			{"wl-copy"},
			{"xclip", "-selection", "clipboard"},
			{"xsel", "--clipboard", "--input"},
		}
	}
}

// clipboardLookPath and clipboardFeed are vars so tests can stub them
// without spawning real clipboard tools.
var clipboardLookPath = exec.LookPath

var clipboardFeed = func(name string, args []string, text string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, name, args...)
	c.Stdin = bytes.NewBufferString(text)
	var stderr bytes.Buffer
	c.Stderr = &stderr
	if err := c.Run(); err != nil {
		if stderr.Len() > 0 {
			return fmt.Errorf("copy to clipboard via %s: %w (%s)", name, err, strings.TrimSpace(stderr.String()))
		}
		return fmt.Errorf("copy to clipboard via %s: %w", name, err)
	}
	return nil
}

// copyTextToClipboard feeds text to the first available OS clipboard
// tool (pbcopy on macOS, wl-copy/xclip/xsel on Linux, clip on
// Windows). It returns an error when no tool is installed or every
// tool fails; callers should still surface the text itself so it stays
// manually copyable.
func copyTextToClipboard(text string) error {
	var lastErr error
	for _, cand := range clipboardCandidates(runtime.GOOS) {
		if _, err := clipboardLookPath(cand[0]); err != nil {
			lastErr = err
			continue
		}
		if err := clipboardFeed(cand[0], cand[1:], text); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	if lastErr == nil {
		return fmt.Errorf("copy to clipboard: no clipboard tool found")
	}
	return fmt.Errorf("copy to clipboard: %w", lastErr)
}
