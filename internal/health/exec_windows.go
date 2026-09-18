//go:build windows

package health

import (
	"context"
	"os/exec"
)

// startKillable runs c until it exits or ctx ends. Windows has no
// process-group kill via syscall, so this falls back to
// CommandContext semantics (direct child only); see exec_unix.go.
func startKillable(ctx context.Context, c *exec.Cmd) error {
	c2 := exec.CommandContext(ctx, c.Path, c.Args[1:]...)
	c2.Dir = c.Dir
	c2.Env = c.Env
	c2.Stdout = c.Stdout
	c2.Stderr = c.Stderr
	c2.Stdin = c.Stdin
	return c2.Run()
}
