//go:build unix

package health

import (
	"context"
	"os/exec"
	"syscall"
)

// startKillable runs c in its own process group and waits for it,
// killing the whole group when ctx ends (deadline or cancel).
//
// Background: exec.CommandContext only signals the direct child. A check
// like `sh -c` forks grandchildren that inherit the captured
// stdout/stderr pipes; killing just the parent leaves them holding those
// pipes open, so Wait blocks past every timeout. Killing the process
// group takes the grandchildren down too (mirrors internal/runner
// startKillable, which solves the same problem for the compose plugin).
func startKillable(ctx context.Context, c *exec.Cmd) error {
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := c.Start(); err != nil {
		return err
	}
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			// Best-effort: the process may already be gone.
			_ = syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
		case <-done:
		}
	}()
	return c.Wait()
}
