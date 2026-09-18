package runner

import (
	"bytes"
	"context"
	"fmt"
	"strings"
)

// ContainerHealth is one running container's health state as reported by
// the engine. Status is one of "healthy", "unhealthy", "starting" (health
// check still running) or "none" (no healthcheck defined).
type ContainerHealth struct {
	ID     string
	Name   string
	Status string
}

// HasHealth reports whether the container defines a healthcheck.
func (h ContainerHealth) HasHealth() bool {
	return h.Status == "healthy" || h.Status == "unhealthy" || h.Status == "starting"
}

// ContainerHealths lists running containers for the worktree's compose
// project candidates with their engine health status. It covers the same
// candidates as Status (configured project plus out-of-band slug/folder
// fallbacks) so stacks started via plain `compose up` are included.
// Unknown runners (stubs) return an error; callers treat that as "no
// container data" and fall back to shell checks.
func ContainerHealths(ctx context.Context, rn Runner, worktreePath string) ([]ContainerHealth, error) {
	switch r := rn.(type) {
	case *DockerRunner:
		return r.ContainerHealths(ctx, worktreePath)
	case *PodmanRunner:
		return r.ContainerHealths(ctx, worktreePath)
	default:
		return nil, fmt.Errorf("container health: unsupported runner %T", rn)
	}
}

// ContainerHealths implements container health for docker.
func (r *DockerRunner) ContainerHealths(ctx context.Context, worktreePath string) ([]ContainerHealth, error) {
	if strings.TrimSpace(worktreePath) == "" {
		return nil, fmt.Errorf("docker container health: empty worktree path")
	}
	ids := r.listProjectIDs(ctx, worktreePath)
	if len(ids) == 0 {
		return nil, nil
	}
	return r.inspectHealth(ctx, worktreePath, ids)
}

// ContainerHealths implements container health for podman.
func (r *PodmanRunner) ContainerHealths(ctx context.Context, worktreePath string) ([]ContainerHealth, error) {
	if strings.TrimSpace(worktreePath) == "" {
		return nil, fmt.Errorf("podman container health: empty worktree path")
	}
	ids := r.listProjectIDs(ctx, worktreePath)
	if len(ids) == 0 {
		return nil, nil
	}
	return r.inspectHealth(ctx, worktreePath, ids)
}

// listProjectIDs returns unique running container IDs across all project
// candidates (configured + out-of-band fallbacks).
func (r *DockerRunner) listProjectIDs(ctx context.Context, worktreePath string) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, project := range CandidateProjects(r.opts, worktreePath, "") {
		for _, key := range []string{"com.docker.compose.project"} {
			ids, err := r.listIDs(ctx, worktreePath, key, project)
			if err != nil {
				continue
			}
			for _, id := range ids {
				if _, ok := seen[id]; !ok {
					seen[id] = struct{}{}
					out = append(out, id)
				}
			}
		}
	}
	return out
}

// listProjectIDs returns unique running container IDs across all project
// candidates and both compose label keys (docker-compat + native).
func (r *PodmanRunner) listProjectIDs(ctx context.Context, worktreePath string) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, project := range CandidateProjects(r.opts, worktreePath, "") {
		for _, key := range podmanComposeLabelKeys {
			ids, err := r.listIDs(ctx, worktreePath, key, project)
			if err != nil {
				continue
			}
			for _, id := range ids {
				if _, ok := seen[id]; !ok {
					seen[id] = struct{}{}
					out = append(out, id)
				}
			}
		}
	}
	return out
}

func (r *DockerRunner) listIDs(ctx context.Context, worktreePath, labelKey, project string) ([]string, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, r.timeout())
	defer cancel()
	var stdout, stderr bytes.Buffer
	if err := runDockerCmd(timeoutCtx, "docker", worktreePath, r.environ(nil),
		&stdout, &stderr, "ps",
		"--filter", "label="+labelKey+"="+project,
		"--format", "{{.ID}}"); err != nil {
		return nil, fmt.Errorf("docker ps (project=%s): %w: %s",
			project, err, strings.TrimSpace(stderr.String()))
	}
	return splitIDs(stdout.String()), nil
}

