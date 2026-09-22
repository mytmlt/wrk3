// Package runner abstracts where worktrees execute.
//
// v1 ships the docker and podman runners (compose -p <prefix>-<slug>,
// or -p <slug> when the prefix is empty, with
// make entry commands, cwd=worktreePath, env=allocated ports) and the
// local runner (host entry commands, no compose, ports optional).
// Portainer/Nomad stubs return "not implemented". New Runner types
// register in registry.go; unknown types error listing available
// options.
package runner

import "context"

// State is a worktree's runtime state.
type State string

const (
	// StateRunning means the worktree is up (compose containers running,
	// or the last local up succeeded).
	StateRunning State = "running"
	// StateStopped means the worktree is down (no compose containers, or
	// the last local down succeeded / never up).
	StateStopped State = "stopped"
	// StateUnknown means the state could not be determined
	// (e.g. container engine unreachable, or local with nothing to probe).
	StateUnknown State = "unknown"
)

// Status describes a worktree's runtime state.
type Status struct {
	// State is the machine-readable runtime state.
	State State
	// Running mirrors State == StateRunning for convenience.
	Running bool
	// Detail carries human context (e.g. container count, error hint).
	Detail string
}

// Options configures a compose Runner (docker or podman) for one worktree.
//
// ComposeFiles lists the -f files passed to every compose invocation.
// ProjectPrefix and Slug combine into the compose project name
// "<prefix>-<slug>" (sanitized); an empty prefix means the project name
// is just the slug (see Options.ProjectName).
type Options struct {
	ComposeFiles  []string
	ProjectPrefix string
	Slug          string
}

// Runner executes worktrees. Implementations must be safe for use from
// Phase 5 CLI wiring: Up/Down start/stop the runtime (compose for
// docker/podman; no-op for local), Exec runs host entry commands with
// cwd=worktreePath and env=allocated ports, Logs captures runtime
// logs, Status probes the runtime (unknown when there is nothing to probe).
type Runner interface {
	// Up starts the worktree (compose up -d --build; no-op for local).
	Up(ctx context.Context, worktreePath string, env map[string]string) error
	// Down stops the worktree (compose down, volumes preserved;
	// volume removal belongs to the remove path; no-op for local).
	Down(ctx context.Context, worktreePath string, env map[string]string) error
	// Logs returns compose logs for the worktree. When follow is true
	// it runs `compose logs -f` and blocks until ctx is cancelled.
	Logs(ctx context.Context, worktreePath string, follow bool) (string, error)
	// Exec runs cmd as a host process with cwd=worktreePath and
	// env applied (used for make entry commands).
	Exec(ctx context.Context, worktreePath string, cmd []string, env map[string]string) error
	// Status reports whether the worktree is running/stopped/unknown.
	Status(ctx context.Context, worktreePath string) (Status, error)
}
