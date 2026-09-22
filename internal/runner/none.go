package runner

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

const defaultNoneTimeout = 5 * time.Minute

type NoneRunner struct {
	Timeout time.Duration
}

func NewNoneRunner() *NoneRunner {
	return &NoneRunner{}
}

func init() {
	Register("none", func(_ Options) Runner { return NewNoneRunner() })
}

var _ Runner = (*NoneRunner)(nil)

func (r *NoneRunner) timeout() time.Duration {
	if r.Timeout > 0 {
		return r.Timeout
	}
	return defaultNoneTimeout
}

func (r *NoneRunner) Up(ctx context.Context, worktreePath string, env map[string]string) error {
	if strings.TrimSpace(worktreePath) == "" {
		return fmt.Errorf("none up: empty worktree path")
	}
	return nil
}

func (r *NoneRunner) Down(ctx context.Context, worktreePath string, env map[string]string) error {
	if strings.TrimSpace(worktreePath) == "" {
		return fmt.Errorf("none down: empty worktree path")
	}
	return nil
}

func (r *NoneRunner) Logs(ctx context.Context, worktreePath string, follow bool) (string, error) {
	if strings.TrimSpace(worktreePath) == "" {
		return "", fmt.Errorf("none logs: empty worktree path")
	}
	return "", nil
}

func (r *NoneRunner) Exec(ctx context.Context, worktreePath string, cmd []string, env map[string]string) error {
	if strings.TrimSpace(worktreePath) == "" {
		return fmt.Errorf("none exec: empty worktree path")
	}
	if len(cmd) == 0 || strings.TrimSpace(cmd[0]) == "" {
		return fmt.Errorf("none exec: empty command")
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, r.timeout())
	defer cancel()
	var stdout, stderr bytes.Buffer
	c := exec.CommandContext(timeoutCtx, cmd[0], cmd[1:]...)
	c.Dir = worktreePath
	c.Env = buildEnvNoProject(os.Environ(), env)
	c.Stdout = &stdout
	c.Stderr = &stderr
	if err := startKillable(ctx, c); err != nil {
		return fmt.Errorf("exec %q (dir=%s): %w: %s",
			strings.Join(cmd, " "), worktreePath, err,
			strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (r *NoneRunner) Status(ctx context.Context, worktreePath string) (Status, error) {
	if strings.TrimSpace(worktreePath) == "" {
		return Status{State: StateUnknown}, fmt.Errorf("none status: empty worktree path")
	}
	return Status{State: StateStopped, Detail: "no runner backend"}, nil
}

// buildEnvNoProject is like buildEnv but without COMPOSE_PROJECT_NAME.
// Since DockerRunner's buildEnv forces that variable, the none runner
// needs its own version that skips it.
func buildEnvNoProject(base []string, extra map[string]string) []string {
	merged := make([]string, 0, len(base)+len(extra))
	index := map[string]int{}
	for _, kv := range base {
		k := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			k = kv[:i]
		}
		if j, ok := index[k]; ok {
			merged[j] = kv
			continue
		}
		index[k] = len(merged)
		merged = append(merged, kv)
	}
	keys := make([]string, 0, len(extra))
	for k := range extra {
		if k == "" || strings.Contains(k, "=") {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		kv := k + "=" + extra[k]
		if j, ok := index[k]; ok {
			merged[j] = kv
			continue
		}
		index[k] = len(merged)
		merged = append(merged, kv)
	}
	return merged
}
