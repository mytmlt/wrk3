package forge

import (
	"fmt"
	"sort"
)

// Factory builds a Forge by provider name.
type Factory func() Forge

var registry = map[string]Factory{}

// Register adds a Forge factory under name. Called from provider files
// (e.g. github.go).
func Register(name string, f Factory) {
	registry[name] = f
}

// Available returns the sorted list of registered forge types.
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
	return nil, fmt.Errorf("unknown forge type %q (available forges: %v)", typ, Available())
}
