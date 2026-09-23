package runner

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

// defaultPodmanTimeout bounds every podman/compose invocation.
// Matches the docker default: cold-cache builds can take minutes.
const defaultPodmanTimeout = 5 * time.Minute

// podmanComposeLabelKeys lists the compose-project label keys probed by
// Status fallbacks. `podman compose` (docker-compat) sets
// com.docker.compose.project; native implementations may use
// io.podman.compose.project. Probing both keeps out-of-band stacks
// visible either way.
var podmanComposeLabelKeys = []string{
	"com.docker.compose.project",
	"io.podman.compose.project",
}

// PodmanRunner implements Runner via stdlib os/exec `podman compose`
// calls with `-p <prefix>-<slug>` (or `-p <slug>` when the prefix is
// empty), cwd=worktreePath and env=allocated
// ports plus COMPOSE_PROJECT_NAME. Shape mirrors DockerRunner; shared
// pure helpers (buildComposeArgs, buildEnv, SanitizeProjectName,
// CandidateProjects, CheckComposeFiles, countIDs, runDockerCmd) are
// reused from the same package.
type PodmanRunner struct {
	opts Options
	// Timeout bounds each podman invocation. Zero means
	// defaultPodmanTimeout.
	Timeout time.Duration
}

// NewPodman returns a PodmanRunner for opts.
func NewPodman(opts Options) *PodmanRunner {
	return &PodmanRunner{opts: opts}
}

func init() {
	Register("podman", func(o Options) Runner { return NewPodman(o) })
}

var _ Runner = (*PodmanRunner)(nil)

func (r *PodmanRunner) timeout() time.Duration {
	if r.Timeout > 0 {
		return r.Timeout
	}
	return defaultPodmanTimeout
}

// ProjectName returns the sanitized compose project name
// "<prefix>-<slug>" (empty prefix => slug-only).
func (r *PodmanRunner) ProjectName() string {
	return r.opts.ProjectName()
}

// composeArgs prefixes sub with the runner's project name and compose
// files (base files plus existing extra overlay files).
func (r *PodmanRunner) composeArgs(sub ...string) []string {
	return buildComposeArgs(r.ProjectName(), allComposeFiles(r.opts), sub...)
}

// environ builds the process environment for podman invocations.
func (r *PodmanRunner) environ(extra map[string]string) []string {
	return buildEnv(os.Environ(), r.ProjectName(), extra)
}

// Up starts the worktree via `podman compose up -d --build`. Command
// output streams to the ctx sink when present (see WithOutput).
func (r *PodmanRunner) Up(ctx context.Context, worktreePath string, env map[string]string) error {
	if strings.TrimSpace(worktreePath) == "" {
		return fmt.Errorf("podman up: empty worktree path")
	}
	if err := CheckComposeFiles(worktreePath, r.opts.ComposeFiles); err != nil {
		return fmt.Errorf("podman up: %w", err)
	}
	_, err := composeLive(ctx, r.timeout(), "podman", r.ProjectName(), allComposeFiles(r.opts), worktreePath, r.environ(env), upArgs(r.opts)...)
	return err
}

// Down stops the worktree via `podman compose down` (containers and
// networks removed, named volumes preserved; the remove path uses
// `down -v` explicitly to reclaim volumes). Output streams to the ctx
// sink when present.
func (r *PodmanRunner) Down(ctx context.Context, worktreePath string, env map[string]string) error {
	if strings.TrimSpace(worktreePath) == "" {
		return fmt.Errorf("podman down: empty worktree path")
	}
	_, err := composeLive(ctx, r.timeout(), "podman", r.ProjectName(), r.opts.ComposeFiles, worktreePath, r.environ(env), "down")
	return err
}

// Logs returns `podman compose logs` output. With follow=true it runs
// `logs -f` and blocks until ctx is cancelled.
func (r *PodmanRunner) Logs(ctx context.Context, worktreePath string, follow bool) (string, error) {
	if strings.TrimSpace(worktreePath) == "" {
		return "", fmt.Errorf("podman logs: empty worktree path")
	}
	sub := []string{"logs", "--no-color", "--tail", "500"}
	if follow {
		sub = []string{"logs", "--no-color", "-f"}
	}
	return r.runCompose(ctx, worktreePath, nil, sub...)
}

