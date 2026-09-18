package health

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestAggregate(t *testing.T) {
	cases := []struct {
		name       string
		results    []Result
		wantState  string
		wantSuffix string
		wantPass   int
		wantTotal  int
	}{
		{"empty", nil, "", "", 0, 0},
		{"single pass", []Result{{Name: "a", Healthy: true}}, StateHealthy, " (healthy)", 1, 1},
		{"single fail", []Result{{Name: "a"}}, StateUnhealthy, " (unhealthy)", 0, 1},
		{"all pass", []Result{{Name: "a", Healthy: true}, {Name: "b", Healthy: true}}, StateHealthy, " (healthy)", 2, 2},
		{"none pass", []Result{{Name: "a"}, {Name: "b"}}, StateUnhealthy, " (unhealthy)", 0, 2},
		{"some pass", []Result{{Name: "a", Healthy: true}, {Name: "b"}}, StateDegraded, " (degraded 1/2)", 1, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rep := Aggregate(c.results)
			if rep.State != c.wantState {
				t.Errorf("State = %q, want %q", rep.State, c.wantState)
			}
			if got := rep.Suffix(); got != c.wantSuffix {
				t.Errorf("Suffix = %q, want %q", got, c.wantSuffix)
			}
			if got := WithSuffix("running", rep); got != "running"+c.wantSuffix {
				t.Errorf("WithSuffix = %q, want %q", got, "running"+c.wantSuffix)
			}
			if rep.Passing != c.wantPass || rep.Total != c.wantTotal {
				t.Errorf("Passing/Total = %d/%d, want %d/%d", rep.Passing, rep.Total, c.wantPass, c.wantTotal)
			}
		})
	}
}

func TestProbeUnknown(t *testing.T) {
	rep := ProbeUnknown()
	if rep.State != StateUnknown {
		t.Fatalf("State = %q, want unknown", rep.State)
	}
	if got := WithSuffix("running", rep); got != "running (unknown)" {
		t.Errorf("WithSuffix = %q, want %q", got, "running (unknown)")
	}
}

func TestProbeShell(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	rep := ProbeShell(ctx, dir, map[string]string{"APP_PORT": "8100"}, []Check{
		{Name: "ok", Run: "true"},
		{Name: "fail", Run: "false"},
	})
	if rep.State != StateDegraded {
		t.Fatalf("State = %q, want degraded", rep.State)
	}
	if rep.Passing != 1 || rep.Total != 2 {
		t.Fatalf("Passing/Total = %d/%d, want 1/2", rep.Passing, rep.Total)
	}
	if got := rep.Summary(); !strings.Contains(got, "ok: pass") || !strings.Contains(got, "fail: fail") {
		t.Errorf("Summary = %q, want ok pass + fail fail", got)
	}

	empty := ProbeShell(ctx, dir, nil, nil)
	if empty.Total != 0 || empty.Suffix() != "" {
		t.Errorf("empty probe = %+v, want zero report", empty)
	}

	blank := ProbeShell(ctx, dir, nil, []Check{{Name: "x", Run: "  "}})
	if blank.Total != 0 {
		t.Errorf("blank run probe Total = %d, want 0", blank.Total)
	}
}

func TestProbeShellEnvAndTimeout(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	rep := ProbeShell(ctx, dir, nil, []Check{{Name: "env", Run: `test "$FOO" = bar`}})
	_ = rep
	envRep := ProbeShell(ctx, dir, map[string]string{"FOO": "bar"}, []Check{{Name: "env", Run: `test "$FOO" = bar`}})
	if envRep.State != StateHealthy {
		t.Errorf("env probe State = %q, want healthy", envRep.State)
	}
	toRep := ProbeShell(ctx, dir, nil, []Check{{Name: "slow", Run: "sleep 2", Timeout: 100 * time.Millisecond}})
	if toRep.State != StateUnhealthy {
		t.Errorf("timeout probe State = %q, want unhealthy", toRep.State)
	}
}

func TestMergeEnv(t *testing.T) {
	got := mergeEnv([]string{"PATH=/usr/bin", "FOO=old"}, map[string]string{
		"FOO": "new", "APP_PORT": "8100", "": "x", "BAD=KEY": "y",
	})
	byKey := map[string]string{}
	for _, kv := range got {
		if i := strings.IndexByte(kv, '='); i >= 0 {
			byKey[kv[:i]] = kv[i+1:]
		}
	}
	if byKey["PATH"] != "/usr/bin" {
		t.Errorf("PATH = %q, want preserved /usr/bin", byKey["PATH"])
	}
	if byKey["FOO"] != "new" {
		t.Errorf("FOO = %q, want overlaid new", byKey["FOO"])
	}
	if byKey["APP_PORT"] != "8100" {
		t.Errorf("APP_PORT = %q, want 8100", byKey["APP_PORT"])
	}
	if _, ok := byKey["BAD=KEY"]; ok {
		t.Error("malformed key should be skipped")
	}
}

func TestCappedBuffer(t *testing.T) {
	var b cappedBuffer
	if n, err := b.Write([]byte("hello")); n != 5 || err != nil {
		t.Fatalf("Write = %d, %v; want 5, nil", n, err)
	}
	big := make([]byte, maxProbeOutput+1000)
	for i := range big {
		big[i] = 'x'
	}
	if n, err := b.Write(big); n != len(big) || err != nil {
		t.Fatalf("Write big = %d, %v; want %d, nil", n, err, len(big))
	}
	if got := len(b.buf); got != maxProbeOutput {
		t.Errorf("buffered = %d, want cap %d", got, maxProbeOutput)
	}
	// Verbose checks stay bounded end to end.
	dir := t.TempDir()
	rep := ProbeShell(context.Background(), dir, nil, []Check{{Name: "chatty", Run: "yes | head -c 1000000"}})
	if rep.Total != 1 || !rep.Results[0].Healthy {
		t.Fatalf("chatty probe = %+v, want one pass", rep)
	}
	if got := len(rep.Results[0].Output); got > 500 {
		t.Errorf("output runes = %d, want <= 500", got)
	}
}

func TestMerge(t *testing.T) {
	rep := Merge([]Result{{Name: "api", Healthy: true}}, []Result{{Name: "container:web", Healthy: false}})
	if rep.State != StateDegraded {
		t.Errorf("State = %q, want degraded", rep.State)
	}
	if rep.Total != 2 {
		t.Errorf("Total = %d, want 2", rep.Total)
	}
}
