package runner

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

// defaultLocalTimeout bounds host entry commands (same budget as compose
// --build: a cold `go test` / install can take minutes).
const defaultLocalTimeout = 5 * time.Minute

// LocalRunner implements Runner with no container engine: Up/Down are
// no-ops, Exec runs host entry commands with cwd=worktreePath and
// env=allocated ports, Status is unknown (no process probe, so stored
// lifecycle is not clobbered), and Logs errors toward entry.logs.
type LocalRunner struct {
	// Timeout bounds each Exec. Zero means defaultLocalTimeout.
	Timeout time.Duration
}

// NewLocal returns a LocalRunner.
func NewLocal() *LocalRunner {
	return &LocalRunner{}
}

func init() {
	Register("local", func(Options) Runner { return NewLocal() })
}

var _ Runner = (*LocalRunner)(nil)

func (r *LocalRunner) timeout() time.Duration {
	if r.Timeout > 0 {
		return r.Timeout
	}
	return defaultLocalTimeout
}

// Up is a no-op: host processes start via entry.setup / entry.run.
func (r *LocalRunner) Up(_ context.Context, worktreePath string, _ map[string]string) error {
	if strings.TrimSpace(worktreePath) == "" {
		return fmt.Errorf("local up: empty worktree path")
	}
	return nil
}

// Down is a no-op: host processes stop via entry.stop.
func (r *LocalRunner) Down(_ context.Context, worktreePath string, _ map[string]string) error {
	if strings.TrimSpace(worktreePath) == "" {
		return fmt.Errorf("local down: empty worktree path")
	}
	return nil
}

// Logs has no compose stack. Callers should set entry.logs for host logs.
func (r *LocalRunner) Logs(_ context.Context, worktreePath string, _ bool) (string, error) {
	if strings.TrimSpace(worktreePath) == "" {
		return "", fmt.Errorf("local logs: empty worktree path")
	}
	return "", fmt.Errorf("local logs: no compose stack; set entry.logs to capture host logs")
}

// Exec runs cmd as a host process with cwd=worktreePath and env applied
// (used for entry commands). COMPOSE_PROJECT_NAME is not forced.
func (r *LocalRunner) Exec(ctx context.Context, worktreePath string, cmd []string, env map[string]string) error {
	if strings.TrimSpace(worktreePath) == "" {
		return fmt.Errorf("local exec: empty worktree path")
	}
	if len(cmd) == 0 || strings.TrimSpace(cmd[0]) == "" {
		return fmt.Errorf("local exec: empty command")
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, r.timeout())
	defer cancel()
	var stdout, stderr bytes.Buffer
	if err := runDockerCmd(timeoutCtx, cmd[0], worktreePath,
		mergeEnv(os.Environ(), env), &stdout, &stderr, cmd[1:]...); err != nil {
		return fmt.Errorf("exec %q (dir=%s): %w: %s",
			strings.Join(cmd, " "), worktreePath,
			err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// Status cannot probe host processes, so it reports unknown (never an
// error). Unknown probes do not persist, so up/down lifecycle in the
// state file is left intact.
func (r *LocalRunner) Status(_ context.Context, worktreePath string) (Status, error) {
	if strings.TrimSpace(worktreePath) == "" {
		return Status{State: StateUnknown}, fmt.Errorf("local status: empty worktree path")
	}
	return Status{State: StateUnknown, Detail: "local runner has no process probe"}, nil
}
