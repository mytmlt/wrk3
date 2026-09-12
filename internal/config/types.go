// Package config loads and validates wrk3.yaml.
//
// Config shape mirrors docs/CONFIGURATION.md (project worktreeBase,
// source git, runner docker, entry setup/run/stop/logs,
// ports base+step with the required `app` port plus any custom names).
//
// Path decision: worktreeBase is resolved relative to the config file
// directory (the repo root — wrk3.yaml always lives there). Rationale:
// the config location is the source of truth for worktrees
// (`git -C <configDir> worktree add <base>/<slug>`), so no absolute
// repo path needs to be stored. Absolute worktreeBase values are used as-is.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/mytmlt/wrk3/internal/ports"
	"github.com/mytmlt/wrk3/internal/runner"
	"github.com/mytmlt/wrk3/internal/source"
)

// Config is the root wrk3.yaml document.
type Config struct {
	Project  ProjectConfig `yaml:"project"`
	Source   SourceConfig  `yaml:"source"`
	Runner   RunnerConfig  `yaml:"runner"`
	Entry    EntryConfig   `yaml:"entry"`
	Ports    PortsConfig   `yaml:"ports"`
	rawPath  string
}

// ProjectConfig holds the worktree base. The repo root is derived
// from the config file directory (see RepoPath).
type ProjectConfig struct {
	// WorktreeBase is the directory holding worktrees + state file.
	// Relative values resolve against the repo root (see AbsWorktreeBase).
	WorktreeBase string `yaml:"worktreeBase"`
}

// GitConfig holds git source options.
type GitConfig struct {
	Remote     string `yaml:"remote"`
	FetchPrune bool   `yaml:"fetchPrune"`
}

// SourceConfig selects the Source backend.
type SourceConfig struct {
	Type string    `yaml:"type"`
	Git  GitConfig `yaml:"git"`
}

// DockerConfig holds docker runner options.
type DockerConfig struct {
	ComposeFiles  []string `yaml:"composeFiles"`
	ProjectPrefix string   `yaml:"projectPrefix"`
}

// RunnerConfig selects the Runner backend.
type RunnerConfig struct {
	Type   string       `yaml:"type"`
	Docker DockerConfig `yaml:"docker"`
}

// EntryConfig holds host entry commands run inside each worktree.
type EntryConfig struct {
	Setup []string `yaml:"setup"`
	Run   string   `yaml:"run"`
	Stop  string   `yaml:"stop"`
	Logs  string   `yaml:"logs"`
}

// PortsConfig holds the port allocation table.
type PortsConfig struct {
	Base map[string]int `yaml:"base"`
	Step int            `yaml:"step"`
}

// SourceType returns the configured source backend name.
func (c *Config) SourceType() string { return c.Source.Type }

// EffectiveRemote returns the configured git remote, defaulting to
// "origin" when unset. Flag values (--remote) override this when non-empty
// (see cmd for resolution).
func (c *Config) EffectiveRemote() string {
	if c == nil {
		return "origin"
	}
	if strings.TrimSpace(c.Source.Git.Remote) == "" {
		return "origin"
	}
	return strings.TrimSpace(c.Source.Git.Remote)
}

// RunnerType returns the configured runner backend name.
func (c *Config) RunnerType() string { return c.Runner.Type }

// ConfigPath returns the path Load read this config from.
func (c *Config) ConfigPath() string { return c.rawPath }

// RepoPath returns the repo root: the directory containing wrk3.yaml.
// The config always lives in the project root, so no repo field is stored.
func (c *Config) RepoPath() string {
	return filepath.Dir(c.rawPath)
}

// AbsWorktreeBase resolves WorktreeBase against the repo root.
// Absolute values pass through; relative values join onto RepoPath.
func (c *Config) AbsWorktreeBase() string {
	if filepath.IsAbs(c.Project.WorktreeBase) {
		return filepath.Clean(c.Project.WorktreeBase)
	}
	return filepath.Clean(filepath.Join(c.RepoPath(), c.Project.WorktreeBase))
}

// StatePath returns <worktreeBase>/.wrk3-state.json (absolute).
func (c *Config) StatePath() string {
	return ports.StatePath(c.AbsWorktreeBase())
}

// Allocator returns the port allocator for this config.
func (c *Config) Allocator() ports.Allocator {
	return ports.Allocator{Base: c.Ports.Base, Step: c.Ports.Step}
}

// ComposeOptions builds runner.Options for slug.
func (c *Config) ComposeOptions(slug string) runner.Options {
	return runner.Options{
		ComposeFiles:  append([]string(nil), c.Runner.Docker.ComposeFiles...),
		ProjectPrefix: c.Runner.Docker.ProjectPrefix,
		Slug:          slug,
	}
}

// Load reads and validates the config at path.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}
	var c Config
	if err := yaml.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parse config %q: %w", path, err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve config path %q: %w", path, err)
	}
	c.rawPath = filepath.Clean(abs)
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// Validate checks the loaded config (see docs/CONFIGURATION.md).
func (c *Config) Validate() error {
	if strings.TrimSpace(c.Project.WorktreeBase) == "" {
		return fmt.Errorf("project.worktreeBase must not be empty")
	}
	// Default the git remote so EffectiveRemote is stable after load.
	if strings.TrimSpace(c.Source.Git.Remote) == "" {
		c.Source.Git.Remote = "origin"
	}
	if strings.TrimSpace(c.Source.Type) == "" {
		return fmt.Errorf("source.type must not be empty (available sources: %v)", source.Available())
	}
	if _, err := source.Resolve(c.Source.Type); err != nil {
		return err
	}
	if strings.TrimSpace(c.Runner.Type) == "" {
		return fmt.Errorf("runner.type must not be empty (available runners: %v)", runner.Available())
	}
	if _, err := runner.Resolve(c.Runner.Type); err != nil {
		return err
	}
	if c.Runner.Type == "docker" {
		if len(c.Runner.Docker.ComposeFiles) == 0 {
			return fmt.Errorf("runner.docker.composeFiles must list at least one compose file")
		}
		for i, f := range c.Runner.Docker.ComposeFiles {
			if strings.TrimSpace(f) == "" {
				return fmt.Errorf("runner.docker.composeFiles[%d] must not be empty", i)
			}
		}
		if strings.TrimSpace(c.Runner.Docker.ProjectPrefix) == "" {
			return fmt.Errorf("runner.docker.projectPrefix must not be empty")
		}
	}
	if strings.TrimSpace(c.Entry.Run) == "" {
		return fmt.Errorf("entry.run must not be empty")
	}
	if strings.TrimSpace(c.Entry.Stop) == "" {
		return fmt.Errorf("entry.stop must not be empty")
	}
	if c.Ports.Base == nil {
		c.Ports.Base = ports.DefaultBase()
	}
	if c.Ports.Step == 0 {
		c.Ports.Step = ports.DefaultStep
	}
	if err := (ports.Allocator{Base: c.Ports.Base, Step: c.Ports.Step}).Validate(); err != nil {
		return fmt.Errorf("ports: %w", err)
	}
	return nil
}
