// Package runner abstracts where worktrees execute.
//
// v1 ships the docker and podman runners (compose -p <prefix>-<slug>,
// or -p <slug> when the prefix is empty, with
// make entry commands, cwd=worktreePath, env=allocated ports).
// Portainer/Nomad stubs return "not implemented". New Runner types
// register in registry.go; unknown types error listing available
// options.
package runner

import "context"

// State is a worktree's runtime state.
type State string

const (
	// StateRunning means at least one compose container is up.
	StateRunning State = "running"
	// StateStopped means no compose containers are up.
	StateStopped State = "stopped"
	// StateUnknown means the state could not be determined
	// (e.g. container engine unreachable).
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
//
// ExtraFiles lists additional -f files appended after ComposeFiles
// (e.g. wrk3-generated overlays); missing files are the caller's
// problem, so only attach paths that exist.
//
// Services, when non-empty, scopes Up to those services
// (`up -d --build [services...]`); empty means all services.
// NoDeps adds --no-deps to Up so only Services start (their depends_on
// and links targets are left alone — used for per-worktree projects
// whose dependencies live in a shared compose project).
// Wait adds --wait to Up so it returns after containers are
// running/healthy instead of right after start (used for the shared
// project, whose dependents assume readiness).
type Options struct {
	ComposeFiles  []string
	ProjectPrefix string
	Slug          string
	ExtraFiles    []string
	Services      []string
	NoDeps        bool
	Wait          bool
}

// Runner executes worktrees. Implementations must be safe for use from
// Phase 5 CLI wiring: Up/Down run compose, Exec runs host entry
// commands (e.g. make targets) with cwd=worktreePath and env=allocated
// ports, Logs captures compose logs, Status probes compose ps.
type Runner interface {
	// Up starts the worktree (compose up -d --build).
	Up(ctx context.Context, worktreePath string, env map[string]string) error
	// Down stops the worktree (compose down, volumes preserved;
	// volume removal belongs to the remove path).
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
