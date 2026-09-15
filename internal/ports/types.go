// Package ports allocates per-worktree host ports and ensures managed keys
// in .env files (append-only: existing lines are never modified).
//
// Allocation rule: allocated = base + index*step per port name. Runtime
// state lives in <worktreeBase>/.wrk3-state.json with absolute paths
// so every command works from any cwd.
package ports

// PortApp is the canonical port name. It is required in every allocation:
// derived URLs (BASE_URL family) build from it and `status` lists it first
// in the PORTS column. Any additional names in Allocator.Base are allowed and
// map to generic <NAME>_PORT .env vars.
const PortApp = "app"

// DefaultStep is the default index stride (see docs/CONFIGURATION.md).
const DefaultStep = 100

// DefaultBase returns the default base ports (see docs/CONFIGURATION.md).
// Only the canonical `app` port ships by default; add more names in
// `ports.base` when the stack binds extra host ports.
func DefaultBase() map[string]int {
	return map[string]int{
		PortApp: 8000,
	}
}

// Allocation is the resolved host ports for one worktree index.
type Allocation struct {
	Index int
	Ports map[string]int
}

// Allocator computes port allocations.
type Allocator struct {
	Base map[string]int
	Step int
}

// WorktreeRecord is one entry in <worktreeBase>/.wrk3-state.json.
// AbsPath is always absolute so commands work from any cwd.
//
// Status is the last-known lifecycle state written by up/down:
// running, stopped, setting up (up in progress), stopping (down in
// progress), or failed (up error).
// Display prefers stored transitional/terminal states (setting up,
// stopping, failed) over the live runner probe so the table stays honest while
// setup/run entries are still executing. "stale" and "unknown" are
// display-only and never persisted.
type WorktreeRecord struct {
	Branch         string         `json:"branch"`
	Slug           string         `json:"slug"`
	AbsPath        string         `json:"absPath"`
	Index          int            `json:"index"`
	Ports          map[string]int `json:"ports"`
	ComposeProject string         `json:"composeProject"`
	Status         string         `json:"status"`
}

// Stored lifecycle states for WorktreeRecord.Status.
const (
	StatusRunning   = "running"
	StatusStopped   = "stopped"
	StatusSettingUp = "setting up"
	StatusStopping  = "stopping"
	StatusFailed    = "failed"
)

// StoredStatusOverridesLive reports whether a stored status must win over
// a non-running live runner probe in display (transitional setup/stop or
// terminal failure). A live "running" probe always wins over stored state
// (see ResolveDisplayStatus): out-of-band `docker compose up` clears stale
// stopped/failed entries.
func StoredStatusOverridesLive(status string) bool {
	return status == StatusSettingUp || status == StatusStopping || status == StatusFailed
}

// ShouldPersistLive reports whether a live probe result should be written
// back to the state file. Unknown probes never persist (e.g. daemon
// unreachable). Running always persists (running wins, even over setting
// up/stopping/failed). Stopped persists only over running/failed so a
// transient empty probe mid-up never clobbers an in-progress transitional
// state.
func ShouldPersistLive(stored, live string) bool {
	switch live {
	case StatusRunning:
		return stored != StatusRunning
	case StatusStopped:
		return stored == StatusRunning || stored == StatusFailed
	default:
		return false
	}
}

// ResolveDisplayStatus picks the cell shown in status/ls/dashboard given
// the stored state and a live probe result. liveErr != nil (or empty live)
// falls back to stored/unknown. Live running always wins; otherwise stored
// transitional/terminal states win over the probe.
func ResolveDisplayStatus(stored, live string, liveErr error) string {
	if liveErr == nil && live == StatusRunning {
		return StatusRunning
	}
	if StoredStatusOverridesLive(stored) {
		return stored
	}
	if liveErr != nil {
		if stored != "" {
			return stored
		}
		return "unknown"
	}
	return live
}

// StateFileName is the state file basename under worktreeBase.
const StateFileName = ".wrk3-state.json"
