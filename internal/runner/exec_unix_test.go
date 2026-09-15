//go:build unix

package runner

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// A shell that backgrounds a long sleep hands the captured stdout pipe
// to the grandchild and exits at once. Plain CommandContext only kills
// the direct child, so the orphaned sleep holds the pipe open and Wait
// wedges past the deadline; startKillable must return near the deadline
// instead (this is the `docker compose` plugin shape that wedged `ls`).
func TestStartKillable_KillsOrphanedGrandchild(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	c := exec.Command("sh", "-c", "sleep 30 &")
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	start := time.Now()
	// The shell itself exits 0 at once, so no error is expected —
	// the assertion is purely on timing: a wedged Wait takes the
	// full 30s sleep, a group kill returns near the 300ms deadline.
	_ = startKillable(ctx, c)
	if elapsed := time.Since(start); elapsed > 15*time.Second {
		t.Fatalf("startKillable took %v; orphaned grandchild wedged Wait", elapsed)
	}
}

func TestStartKillable_Passthrough(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c := exec.Command("echo", "hi")
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	if err := startKillable(ctx, c); err != nil {
		t.Fatalf("startKillable: %v", err)
	}
	if strings.TrimSpace(stdout.String()) != "hi" {
		t.Fatalf("stdout = %q, want hi", stdout.String())
	}
}
