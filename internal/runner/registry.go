package runner

import (
	"fmt"
	"sort"
)

// Factory builds a Runner from a config type name.
type Factory func() Runner

var registry = map[string]Factory{}

// Register adds a Runner factory under name. Called from provider files
// (e.g. docker.go).
func Register(name string, f Factory) {
	registry[name] = f
}

// Available returns the sorted list of registered runner types.
func Available() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Resolve returns the factory for typ or an error listing available options.
func Resolve(typ string) (Factory, error) {
	if f, ok := registry[typ]; ok {
		return f, nil
	}
	return nil, fmt.Errorf("unknown runner type %q (available runners: %v)", typ, Available())
}
