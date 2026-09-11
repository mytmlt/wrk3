// Package project holds the Project registry types.
//
// A Project is a named pointer to a wrk3.yaml on disk:
// {Name, ConfigPath (absolute), AddedAt}. The global registry lives at
// ~/.config/wrk3/projects.yaml (XDG-aware). Full FileStore
// implementation lands in Phase 1.
package project

import "time"

// Project is a named pointer to a wrk3.yaml config file.
type Project struct {
	Name       string    `yaml:"name"`
	ConfigPath string    `yaml:"configPath"`
	AddedAt    time.Time `yaml:"addedAt"`
}

// Store is the project registry interface. cmd depends on this
// interface only, never on FileStore directly.
type Store interface {
	Add(name, configPath string) error
	Get(name string) (*Project, error)
	List() ([]Project, error)
	Remove(name string) error
	SetCurrent(name string) error
	Current() (*Project, error)
	CurrentName() (string, error)
	Resolve(explicit string) (*Project, error)
}
