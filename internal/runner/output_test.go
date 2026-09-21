package runner

import (
	"context"
	"strings"
	"testing"
)

func TestWithOutputRoundTrip(t *testing.T) {
	var got []string
	ctx := WithOutput(context.Background(), func(line string) { got = append(got, line) })
	sink := OutputFrom(ctx)
	if sink == nil {
		t.Fatal("sink missing from ctx")
	}
	FeedLine(sink, "hello")
	FeedLine(sink, "   ")
	FeedLine(sink, "")
	if len(got) != 1 || got[0] != "hello" {
		t.Fatalf("got %q, want [hello]", got)
	}
	if OutputFrom(context.Background()) != nil {
		t.Fatal("empty ctx must yield nil sink")
	}
	if OutputFrom(nil) != nil {
		t.Fatal("nil ctx must yield nil sink")
	}
}

func TestPrefixedNilStaysNil(t *testing.T) {
	if Prefixed(nil, "[x] ") != nil {
		t.Fatal("Prefixed(nil) must stay nil")
	}
	var got []string
	p := Prefixed(func(l string) { got = append(got, l) }, "[up] ")
	p("hi")
	if len(got) != 1 || got[0] != "[up] hi" {
		t.Fatalf("got %q", got)
	}
}

func TestFeedLinesSplitsAndSkipsBlanks(t *testing.T) {
	var got []string
	FeedLines(func(l string) { got = append(got, l) }, "a\n\nb\r\n")
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("got %q, want [a b]", got)
	}
	FeedLines(nil, "a\n")
}

func TestFeedLineTruncatesRunaway(t *testing.T) {
	var got []string
	FeedLine(func(l string) { got = append(got, l) }, strings.Repeat("x", maxSinkLine+10))
	if len(got) != 1 || !strings.HasSuffix(got[0], "… [truncated]") {
		t.Fatalf("runaway line not truncated: %d chars", len(got[0]))
	}
}

func TestRunCmdLiveNoSinkMatchesBuffered(t *testing.T) {
	stdout, stderr, err := runCmdLive(context.Background(), "sh", t.TempDir(), nil, "-c", "echo hi")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(stdout, "hi") {
		t.Fatalf("stdout = %q, want hi", stdout)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
}

func TestRunCmdLiveSinkStreamsLines(t *testing.T) {
	var got []string
	ctx := WithOutput(context.Background(), func(l string) { got = append(got, l) })
	stdout, _, err := runCmdLive(ctx, "sh", t.TempDir(), nil, "-c", "echo one; echo two >&2")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(stdout, "one") {
		t.Fatalf("stdout = %q, want one", stdout)
	}
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "one") || !strings.Contains(joined, "two") {
		t.Fatalf("sink missing lines: %q", joined)
	}
}

func TestRunCmdLiveSinkErrorKeepsOutput(t *testing.T) {
	var got []string
	ctx := WithOutput(context.Background(), func(l string) { got = append(got, l) })
	_, _, err := runCmdLive(ctx, "sh", t.TempDir(), nil, "-c", "echo before-fail; exit 3")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if len(got) == 0 || !strings.Contains(strings.Join(got, "\n"), "before-fail") {
		t.Fatalf("sink must keep pre-failure output: %q", got)
	}
}
