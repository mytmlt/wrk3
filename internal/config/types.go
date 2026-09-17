// Package config loads and validates wrk3.yaml.
//
// Config shape mirrors docs/CONFIGURATION.md (project worktreeBase,
// source git, runner docker/podman, entry setup/run/stop/logs,
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
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/mytmlt/wrk3/internal/ports"
	"github.com/mytmlt/wrk3/internal/runner"
	"github.com/mytmlt/wrk3/internal/source"
)

// Config is the root wrk3.yaml document.
type Config struct {
	Project ProjectConfig `yaml:"project"`
	Source  SourceConfig  `yaml:"source"`
	Runner  RunnerConfig  `yaml:"runner"`
	Entry   EntryConfig   `yaml:"entry"`
	Ports   PortsConfig   `yaml:"ports"`
	Proxy   ProxyConfig   `yaml:"proxy"`
	rawPath string
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
	// Copy lists repo-root-relative files/dirs (glob patterns allowed)
	// copied into each new worktree after git worktree add.
	// Missing sources are skipped; existing destinations are never
	// overwritten. Applies to add only, not adopt.
	Copy []string `yaml:"copy"`
}

// SourceConfig selects the Source backend.
type SourceConfig struct {
	Type string    `yaml:"type"`
	Git  GitConfig `yaml:"git"`
}

// DockerConfig holds docker runner options.
type DockerConfig struct {
	ComposeFiles []string `yaml:"composeFiles"`
	// ProjectPrefix is optional. Empty (or missing/whitespace-only, which
	// normalizes to empty) means the compose project name is just the
	// worktree slug; otherwise it is "<prefix>-<slug>" (sanitized).
	ProjectPrefix string `yaml:"projectPrefix"`
}

// PodmanConfig holds podman runner options (mirrors DockerConfig;
// `podman compose` is CLI-compatible for the wrk3-managed flags).
// ProjectPrefix is optional: empty means slug-only project names.
type PodmanConfig struct {
	ComposeFiles  []string `yaml:"composeFiles"`
	ProjectPrefix string   `yaml:"projectPrefix"`
}

// RunnerConfig selects the Runner backend.
type RunnerConfig struct {
	Type   string       `yaml:"type"`
	Docker DockerConfig `yaml:"docker"`
	Podman PodmanConfig `yaml:"podman"`
}

// EntryConfig holds host entry commands run inside each worktree.
type EntryConfig struct {
	Setup  []string `yaml:"setup"`
	Run    string   `yaml:"run"`
	Stop   string   `yaml:"stop"`
	Logs   string   `yaml:"logs"`
	Reload []string `yaml:"reload"`
}

// PortsConfig holds the port allocation table.
type PortsConfig struct {
	Base map[string]int `yaml:"base"`
	Step int            `yaml:"step"`
}

// ProxyConfig holds the local-only gateway (<slug>.<domain> -> app port).
// Disabled by default; when enabled `up` ensures the gateway and each
// worktree gains an APP_URL in its .env. Domain defaults to "localhost"
// (zero-config in Chrome/Firefox/Edge; Safari/curl need hosts-sync) and
// addr defaults to "127.0.0.1:8080" (port 80 needs root).
type ProxyConfig struct {
	Enabled bool   `yaml:"enabled"`
	Domain  string `yaml:"domain"`
	Addr    string `yaml:"addr"`
}

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

// ProxyDomain returns the effective gateway domain (default localhost).
func (c *Config) ProxyDomain() string {
	if strings.TrimSpace(c.Proxy.Domain) == "" {
		return "localhost"
	}
	return strings.ToLower(strings.Trim(strings.TrimSpace(c.Proxy.Domain), "."))
}

// ProxyAddr returns the effective gateway listen addr.
func (c *Config) ProxyAddr() string {
	if strings.TrimSpace(c.Proxy.Addr) == "" {
		return "127.0.0.1:8080"
	}
	return strings.TrimSpace(c.Proxy.Addr)
}

// ProxyURL returns http://<slug>.<domain>[:port] for a worktree.
func (c *Config) ProxyURL(slug string) string {
	return proxyURLFor(c.ProxyDomain(), c.ProxyAddr(), slug)
}

// proxyURLFor builds the public URL; port 80 is omitted. Kept unexported
// so the stdlib-only proxy package stays dependency-free of config.
func proxyURLFor(domain, addr, slug string) string {
	d := strings.ToLower(strings.Trim(strings.TrimSpace(domain), "."))
	if d == "" {
		d = "localhost"
	}
	port := 0
	if _, p, err := net.SplitHostPort(strings.TrimSpace(addr)); err == nil {
		if n, err := strconv.Atoi(p); err == nil {
			port = n
		}
	}
	if port == 80 {
		return "http://" + slug + "." + d
	}
	if port <= 0 {
		return "http://" + slug + "." + d
	}
	return "http://" + slug + "." + d + ":" + strconv.Itoa(port)
}