// Exec runs cmd as a host process with cwd=worktreePath and env applied
// (used for entry commands such as `podman compose up --wait`). Output
// streams to the ctx sink when present.
func (r *PodmanRunner) Exec(ctx context.Context, worktreePath string, cmd []string, env map[string]string) error {
	if strings.TrimSpace(worktreePath) == "" {
		return fmt.Errorf("podman exec: empty worktree path")
	}
	if len(cmd) == 0 || strings.TrimSpace(cmd[0]) == "" {
		return fmt.Errorf("podman exec: empty command")
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, r.timeout())
	defer cancel()
	_, stderr, err := runCmdLive(timeoutCtx, cmd[0], worktreePath,
		buildEnv(os.Environ(), r.ProjectName(), env), cmd[1:]...)
	if err != nil {
		return fmt.Errorf("exec %q (dir=%s project=%s): %w: %s",
			strings.Join(cmd, " "), worktreePath, r.ProjectName(),
			err, strings.TrimSpace(stderr))
	}
	return nil
}

// Status probes the runtime for running containers. The primary probe is
// `podman compose -p <project> -f <files>... ps -q`. When that reports no
// containers, fallback candidates cover stacks started out-of-band via
// plain `podman compose up` (default project = worktree folder name, no
// prefix, no wrk3 files): the check is label-based, so it is independent
// of compose files. Both the docker-compat label
// (com.docker.compose.project) and the native podman label
// (io.podman.compose.project) are probed. Exec failures mean unknown.
func (r *PodmanRunner) Status(ctx context.Context, worktreePath string) (Status, error) {
	if strings.TrimSpace(worktreePath) == "" {
		return Status{State: StateUnknown}, fmt.Errorf("podman status: empty worktree path")
	}
	out, err := r.runCompose(ctx, worktreePath, nil, "ps", "-q")
	if err != nil {
		return Status{State: StateUnknown, Detail: err.Error()}, err
	}
	if n := countIDs(out); n > 0 {
		return Status{
			State:   StateRunning,
			Running: true,
			Detail:  fmt.Sprintf("%d container(s)", n),
		}, nil
	}
	for _, project := range r.fallbackProjects(worktreePath) {
		for _, key := range podmanComposeLabelKeys {
			n, ferr := r.countProjectContainers(ctx, worktreePath, key, project)
			if ferr != nil {
				continue
			}
			if n > 0 {
				return Status{
					State:   StateRunning,
					Running: true,
					Detail:  fmt.Sprintf("%d container(s) (%s)", n, project),
				}, nil
			}
		}
	}
	return Status{State: StateStopped, Detail: "no running containers"}, nil
}

// fallbackProjects returns compose project name candidates beyond the
// configured "<prefix>-<slug>" for out-of-band stacks: the slug itself and
// the worktree folder basename (what plain `podman compose up` defaults
// to). The configured project comes first and is never repeated.
func (r *PodmanRunner) fallbackProjects(worktreePath string) []string {
	return FallbackProjects(r.opts, worktreePath, "")
}

// countProjectContainers counts running containers labelled with the given
// compose project label key, independent of compose files or cwd.
func (r *PodmanRunner) countProjectContainers(ctx context.Context, worktreePath, labelKey, project string) (int, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, r.timeout())
	defer cancel()
	var stdout, stderr bytes.Buffer
	if err := runDockerCmd(timeoutCtx, "podman", worktreePath, r.environ(nil),
		&stdout, &stderr, "ps",
		"--filter", "label="+labelKey+"="+project,
		"--format", "{{.ID}}"); err != nil {
		return 0, fmt.Errorf("podman ps (project=%s): %w: %s",
			project, err, strings.TrimSpace(stderr.String()))
	}
	return countIDs(stdout.String()), nil
}

// runCompose runs `podman compose -p <project> -f <files>... <sub>`
// with cwd=worktreePath and returns combined stdout.
func (r *PodmanRunner) runCompose(ctx context.Context, worktreePath string, env map[string]string, sub ...string) (string, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, r.timeout())
	defer cancel()
	args := r.composeArgs(sub...)
	var stdout, stderr bytes.Buffer
	if err := runDockerCmd(timeoutCtx, "podman", worktreePath, r.environ(env),
		&stdout, &stderr, args...); err != nil {
		return "", fmt.Errorf("podman %s (dir=%s project=%s): %w: %s",
			strings.Join(args, " "), worktreePath, r.ProjectName(),
			err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}