func (r *PodmanRunner) listIDs(ctx context.Context, worktreePath, labelKey, project string) ([]string, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, r.timeout())
	defer cancel()
	var stdout, stderr bytes.Buffer
	if err := runDockerCmd(timeoutCtx, "podman", worktreePath, r.environ(nil),
		&stdout, &stderr, "ps",
		"--filter", "label="+labelKey+"="+project,
		"--format", "{{.ID}}"); err != nil {
		return nil, fmt.Errorf("podman ps (project=%s): %w: %s",
			project, err, strings.TrimSpace(stderr.String()))
	}
	return splitIDs(stdout.String()), nil
}

func splitIDs(out string) []string {
	var ids []string
	for _, line := range strings.Split(out, "\n") {
		if id := strings.TrimSpace(line); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

// inspectHealth resolves engine health via one batched `inspect` call. A
// container that stops between `ps` and `inspect` fails the whole batch,
// so that path retries per-ID and keeps the survivors instead of losing
// every container's health.
func (r *DockerRunner) inspectHealth(ctx context.Context, worktreePath string, ids []string) ([]ContainerHealth, error) {
	if out, err := r.inspectBatch(ctx, worktreePath, "docker", ids); err == nil {
		return out, nil
	}
	return r.inspectEach(ctx, worktreePath, "docker", ids)
}

func (r *PodmanRunner) inspectHealth(ctx context.Context, worktreePath string, ids []string) ([]ContainerHealth, error) {
	if out, err := r.inspectBatch(ctx, worktreePath, "podman", ids); err == nil {
		return out, nil
	}
	return r.inspectEach(ctx, worktreePath, "podman", ids)
}

func inspectFormatArgs(ids []string) []string {
	return append([]string{"inspect", "--format", "{{.Id}} {{.Name}} {{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}"}, ids...)
}

func (r *DockerRunner) inspectBatch(ctx context.Context, worktreePath, binary string, ids []string) ([]ContainerHealth, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, r.timeout())
	defer cancel()
	var stdout, stderr bytes.Buffer
	if err := runDockerCmd(timeoutCtx, binary, worktreePath, r.environ(nil), &stdout, &stderr, inspectFormatArgs(ids)...); err != nil {
		return nil, fmt.Errorf("%s inspect health: %w: %s", binary, err, strings.TrimSpace(stderr.String()))
	}
	return parseInspectHealth(stdout.String()), nil
}

func (r *PodmanRunner) inspectBatch(ctx context.Context, worktreePath, binary string, ids []string) ([]ContainerHealth, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, r.timeout())
	defer cancel()
	var stdout, stderr bytes.Buffer
	if err := runDockerCmd(timeoutCtx, binary, worktreePath, r.environ(nil), &stdout, &stderr, inspectFormatArgs(ids)...); err != nil {
		return nil, fmt.Errorf("%s inspect health: %w: %s", binary, err, strings.TrimSpace(stderr.String()))
	}
	return parseInspectHealth(stdout.String()), nil
}

// inspectEach falls back to one inspect per container, skipping IDs that
// vanish mid-probe. An error surfaces only when every ID failed.
func (r *DockerRunner) inspectEach(ctx context.Context, worktreePath, binary string, ids []string) ([]ContainerHealth, error) {
	var out []ContainerHealth
	for _, id := range ids {
		hs, err := r.inspectBatch(ctx, worktreePath, binary, []string{id})
		if err != nil {
			continue
		}
		out = append(out, hs...)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s inspect health: all %d container(s) failed", binary, len(ids))
	}
	return out, nil
}

// inspectEach falls back to one inspect per container, skipping IDs that
// vanish mid-probe. An error surfaces only when every ID failed.
func (r *PodmanRunner) inspectEach(ctx context.Context, worktreePath, binary string, ids []string) ([]ContainerHealth, error) {
	var out []ContainerHealth
	for _, id := range ids {
		hs, err := r.inspectBatch(ctx, worktreePath, binary, []string{id})
		if err != nil {
			continue
		}
		out = append(out, hs...)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s inspect health: all %d container(s) failed", binary, len(ids))
	}
	return out, nil
}

// parseInspectHealth parses "<id> <name> <status>" lines. Kept pure for
// unit tests (no daemon needed).
func parseInspectHealth(out string) []ContainerHealth {
	var res []ContainerHealth
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		res = append(res, ContainerHealth{
			ID:     fields[0],
			Name:   strings.TrimPrefix(fields[1], "/"),
			Status: strings.ToLower(fields[len(fields)-1]),
		})
	}
	return res
}
