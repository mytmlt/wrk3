package runner

import (
	"context"
	"strings"
)

// OutputFunc receives one raw command output line (no trailing newline)
// while a Runner command executes. Implementations call it for every
// stdout/stderr line when the caller's context carries a sink (see
// WithOutput); a nil sink means discard. Sinks must be safe for
// concurrent use: parallel targets share one context and report on
// overlapping goroutines.
type OutputFunc func(line string)

// ctxKey carries the OutputFunc. Unexported key type keeps the value
// collision-free; access goes through WithOutput/OutputFrom.
type ctxKey struct{}

// WithOutput returns ctx carrying out as the live command-output sink.
// A nil out clears the sink. Runner commands (Up/Down/Exec) tee every
// output line to the sink while still capturing full output for error
// text, so callers see progress live instead of only on failure.
func WithOutput(ctx context.Context, out OutputFunc) context.Context {
	return context.WithValue(ctx, ctxKey{}, out)
}

// OutputFrom returns the live output sink carried by ctx, or nil when
// none is set. Custom Runner backends (see docs/PLUGINS.md) should tee
// long-running command output through it so up/down/reload stay visible.
func OutputFrom(ctx context.Context) OutputFunc {
	if ctx == nil {
		return nil
	}
	out, _ := ctx.Value(ctxKey{}).(OutputFunc)
	return out
}

// maxSinkLine caps one sink line so a runaway command cannot bloat the
// consumer (dashboard console buffer, CLI scrollback).
const maxSinkLine = 4096

// FeedLine delivers one raw line to out, dropping empties (compose
// progress and shell output are full of blank padding) and truncating
// runaway lines with a marker. Nil out is a no-op.
func FeedLine(out OutputFunc, line string) {
	if out == nil {
		return
	}
	line = strings.TrimRight(line, "\r")
	if strings.TrimSpace(line) == "" {
		return
	}
	if len(line) > maxSinkLine {
		line = line[:maxSinkLine] + "… [truncated]"
	}
	out(line)
}

// FeedLines splits captured output into lines and feeds each to out.
// Used by platforms without live pipe streaming (windows) so the
// console still shows command output, just after completion.
func FeedLines(out OutputFunc, s string) {
	if out == nil {
		return
	}
	for _, line := range strings.Split(s, "\n") {
		FeedLine(out, line)
	}
}

// Prefixed wraps out so every line carries prefix (e.g. "[slug] ").
// A nil out stays nil so callers can wrap unconditionally.
func Prefixed(out OutputFunc, prefix string) OutputFunc {
	if out == nil {
		return nil
	}
	return func(line string) { out(prefix + line) }
}
