package source

import (
	"fmt"
	"sort"
)

// Factory builds a Source from a config type name.
type Factory func() Source

var registry = map[string]Factory{}

// Register adds a Source factory under name. Called from provider files
// (e.g. git.go).
func Register(name string, f Factory) {
	registry[name] = f
}

// Available returns the sorted list of registered source types.
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
	return nil, fmt.Errorf("unknown source type %q (available sources: %v)", typ, Available())
}
