package runner

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

// defaultLocalTimeout bounds each host entry command. Builds on a cold
// cache can take minutes, matching the compose runners.
const defaultLocalTimeout = 5 * time.Minute

// LocalRunner executes worktrees on the host with no compose stack and
// no daemon. Isolation is the worktree directory (and optional ports).
//
// Up/Down are no-ops: cmd/up and cmd/down already run entry.setup/run/stop
// via Exec. Status is unknown (nothing to probe) so stored up/down
// lifecycle is the source of truth — running means the last up succeeded
// (ready/built), not that a daemon is alive. Logs errors unless the
// caller runs entry.logs.
type LocalRunner struct {
	// Timeout bounds each Exec. Zero means defaultLocalTimeout.
	Timeout time.Duration
}

// NewLocal returns a LocalRunner.
func NewLocal() *LocalRunner { return &LocalRunner{} }

func init() {
	Register("local", func(Options) Runner { return NewLocal() })
}

var _ Runner = (*LocalRunner)(nil)

func (r *LocalRunner) timeout() time.Duration {
	if r != nil && r.Timeout > 0 {
		return r.Timeout
	}
	return defaultLocalTimeout
}

// Up is a no-op: there is no runtime to start. entry.setup/run run via Exec.
func (r *LocalRunner) Up(_ context.Context, worktreePath string, _ map[string]string) error {
	if strings.TrimSpace(worktreePath) == "" {
		return fmt.Errorf("local up: empty worktree path")
	}
	return nil
}

// Down is a no-op: there is no runtime to stop. entry.stop runs via Exec.
func (r *LocalRunner) Down(_ context.Context, worktreePath string, _ map[string]string) error {
	if strings.TrimSpace(worktreePath) == "" {
		return fmt.Errorf("local down: empty worktree path")
	}
	return nil
}

// Logs has nothing to tail. Set entry.logs to a host command instead.
func (r *LocalRunner) Logs(_ context.Context, worktreePath string, _ bool) (string, error) {
	if strings.TrimSpace(worktreePath) == "" {
		return "", fmt.Errorf("local logs: empty worktree path")
	}
	return "", fmt.Errorf("local logs: no process to tail; set entry.logs in wrk3.yaml")
}

// Exec runs cmd as a host process with cwd=worktreePath and env applied.
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
			strings.Join(cmd, " "), worktreePath, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// Status reports unknown: there is no daemon to probe. Display uses the
// stored up/down lifecycle (see ports.ResolveDisplayStatus).
func (r *LocalRunner) Status(_ context.Context, worktreePath string) (Status, error) {
	if strings.TrimSpace(worktreePath) == "" {
		return Status{State: StateUnknown}, fmt.Errorf("local status: empty worktree path")
	}
	return Status{State: StateUnknown, Detail: "no runtime to probe"}, nil
}
