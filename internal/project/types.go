// Package project holds the lightweight auto-registered project list.
//
// A Project is a named pointer to a wrk3.yaml on disk:
// {Name, ConfigPath (absolute), LastSeen}. Names derive from the repo
// root directory basename at `wrk3 add` time. The registry lives at
// ~/.config/wrk3/projects.yaml (XDG-aware) and is best-effort:
// commands never fail hard when it is missing or corrupt beyond
// a clear error.
package project

import "time"

// Project is a named pointer to a wrk3.yaml config file.
type Project struct {
	Name       string    `yaml:"name"`
	ConfigPath string    `yaml:"configPath"`
	LastSeen   time.Time `yaml:"lastSeen"`
}

// Store is the project registry interface. cmd depends on this
// interface only, never on FileStore directly.
type Store interface {
	Touch(name, configPath string) error
	Get(name string) (*Project, error)
	List() ([]Project, error)
}
