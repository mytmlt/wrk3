//go:build unix

package runner

import (
	"context"
	"testing"
	"time"
)

// None Exec must bound the invocation by the runner timeout even when a
// grandchild inherits the captured pipes (compose-plugin shape): the
// timeout context has to reach the group kill, not just CommandContext.
func TestNoneExecTimeoutKillsPipeHoldingGrandchild(t *testing.T) {
	r := &NoneRunner{Timeout: 300 * time.Millisecond}
	start := time.Now()
	// The shell itself exits 0 at once, so no error is expected — the
	// assertion is purely on timing: a wedged Wait takes the full 30s
	// sleep, a group kill returns near the 300ms deadline.
	_ = r.Exec(context.Background(), t.TempDir(), []string{"sh", "-c", "sleep 30 &"}, nil)
	if d := time.Since(start); d > 20*time.Second {
		t.Fatalf("Exec took %v, want return near the 300ms timeout", d)
	}
}
