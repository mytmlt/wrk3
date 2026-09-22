package runner

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"
)

// runCmdLive runs name args... with cwd=dir and env, returning captured
// stdout/stderr. When ctx carries an output sink (see WithOutput) every
// stdout/stderr line is also teed to it live while the command runs, so
// long `compose up --build` or `make` steps stay visible instead of
// surfacing only on failure. Without a sink this behaves exactly like
// runDockerCmd (buffered, no streaming overhead for status probes).
// Error text is the caller's job: both streams are returned so callers
// keep their exact existing formats.
func runCmdLive(ctx context.Context, name, dir string, env []string, args ...string) (stdout, stderr string, err error) {
	sink := OutputFrom(ctx)
	if sink == nil {
		var outB, errB bytes.Buffer
		if err := runDockerCmd(ctx, name, dir, env, &outB, &errB, args...); err != nil {
			return outB.String(), errB.String(), err
		}
		return outB.String(), errB.String(), nil
	}
	return runStreaming(ctx, name, dir, env, sink, args...)
}

// composeLive runs `<binary> compose -p <project> -f <files>... <sub>`
// with cwd=worktreePath and env, streaming output to the ctx sink when
// present. It mirrors runCompose error text exactly (binary, args, dir,
// project, stderr tail) so failures read identically with or without a
// sink. timeout bounds the invocation; callers pass their runner timeout.
func composeLive(ctx context.Context, timeout time.Duration, binary, project string, files []string, worktreePath string, environ []string, sub ...string) (string, error) {
	args := buildComposeArgs(project, files, sub...)
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	stdout, stderr, runErr := runCmdLive(timeoutCtx, binary, worktreePath, environ, args...)
	if runErr != nil {
		return "", fmt.Errorf("%s %s (dir=%s project=%s): %w: %s",
			binary, strings.Join(args, " "), worktreePath, project,
			runErr, strings.TrimSpace(stderr))
	}
	return stdout, nil
}
