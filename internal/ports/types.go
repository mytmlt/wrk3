// Package ports allocates per-worktree host ports and writes .env files.
//
// Allocation rule: allocated = base + index*step per port name. Runtime
// state lives in <worktreeBase>/.wrk3-state.json with absolute paths
// so every command works from any cwd.
package ports

// PortApp is the canonical port name. It is required in every allocation:
// derived URLs (BASE_URL family) build from it and `status` shows it in
// the APP column. Any additional names in Allocator.Base are allowed and
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
type WorktreeRecord struct {
	Branch         string         `json:"branch"`
	Slug           string         `json:"slug"`
	AbsPath        string         `json:"absPath"`
	Index          int            `json:"index"`
	Ports          map[string]int `json:"ports"`
	ComposeProject string         `json:"composeProject"`
	Status         string         `json:"status"`
}

// StateFileName is the state file basename under worktreeBase.
const StateFileName = ".wrk3-state.json"