// ComposeOptions builds runner.Options for slug from the selected
// compose backend (docker or podman).
func (c *Config) ComposeOptions(slug string) runner.Options {
	if c.Runner.Type == "podman" {
		return runner.Options{
			ComposeFiles:  append([]string(nil), c.Runner.Podman.ComposeFiles...),
			ProjectPrefix: c.Runner.Podman.ProjectPrefix,
			Slug:          slug,
		}
	}
	return runner.Options{
		ComposeFiles:  append([]string(nil), c.Runner.Docker.ComposeFiles...),
		ProjectPrefix: c.Runner.Docker.ProjectPrefix,
		Slug:          slug,
	}
}

// ComposeFiles returns the compose files of the selected compose backend
// (docker or podman). Empty for non-compose backends.
func (c *Config) ComposeFiles() []string {
	if c.Runner.Type == "podman" {
		return c.Runner.Podman.ComposeFiles
	}
	if c.Runner.Type == "docker" {
		return c.Runner.Docker.ComposeFiles
	}
	return nil
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
		// ProjectPrefix is optional: empty/missing/whitespace-only means
		// slug-only compose project names (see runner.Options.ProjectName).
		c.Runner.Docker.ProjectPrefix = strings.TrimSpace(c.Runner.Docker.ProjectPrefix)
	}
	if c.Runner.Type == "podman" {
		if len(c.Runner.Podman.ComposeFiles) == 0 {
			return fmt.Errorf("runner.podman.composeFiles must list at least one compose file")
		}
		for i, f := range c.Runner.Podman.ComposeFiles {
			if strings.TrimSpace(f) == "" {
				return fmt.Errorf("runner.podman.composeFiles[%d] must not be empty", i)
			}
		}
		// ProjectPrefix is optional: empty/missing/whitespace-only means
		// slug-only compose project names (see runner.Options.ProjectName).
		c.Runner.Podman.ProjectPrefix = strings.TrimSpace(c.Runner.Podman.ProjectPrefix)
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
	if err := validateGitCopy(c.Source.Git.Copy); err != nil {
		return err
	}
	if err := c.validateProxy(); err != nil {
		return err
	}
	return nil
}

// validateProxy defaults empty domain/addr and validates them when the
// gateway is enabled. Disabled configs always normalize so ProxyURL and
// status helpers stay stable.
func (c *Config) validateProxy() error {
	if strings.TrimSpace(c.Proxy.Domain) == "" {
		c.Proxy.Domain = "localhost"
	} else {
		c.Proxy.Domain = strings.ToLower(strings.Trim(strings.TrimSpace(c.Proxy.Domain), "."))
	}
	if strings.TrimSpace(c.Proxy.Addr) == "" {
		c.Proxy.Addr = "127.0.0.1:8080"
	} else {
		c.Proxy.Addr = strings.TrimSpace(c.Proxy.Addr)
	}
	if !c.Proxy.Enabled {
		return nil
	}
	if err := validateProxyValues(c.Proxy.Domain, c.Proxy.Addr); err != nil {
		return err
	}
	return nil
}

// validateProxyValues checks domain labels and host:port shape without
// importing internal/proxy (config sits below proxy in the dep chain).
func validateProxyValues(domain, addr string) error {
	for _, part := range strings.Split(domain, ".") {
		if part == "" {
			return fmt.Errorf("proxy.domain %q has an empty label", domain)
		}
		if len(part) > 63 {
			return fmt.Errorf("proxy.domain %q label %q too long", domain, part)
		}
		for _, r := range part {
			ok := r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-'
			if !ok {
				return fmt.Errorf("proxy.domain %q must be hostname characters", domain)
			}
		}
		if strings.HasPrefix(part, "-") || strings.HasSuffix(part, "-") {
			return fmt.Errorf("proxy.domain %q label %q must not start/end with -", domain, part)
		}
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("proxy.addr %q must be host:port: %w", addr, err)
	}
	if strings.TrimSpace(host) == "" {
		return fmt.Errorf("proxy.addr %q must include a host", addr)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n <= 0 || n > 65535 {
		return fmt.Errorf("proxy.addr %q has invalid port", addr)
	}
	return nil
}

// validateGitCopy checks source.git.copy entries are safe repo-relative
// patterns: non-empty, not absolute, and not escaping the repo root via
// "..". Glob metacharacters (* ? [ ]) are allowed; existence is checked
// at copy time (missing sources only warn), not here.
func validateGitCopy(patterns []string) error {
	for i, p := range patterns {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			return fmt.Errorf("source.git.copy[%d] must not be empty", i)
		}
		if filepath.IsAbs(trimmed) {
			return fmt.Errorf("source.git.copy[%d] %q must be repo-relative, not absolute", i, p)
		}
		// Strip a single trailing slash (dir marker) before cleaning so
		// "certs/" validates as "certs" without tripping prefix checks.
		noSlash := strings.TrimSuffix(trimmed, "/")
		clean := filepath.Clean(noSlash)
		if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return fmt.Errorf("source.git.copy[%d] %q must not escape the repo root", i, p)
		}
	}
	return nil
}
