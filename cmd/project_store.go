package cmd

import (
	"path/filepath"

	"github.com/mytmlt/wrk3/internal/config"
	"github.com/mytmlt/wrk3/internal/project"
)

// newProjectStore builds the default registry store.
// Variable (not func call inline) so tests can stub it without disk.
var newProjectStore = func() (project.Store, error) {
	return project.NewFileStore()
}

// touchProject registers the current config in the global registry.
// Name derives from the repo root directory basename (config dir).
// Callers treat errors as best-effort warnings, never fatal.
func touchProject(cfg *config.Config) error {
	name := filepath.Base(filepath.Clean(cfg.RepoPath()))
	if name == "" || name == "." || name == string(filepath.Separator) {
		name = "wrk3"
	}
	store, err := newProjectStore()
	if err != nil {
		return err
	}
	return store.Touch(name, cfg.ConfigPath())
}
