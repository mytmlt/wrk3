//go:build unix

package runner

import (
	"context"
	"os/exec"
	"syscall"
)

// startKillable runs c in its own process group and waits for it,
// killing the whole group when ctx ends (deadline or cancel).
//
// Background: exec.CommandContext only signals the direct child. The
// docker CLI forks grandchildren (the compose plugin), which inherit
// the captured stdout/stderr pipes. Killing just the parent leaves the
// grandchildren holding those pipes open, so Wait blocks forever and a
// read-only command like `ls` hangs past every timeout. Killing the
// process group takes the grandchildren down too, closing the pipes
// and letting Wait return promptly.
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
