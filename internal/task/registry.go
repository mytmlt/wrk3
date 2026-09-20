package task

import (
	"fmt"
	"sort"
)

// Target renders a Definition into one execution environment's native
// artifact (compose YAML, swarm stack, Portainer payload, machine script).
type Target interface {
	// Name is the target identifier used on the CLI and in config.
	Name() string
	// Render emits the artifact for d. Implementations must not mutate
	// the caller's Definition beyond Normalize.
	Render(d *Definition) ([]byte, error)
}

var targets = map[string]Target{}

// RegisterTarget adds a target under its Name. Called from target files
// (compose.go, swarm.go, portainer.go, machine.go).
func RegisterTarget(t Target) {
	targets[t.Name()] = t
}

// AvailableTargets returns the sorted list of registered target names.
func AvailableTargets() []string {
	names := make([]string, 0, len(targets))
	for name := range targets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ResolveTarget returns the target for name or an error listing options.
func ResolveTarget(name string) (Target, error) {
	if t, ok := targets[name]; ok {
		return t, nil
	}
	return nil, fmt.Errorf("unknown task target %q (available targets: %v)", name, AvailableTargets())
}
