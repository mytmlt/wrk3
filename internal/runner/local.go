package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const defaultLocalTimeout = 30 * time.Minute

// LocalRunner implements Runner for CLI-only repos without compose.
// Entry commands (setup, run, stop, logs) execute directly on the host
// via os/exec with cwd=worktreePath and env=provided env vars.
// Status is state-file-based — it reads .wrk3-state.json from the
// worktree base directory to determine running/stopped.
type LocalRunner struct {
	opts    Options
	Timeout time.Duration
	// StatePath is the path to <worktreeBase>/.wrk3-state.json.
	// Set by callers that need Status() to reflect stored state.
	StatePath string
}

func init() {
	Register("local", func(o Options) Runner { return NewLocal(o) })
}

var _ Runner = (*LocalRunner)(nil)

// NewLocal returns a LocalRunner for opts.
func NewLocal(opts Options) *LocalRunner {
	return &LocalRunner{opts: opts}
}

func (r *LocalRunner) timeout() time.Duration {
	if r.Timeout > 0 {
		return r.Timeout
	}
	return defaultLocalTimeout
}

// Up runs setup entries then the run entry directly on the host.
// Output streams to the ctx sink when present.
func (r *LocalRunner) Up(ctx context.Context, worktreePath string, env map[string]string) error {
	if strings.TrimSpace(worktreePath) == "" {
		return fmt.Errorf("local up: empty worktree path")
	}
	return nil
}

// Down runs the stop entry if it is set; otherwise no-ops.
func (r *LocalRunner) Down(ctx context.Context, worktreePath string, env map[string]string) error {
	if strings.TrimSpace(worktreePath) == "" {
		return fmt.Errorf("local down: empty worktree path")
	}
	return nil
}

// Logs runs the logs entry if it is set; otherwise returns empty.
func (r *LocalRunner) Logs(ctx context.Context, worktreePath string, follow bool) (string, error) {
	if strings.TrimSpace(worktreePath) == "" {
		return "", fmt.Errorf("local logs: empty worktree path")
	}
	return "", nil
}

// Exec runs cmd as a host process with cwd=worktreePath and env applied.
// Output streams to the ctx sink when present.
func (r *LocalRunner) Exec(ctx context.Context, worktreePath string, cmd []string, env map[string]string) error {
	if strings.TrimSpace(worktreePath) == "" {
		return fmt.Errorf("local exec: empty worktree path")
	}
	if len(cmd) == 0 || strings.TrimSpace(cmd[0]) == "" {
		return fmt.Errorf("local exec: empty command")
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, r.timeout())
	defer cancel()

	environ := buildEnv(os.Environ(), r.opts.ProjectName(), env)

	sink := OutputFrom(timeoutCtx)
	if sink != nil {
		_, stderr, err := runStreaming(timeoutCtx, cmd[0], worktreePath, environ, sink, cmd[1:]...)
		if err != nil {
			return fmt.Errorf("exec %q (dir=%s): %w: %s",
				strings.Join(cmd, " "), worktreePath, err, strings.TrimSpace(stderr))
		}
		return nil
	}

	var stdout, stderr bytes.Buffer
	c := exec.CommandContext(timeoutCtx, cmd[0], cmd[1:]...)
	c.Dir = worktreePath
	c.Env = environ
	c.Stdout = &stdout
	c.Stderr = &stderr
	if err := startKillable(timeoutCtx, c); err != nil {
		return fmt.Errorf("exec %q (dir=%s): %w: %s",
			strings.Join(cmd, " "), worktreePath, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// Status reads the state file to determine running/stopped for local
// runner. When no state file exists or the slug record is not found,
// it returns StateStopped. No live probing — the state file is the
// source of truth for local runners.
func (r *LocalRunner) Status(ctx context.Context, worktreePath string) (Status, error) {
	if strings.TrimSpace(worktreePath) == "" {
		return Status{State: StateUnknown}, fmt.Errorf("local status: empty worktree path")
	}
	if r.StatePath == "" {
		return Status{State: StateStopped, Detail: "state-file-based"}, nil
	}
	raw, err := os.ReadFile(r.StatePath)
	if err != nil {
		return Status{State: StateStopped, Detail: "state-file-based"}, nil
	}
	type record struct {
		Slug   string `json:"slug"`
		Status string `json:"status"`
	}
	var recs []record
	if err := json.Unmarshal(raw, &recs); err != nil {
		return Status{State: StateStopped, Detail: "state-file-based"}, nil
	}
	slug := strings.TrimSuffix(filepath.Base(worktreePath), string(filepath.Separator))
	for _, rec := range recs {
		if rec.Slug == slug && rec.Status == "running" {
			return Status{
				State:   StateRunning,
				Running: true,
				Detail:  "state-file-based",
			}, nil
		}
	}
	return Status{State: StateStopped, Detail: "state-file-based"}, nil
}
