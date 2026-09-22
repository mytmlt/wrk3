// Package ports allocates per-worktree host ports and ensures managed keys
// in .env files (managed <NAME>_PORT keys always match the allocation;
// unmanaged lines are never modified).
//
// Allocation rule: per-service ranges scanned from base upward by 1; the
// lowest free port wins (gap reuse). URL ports (config-driven url vars
// with explicit port ranges) follow the same scan. Runtime state lives in
// <worktreeBase>/.wrk3-state.json with absolute paths so every command
// works from any cwd.
package ports

import "net/url"

// PortApp is the canonical port name. It is required in every allocation:
// the proxy gateway targets it and `status` lists it first in the PORTS
// column. Any additional names in Allocator.Base are allowed and map to
// generic <NAME>_PORT .env vars.
const PortApp = "app"

// DefaultBase returns the default base ports (see docs/CONFIGURATION.md).
// Only the canonical `app` port ships by default; add more names in
// `ports.base` when the stack binds extra host ports.
func DefaultBase() map[string]int {
	return map[string]int{
		PortApp: 8000,
	}
}

// DefaultRanges returns the default per-service port ranges
// (see docs/CONFIGURATION.md). Each range is [min, max] inclusive.
func DefaultRanges() map[string][2]int {
	return map[string][2]int{
		PortApp: {8000, 8099},
	}
}

// Allocation is the resolved host ports for one worktree index.
// Index is a monotonic id (max+1); Ports are range-allocated and
// decoupled from it.
type Allocation struct {
	Index int
	Ports map[string]int
}

// Allocator computes port allocations from per-service ranges.
type Allocator struct {
	Base   map[string]int
	Ranges map[string][2]int
}

// URLSpec describes one config-driven URL with a port range.
// Var is the .env variable name (e.g. "APP_URL"); BaseURL is the
// parsed base URL with an explicit port; Range is [min, max] inclusive.
type URLSpec struct {
	Var     string
	BaseURL *url.URL
	Range   [2]int
}

// EnvAllocation combines host port and URL port allocations for one
// worktree. Ports maps port name -> allocated port; URLs maps var -> port.
type EnvAllocation struct {
	Ports map[string]int
	URLs  map[string]int
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
	Urls           map[string]int `json:"urls,omitempty"`
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
// unreachable). Running persists (clears stopped/failed) but must never
// clobber transitional states (setting up, stopping) that an active in-band
// operation just wrote. Stopped persists only over running/failed so a
// transient empty probe mid-up never clobbers an in-progress transitional
// state.
func ShouldPersistLive(stored, live string) bool {
	switch live {
	case StatusRunning:
		return stored != StatusRunning && stored != StatusSettingUp && stored != StatusStopping
	case StatusStopped:
		return stored == StatusRunning || stored == StatusFailed
	default:
		return false
	}
}

// ResolveDisplayStatus picks the cell shown in status/ls/dashboard given
// the stored state and a live probe result. liveErr != nil (or empty live)
// falls back to stored/unknown. Transitional states (setting up, stopping)
// always win over a live running probe to prevent flicker during long-running
// operations. Live running always wins over terminal stored states (failed,
// stopped) so out-of-band recovery still works.
func ResolveDisplayStatus(stored, live string, liveErr error) string {
	if stored == StatusSettingUp || stored == StatusStopping {
		return stored
	}
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
