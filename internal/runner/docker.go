package runner

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// defaultDockerTimeout bounds every docker/compose invocation.
// Up --build on a cold cache can take minutes, so the default is
// generous; callers can tighten it via context or DockerRunner.Timeout.
const defaultDockerTimeout = 5 * time.Minute

// DockerRunner implements Runner via stdlib os/exec `docker compose`
// calls with `-p <prefix>-<slug>` (or `-p <slug>` when the prefix is
// empty), cwd=worktreePath and env=allocated
// ports plus COMPOSE_PROJECT_NAME.
type DockerRunner struct {
	opts Options
	// Timeout bounds each docker invocation. Zero means
	// defaultDockerTimeout.
	Timeout time.Duration
}

// New returns a DockerRunner for opts.
func New(opts Options) *DockerRunner {
	return &DockerRunner{opts: opts}
}

func init() {
	Register("docker", func(o Options) Runner { return New(o) })
}

var _ Runner = (*DockerRunner)(nil)

func (r *DockerRunner) timeout() time.Duration {
	if r.Timeout > 0 {
		return r.Timeout
	}
	return defaultDockerTimeout
}

// ProjectName returns the sanitized compose project name
// "<prefix>-<slug>" (empty prefix => slug-only).
func (r *DockerRunner) ProjectName() string {
	return r.opts.ProjectName()
}

// ProjectName returns the sanitized compose project name
// "<prefix>-<slug>" (empty prefix => slug-only; empty both => "wrk3").
func (o Options) ProjectName() string {
	combined := ""
	switch {
	case o.ProjectPrefix != "" && o.Slug != "":
		combined = o.ProjectPrefix + "-" + o.Slug
	case o.ProjectPrefix != "":
		combined = o.ProjectPrefix
	case o.Slug != "":
		combined = o.Slug
	default:
		combined = "wrk3"
	}
	return SanitizeProjectName(combined)
}

// SanitizeProjectName lowercases s and replaces every run of characters
// outside [a-z0-9_.-] with a single "-". Leading/trailing "-_." are
// trimmed; empty results fall back to "wrk3". Uppercase, slashes,
// spaces and colons (e.g. "Feature/foo Bar:1") thus become
// "feature-foo-bar-1".
func SanitizeProjectName(s string) string {
	lower := strings.ToLower(s)
	var b strings.Builder
	b.Grow(len(lower))
	prevDash := false
	for _, r := range lower {
		ok := r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '.' || r == '-'
		if ok {
			b.WriteRune(r)
			prevDash = r == '-'
			continue
		}
		if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	out := strings.Trim(b.String(), "-_.")
	if out == "" {
		return "wrk3"
	}
	return out
}

// buildComposeArgs builds ["compose", "-p", project, "-f", f...,
// sub...] for `docker <args>`. Exported logic lives in this pure
// helper so unit tests cover command construction without a daemon.
func buildComposeArgs(project string, files []string, sub ...string) []string {
	args := []string{"compose", "-p", project}
	for _, f := range files {
		if f == "" {
			continue
		}
		args = append(args, "-f", f)
	}
	return append(args, sub...)
}

// composeArgs prefixes sub with the runner's project name and compose
// files.
func (r *DockerRunner) composeArgs(sub ...string) []string {
	return buildComposeArgs(r.ProjectName(), r.opts.ComposeFiles, sub...)
}

// mergeEnv merges base (typically os.Environ()) with extra. Existing keys
// are replaced in place (no duplicates); new keys from extra are appended
// sorted by key for determinism. Empty extra keys and keys containing "="
// are skipped. Unlike buildEnv this does not force COMPOSE_PROJECT_NAME.
func mergeEnv(base []string, extra map[string]string) []string {
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

// buildEnv is mergeEnv plus COMPOSE_PROJECT_NAME forced to project so
// callers cannot break compose project isolation via env.
func buildEnv(base []string, project string, extra map[string]string) []string {
	merged := mergeEnv(base, extra)
	index := map[string]int{}
	for i, kv := range merged {
		k := kv
		if j := strings.IndexByte(kv, '='); j >= 0 {
			k = kv[:j]
		}
		index[k] = i
	}
	kv := "COMPOSE_PROJECT_NAME=" + project
	if j, ok := index["COMPOSE_PROJECT_NAME"]; ok {
		merged[j] = kv
	} else {
		merged = append(merged, kv)
	}
	return merged
}

// environ builds the process environment for docker invocations.
func (r *DockerRunner) environ(extra map[string]string) []string {
	return buildEnv(os.Environ(), r.ProjectName(), extra)
}

// runDockerCmd runs name args... with cwd=dir and env, capturing stdout
// and stderr. The child runs in its own process group and the whole
// group dies on ctx end (see startKillable), so a wedged daemon can
// delay the call up to the context deadline but never wedge it forever.
func runDockerCmd(ctx context.Context, name, dir string, env []string, stdout, stderr *bytes.Buffer, args ...string) error {
	c := exec.Command(name, args...)
	c.Dir = dir
	c.Env = env
	c.Stdout = stdout
	c.Stderr = stderr
	return startKillable(ctx, c)
}

// Up starts the worktree via `docker compose up -d --build`.
func (r *DockerRunner) Up(ctx context.Context, worktreePath string, env map[string]string) error {
	if strings.TrimSpace(worktreePath) == "" {
		return fmt.Errorf("docker up: empty worktree path")
	}
	if err := CheckComposeFiles(worktreePath, r.opts.ComposeFiles); err != nil {
		return fmt.Errorf("docker up: %w", err)
	}
	_, err := r.runCompose(ctx, worktreePath, env, "up", "-d", "--build")
	return err
}

// Down stops the worktree via `docker compose down` (containers and
// networks removed, named volumes preserved; the remove path uses
// `down -v` explicitly to reclaim volumes).
func (r *DockerRunner) Down(ctx context.Context, worktreePath string, env map[string]string) error {
	if strings.TrimSpace(worktreePath) == "" {
		return fmt.Errorf("docker down: empty worktree path")
	}
	_, err := r.runCompose(ctx, worktreePath, env, "down")
	return err
}

// Logs returns `docker compose logs` output. With follow=true it runs
// `logs -f` and blocks until ctx is cancelled.
func (r *DockerRunner) Logs(ctx context.Context, worktreePath string, follow bool) (string, error) {
	if strings.TrimSpace(worktreePath) == "" {
		return "", fmt.Errorf("docker logs: empty worktree path")
	}
	sub := []string{"logs", "--no-color", "--tail", "500"}
	if follow {
		sub = []string{"logs", "--no-color", "-f"}
	}
	return r.runCompose(ctx, worktreePath, nil, sub...)
}

// Exec runs cmd as a host process with cwd=worktreePath and env applied
// (used for entry commands such as `docker compose up --wait`).
func (r *DockerRunner) Exec(ctx context.Context, worktreePath string, cmd []string, env map[string]string) error {
	if strings.TrimSpace(worktreePath) == "" {
		return fmt.Errorf("docker exec: empty worktree path")
	}
	if len(cmd) == 0 || strings.TrimSpace(cmd[0]) == "" {
		return fmt.Errorf("docker exec: empty command")
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, r.timeout())
	defer cancel()
	var stdout, stderr bytes.Buffer
	if err := runDockerCmd(timeoutCtx, cmd[0], worktreePath,
		buildEnv(os.Environ(), r.ProjectName(), env), &stdout, &stderr, cmd[1:]...); err != nil {
		return fmt.Errorf("exec %q (dir=%s project=%s): %w: %s",
			strings.Join(cmd, " "), worktreePath, r.ProjectName(),
			err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// Status probes the runtime for running containers. The primary probe is
// `docker compose -p <project> -f <files>... ps -q`. When that reports no
// containers, fallback candidates cover stacks started out-of-band via
// plain `docker compose up` (default project = worktree folder name, no
// prefix, no wrk3 files): the check is label-based
// (`docker ps --filter label=com.docker.compose.project=...`), so it is
// independent of compose files. Exec failures mean unknown.
func (r *DockerRunner) Status(ctx context.Context, worktreePath string) (Status, error) {
	if strings.TrimSpace(worktreePath) == "" {
		return Status{State: StateUnknown}, fmt.Errorf("docker status: empty worktree path")
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
		n, ferr := r.countProjectContainers(ctx, worktreePath, project)
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
	return Status{State: StateStopped, Detail: "no running containers"}, nil
}

// fallbackProjects returns compose project name candidates beyond the
// configured "<prefix>-<slug>" for out-of-band stacks: the slug itself and
// the worktree folder basename (what plain `docker compose up` defaults
// to). The configured project comes first and is never repeated.
func (r *DockerRunner) fallbackProjects(worktreePath string) []string {
	return FallbackProjects(r.opts, worktreePath, "")
}

// CandidateProjects lists the compose project names to probe for a
// worktree: configured project first, then slug and folder basename
// fallbacks (sanitized, deduplicated). storedProject covers records whose
// saved project predates the current config.
func CandidateProjects(opts Options, worktreePath, storedProject string) []string {
	var out []string
	seen := map[string]struct{}{}
	add := func(s string) {
		s = SanitizeProjectName(strings.TrimSpace(s))
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	add(opts.ProjectName())
	add(opts.Slug)
	if base := filepath.Base(filepath.Clean(worktreePath)); base != "" && base != "." && base != "/" {
		add(base)
	}
	add(storedProject)
	return out
}

// FallbackProjects is CandidateProjects without the already-probed primary
// (used by Status after `compose ps -q` covers the configured project).
func FallbackProjects(opts Options, worktreePath, storedProject string) []string {
	out := CandidateProjects(opts, worktreePath, storedProject)
	primary := opts.ProjectName()
	if len(out) > 0 && out[0] == primary {
		return out[1:]
	}
	return out
}

// countProjectContainers counts running containers labelled with the given
// compose project, independent of compose files or cwd.
func (r *DockerRunner) countProjectContainers(ctx context.Context, worktreePath, project string) (int, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, r.timeout())
	defer cancel()
	var stdout, stderr bytes.Buffer
	if err := runDockerCmd(timeoutCtx, "docker", worktreePath, r.environ(nil),
		&stdout, &stderr, "ps",
		"--filter", "label=com.docker.compose.project="+project,
		"--format", "{{.ID}}"); err != nil {
		return 0, fmt.Errorf("docker ps (project=%s): %w: %s",
			project, err, strings.TrimSpace(stderr.String()))
	}
	return countIDs(stdout.String()), nil
}

// countIDs counts non-empty lines of container ID output.
func countIDs(out string) int {
	n := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

// runCompose runs `docker compose -p <project> -f <files>... <sub>`
// with cwd=worktreePath and returns combined stdout.
func (r *DockerRunner) runCompose(ctx context.Context, worktreePath string, env map[string]string, sub ...string) (string, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, r.timeout())
	defer cancel()
	args := r.composeArgs(sub...)
	var stdout, stderr bytes.Buffer
	if err := runDockerCmd(timeoutCtx, "docker", worktreePath, r.environ(env),
		&stdout, &stderr, args...); err != nil {
		return "", fmt.Errorf("docker %s (dir=%s project=%s): %w: %s",
			strings.Join(args, " "), worktreePath, r.ProjectName(),
			err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}
