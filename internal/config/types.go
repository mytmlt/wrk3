// Package config loads and validates wrk3.yaml.
//
// Config shape mirrors docs/CONFIGURATION.md (project worktreeBase,
// source git, runner docker/podman, entry setup/run/stop/logs,
// ports base+ranges with the required `app` port plus any custom names).
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
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"gopkg.in/yaml.v3"

	"github.com/mytmlt/wrk3/internal/ports"
	"github.com/mytmlt/wrk3/internal/runner"
	"github.com/mytmlt/wrk3/internal/shared"
	"github.com/mytmlt/wrk3/internal/source"
)

// Config is the root wrk3.yaml document.
type Config struct {
	Project ProjectConfig `yaml:"project"`
	Source  SourceConfig  `yaml:"source"`
	Runner  RunnerConfig  `yaml:"runner"`
	Shared  SharedConfig  `yaml:"shared"`
	Entry   EntryConfig   `yaml:"entry"`
	Ports   PortsConfig   `yaml:"ports"`
	Urls    []URLConfig   `yaml:"urls"`
	Proxy   ProxyConfig   `yaml:"proxy"`
	Health  HealthConfig  `yaml:"health"`
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
	// overwritten. `.env` is rejected (wrk3 manages that file).
	// Applies to add only, not adopt.
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

// SharedServiceConfig holds one shared service's overlay options.
// Ports lists short-syntax host publishes (e.g. ["5432:5432"]) the
// shared overlay adds so worktree containers can reach the service
// over the host gateway. Empty means no published ports.
type SharedServiceConfig struct {
	Ports []string `yaml:"ports"`
}

// SharedComposeConfig holds one engine's shared compose options.
// ComposeFiles resolve against the repo root (config dir), not a
// worktree: shared services run once per repo, not once per branch.
// Project is the fixed compose project name (sanitized); Services
// maps compose service names started by `shared up` and scoped by
// `up -d <services>` in the shared project.
type SharedComposeConfig struct {
	ComposeFiles []string                       `yaml:"composeFiles"`
	Project      string                         `yaml:"project"`
	Services     map[string]SharedServiceConfig `yaml:"services"`
}

// SharedConfig holds optional shared services (databases, brokers).
// When no shared key is set the block is disabled and every command
// behaves exactly as before. When enabled, per-worktree compose up
// starts only WorktreeServices (with --no-deps, so shared services
// are never duplicated), Env values (template-expanded per slug)
// reach worktree containers via a generated overlay plus runner env,
// and Setup commands run per worktree before entry.setup (e.g.
// create-once-per-branch databases). `down` never touches the shared
// project: only `shared down` stops it.
type SharedConfig struct {
	Docker           SharedComposeConfig `yaml:"docker"`
	Podman           SharedComposeConfig `yaml:"podman"`
	WorktreeServices []string            `yaml:"worktreeServices"`
	Env              map[string]string   `yaml:"env"`
	Setup            []string            `yaml:"setup"`
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
	Base   map[string]int    `yaml:"base"`
	Ranges map[string][2]int `yaml:"ranges"`
	// Step is legacy and always rejected: ports.step was removed in favor
	// of per-service ranges with step 1. Kept as a pointer only to detect
	// presence and error with a migration hint.
	Step *int `yaml:"step"`
}

// ProxyConfig holds the local-only gateway (<slug>.<domain> -> app port).
// Disabled by default; when enabled `up` ensures the gateway and runner
// env gains APP_URL. Domain defaults to "localhost"
// (zero-config in Chrome/Firefox/Edge; Safari/curl need hosts-sync) and
// addr defaults to "127.0.0.1:8080" (port 80 needs root).
type ProxyConfig struct {
	Enabled bool   `yaml:"enabled"`
	Domain  string `yaml:"domain"`
	Addr    string `yaml:"addr"`
}

// HealthCheck is one user-configured health probe. Run executes verbatim
// via `sh -c` with cwd=worktree and env=allocated ports (like entry.*).
// Timeout is a Go duration string (e.g. "10s"); empty means the default.
type HealthCheck struct {
	Name    string `yaml:"name"`
	Run     string `yaml:"run"`
	Timeout string `yaml:"timeout"`
}

// HealthConfig holds the optional health check list. Empty means the
// feature is off for shell checks; compose container health is still
// probed automatically when the stack defines healthchecks.
type HealthConfig struct {
	Checks []HealthCheck `yaml:"checks"`
}

// URLConfig describes one config-driven URL with a port range.
// Var is the .env variable name (e.g. "APP_URL"); Base is the URL
// with an explicit port; Range is [min, max] inclusive.
type URLConfig struct {
	Var   string `yaml:"var"`
	Base  string `yaml:"base"`
	Range [2]int `yaml:"range"`
}

// EffectiveTimeout parses Timeout, defaulting to 10s. Validation bounds
// it to 1s-120s; this accessor clamps defensively for direct callers.
func (h HealthCheck) EffectiveTimeout() time.Duration {
	if strings.TrimSpace(h.Timeout) == "" {
		return 10 * time.Second
	}
	d, err := time.ParseDuration(strings.TrimSpace(h.Timeout))
	if err != nil {
		return 10 * time.Second
	}
	return min(max(d, time.Second), 120*time.Second)
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
	return ports.Allocator{Base: c.Ports.Base, Ranges: c.Ports.Ranges}
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
// compose backend (docker or podman). When shared services are
// configured the worktree scope narrows to WorktreeServices started
// with --no-deps (shared dependencies live in the shared project),
// plus the per-slug worktree overlay when it has been written (up
// writes it before use; other commands attach it only when present
// so probes on never-started worktrees keep working).
func (c *Config) ComposeOptions(slug string) runner.Options {
	if c.Runner.Type == "podman" {
		return runner.Options{
			ComposeFiles:  append([]string(nil), c.Runner.Podman.ComposeFiles...),
			ProjectPrefix: c.Runner.Podman.ProjectPrefix,
			Slug:          slug,
			Services:      c.sharedWorktreeServices(),
			NoDeps:        c.HasShared(),
			ExtraFiles:    c.worktreeOverlayIfExists(slug),
		}
	}
	return runner.Options{
		ComposeFiles:  append([]string(nil), c.Runner.Docker.ComposeFiles...),
		ProjectPrefix: c.Runner.Docker.ProjectPrefix,
		Slug:          slug,
		Services:      c.sharedWorktreeServices(),
		NoDeps:        c.HasShared(),
		ExtraFiles:    c.worktreeOverlayIfExists(slug),
	}
}

// sharedWorktreeServices returns a copy of the worktree service scope,
// or nil when shared services are disabled (meaning: all services).
func (c *Config) sharedWorktreeServices() []string {
	if !c.HasShared() {
		return nil
	}
	return append([]string(nil), c.Shared.WorktreeServices...)
}

// worktreeOverlayIfExists returns the per-slug overlay path when the
// file has been written, else nil.
func (c *Config) worktreeOverlayIfExists(slug string) []string {
	if !c.HasShared() || strings.TrimSpace(slug) == "" {
		return nil
	}
	p := shared.WorktreeOverlayPath(c.AbsWorktreeBase(), slug)
	if st, err := os.Stat(p); err != nil || st.IsDir() {
		return nil
	}
	return []string{p}
}

// HasShared reports whether any shared-services key is set.
func (c *Config) HasShared() bool {
	return c.Shared.isSet()
}

func (s SharedConfig) isSet() bool {
	return s.Docker.isSet() || s.Podman.isSet() ||
		len(s.WorktreeServices) > 0 || len(s.Env) > 0 || len(s.Setup) > 0
}

func (s SharedComposeConfig) isSet() bool {
	return len(s.ComposeFiles) > 0 || strings.TrimSpace(s.Project) != "" || len(s.Services) > 0
}

// sharedBackendName is the compose backend selected by runner.type for
// shared operations ("" for non-compose runners).
func (c *Config) sharedBackendName() string {
	switch c.Runner.Type {
	case "docker", "podman":
		return c.Runner.Type
	default:
		return ""
	}
}

// SharedBackend returns the shared compose block for the selected
// backend. Callers must check HasShared and Validate first.
func (c *Config) SharedBackend() SharedComposeConfig {
	if c.Runner.Type == "podman" {
		return c.Shared.Podman
	}
	return c.Shared.Docker
}

// SharedProject returns the sanitized fixed compose project name for
// shared services.
func (c *Config) SharedProject() string {
	return runner.SanitizeProjectName(strings.TrimSpace(c.SharedBackend().Project))
}

// SharedServiceNames returns the sorted shared compose service names.
func (c *Config) SharedServiceNames() []string {
	out := make([]string, 0, len(c.SharedBackend().Services))
	for name := range c.SharedBackend().Services {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// SharedRunnerOptions builds runner.Options for the shared project:
// fixed project name, shared compose files, and the shared service
// scope, plus the shared overlay when written. Slug stays empty so
// the project name is exactly the shared project.
func (c *Config) SharedRunnerOptions() runner.Options {
	be := c.SharedBackend()
	var extra []string
	if p := c.SharedOverlayPath(); statExists(p) {
		extra = []string{p}
	}
	return runner.Options{
		ComposeFiles:  append([]string(nil), be.ComposeFiles...),
		ProjectPrefix: strings.TrimSpace(be.Project),
		Services:      c.SharedServiceNames(),
		ExtraFiles:    extra,
		// Wait for running/healthy before returning: per-worktree
		// isolation hooks (createdb, vhost provisioning) assume
		// the shared services are actually ready, and a published
		// host port can accept TCP while the service inside is
		// still initializing.
		Wait: true,
	}
}

// SharedOverlayPath returns the generated shared overlay path.
func (c *Config) SharedOverlayPath() string {
	return shared.SharedOverlayPath(c.AbsWorktreeBase(), c.SharedProject())
}

// WorktreeOverlayPath returns the generated per-slug overlay path.
func (c *Config) WorktreeOverlayPath(slug string) string {
	return shared.WorktreeOverlayPath(c.AbsWorktreeBase(), slug)
}

// SharedContext returns the template context for slug.
func (c *Config) SharedContext(slug string) shared.Context {
	return shared.Context{Slug: slug, SharedProject: c.SharedProject()}
}

// ExpandSharedEnv returns the shared env block with per-slug
// template variables expanded.
func (c *Config) ExpandSharedEnv(slug string) map[string]string {
	return shared.ExpandMap(c.Shared.Env, c.SharedContext(slug))
}

// ExpandSharedSetup returns the shared setup commands with per-slug
// template variables expanded (empty strings preserved; callers
// skip them like entry.setup).
func (c *Config) ExpandSharedSetup(slug string) []string {
	if len(c.Shared.Setup) == 0 {
		return nil
	}
	ctx := c.SharedContext(slug)
	out := make([]string, 0, len(c.Shared.Setup))
	for _, s := range c.Shared.Setup {
		out = append(out, shared.Expand(s, ctx))
	}
	return out
}

// SharedServicePorts returns the shared services as overlay input.
func (c *Config) SharedServicePorts() map[string]shared.ServicePorts {
	be := c.SharedBackend()
	out := make(map[string]shared.ServicePorts, len(be.Services))
	for name, svc := range be.Services {
		out[name] = shared.ServicePorts{Ports: append([]string(nil), svc.Ports...)}
	}
	return out
}

// SharedHostPorts returns the sorted distinct explicit host ports
// published by shared services (short-syntax only; ephemeral or
// unparsable publishes are skipped). Used for the readiness wait.
func (c *Config) SharedHostPorts() []int {
	seen := map[int]struct{}{}
	var out []int
	for _, svc := range c.SharedBackend().Services {
		for _, p := range shared.HostPorts(svc.Ports) {
			if _, ok := seen[p]; ok {
				continue
			}
			seen[p] = struct{}{}
			out = append(out, p)
		}
	}
	sort.Ints(out)
	return out
}

// statExists reports whether path exists as a regular file.
func statExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
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

// HasURLs reports whether any URL vars are configured.
func (c *Config) HasURLs() bool {
	return len(c.Urls) > 0
}

// URLSpecs converts configured URL entries to ports.URLSpec values.
// The config must already be validated.
func (c *Config) URLSpecs() []ports.URLSpec {
	if len(c.Urls) == 0 {
		return nil
	}
	specs := make([]ports.URLSpec, 0, len(c.Urls))
	for _, u := range c.Urls {
		parsed, _ := url.Parse(u.Base)
		specs = append(specs, ports.URLSpec{
			Var:     u.Var,
			BaseURL: parsed,
			Range:   u.Range,
		})
	}
	return specs
}

// URLBasePorts returns a map of URL var → base port (from the base URL)
// for the implicit main worktree allocation.
func (c *Config) URLBasePorts() map[string]int {
	if len(c.Urls) == 0 {
		return nil
	}
	out := make(map[string]int, len(c.Urls))
	for _, u := range c.Urls {
		parsed, _ := url.Parse(u.Base)
		if p := parsed.Port(); p != "" {
			if n, err := strconv.Atoi(p); err == nil {
				out[u.Var] = n
			}
		}
	}
	return out
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
	if c.Ports.Ranges == nil {
		c.Ports.Ranges = ports.DefaultRanges()
	}
	if c.Ports.Step != nil {
		return fmt.Errorf("ports.step was removed: delete `ports.step` and add per-service `ports.ranges` (e.g. ranges: {app: [8000, 8099]}); ports now increment by 1 with gap reuse")
	}
	if err := (ports.Allocator{Base: c.Ports.Base, Ranges: c.Ports.Ranges}).Validate(); err != nil {
		return fmt.Errorf("ports: %w", err)
	}
	if err := validateGitCopy(c.Source.Git.Copy); err != nil {
		return err
	}
	if err := c.validateURLs(); err != nil {
		return err
	}
	if err := c.validateShared(); err != nil {
		return err
	}
	if err := c.validateProxy(); err != nil {
		return err
	}
	if err := validateHealth(c.Health); err != nil {
		return err
	}
	return nil
}

// validateHealth checks health.checks entries: non-empty unique names,
// non-empty run commands, and parseable timeouts (1s-120s, empty=default).
func validateHealth(h HealthConfig) error {
	seen := map[string]struct{}{}
	for i, c := range h.Checks {
		name := strings.TrimSpace(c.Name)
		if name == "" {
			return fmt.Errorf("health.checks[%d].name must not be empty", i)
		}
		if _, ok := seen[name]; ok {
			return fmt.Errorf("health.checks[%d].name %q is duplicated", i, c.Name)
		}
		seen[name] = struct{}{}
		if strings.TrimSpace(c.Run) == "" {
			return fmt.Errorf("health.checks[%d].run must not be empty", i)
		}
		if strings.TrimSpace(c.Timeout) == "" {
			continue
		}
		d, err := time.ParseDuration(strings.TrimSpace(c.Timeout))
		if err != nil {
			return fmt.Errorf("health.checks[%d].timeout %q: %w", i, c.Timeout, err)
		}
		if d < time.Second || d > 120*time.Second {
			return fmt.Errorf("health.checks[%d].timeout %q must be 1s-120s", i, c.Timeout)
		}
	}
	return nil
}

// validateURLs checks url entries: non-empty unique var names that are
// valid .env identifiers, parseable base URLs with explicit ports
// (1–65535), valid ranges [min,max] 1–65535, base port inside range.
// Vars must not collide with managed <NAME>_PORT keys.
func (c *Config) validateURLs() error {
	if len(c.Urls) == 0 {
		return nil
	}
	seenVar := make(map[string]int, len(c.Urls))
	portVars := make(map[string]struct{})
	for name := range c.Ports.Base {
		portVars[ports.EnvVarForPort(name)] = struct{}{}
	}
	for i, u := range c.Urls {
		v := strings.TrimSpace(u.Var)
		if v == "" {
			return fmt.Errorf("urls[%d].var must not be empty", i)
		}
		if _, dup := seenVar[v]; dup {
			return fmt.Errorf("urls[%d].var %q is duplicated", i, v)
		}
		seenVar[v] = i
		if _, collision := portVars[v]; collision {
			return fmt.Errorf("urls[%d].var %q collides with managed port key", i, v)
		}
		if !isValidEnvVar(v) {
			return fmt.Errorf("urls[%d].var %q is not a valid .env variable name", i, v)
		}
		parsed, err := url.Parse(strings.TrimSpace(u.Base))
		if err != nil {
			return fmt.Errorf("urls[%d].base %q: %w", i, u.Base, err)
		}
		if parsed.Scheme == "" || parsed.Host == "" {
			return fmt.Errorf("urls[%d].base %q: must be a valid absolute URL", i, u.Base)
		}
		p := parsed.Port()
		if p == "" {
			return fmt.Errorf("urls[%d].base %q: must have an explicit port", i, u.Base)
		}
		n, err := strconv.Atoi(p)
		if err != nil || n <= 0 || n > 65535 {
			return fmt.Errorf("urls[%d].base %q: invalid port %q", i, u.Base, p)
		}
		if u.Range[0] <= 0 || u.Range[0] > 65535 || u.Range[1] <= 0 || u.Range[1] > 65535 {
			return fmt.Errorf("urls[%d].range [%d,%d]: ports must be 1–65535", i, u.Range[0], u.Range[1])
		}
		if u.Range[0] > u.Range[1] {
			return fmt.Errorf("urls[%d].range [%d,%d]: min must be <= max", i, u.Range[0], u.Range[1])
		}
		if n < u.Range[0] || n > u.Range[1] {
			return fmt.Errorf("urls[%d].base port %d outside its range [%d,%d]", i, n, u.Range[0], u.Range[1])
		}
	}
	return nil
}

// serviceNameRe matches compose service names ([a-zA-Z0-9._-],
// non-empty), used for shared service and worktree service entries.
var serviceNameRe = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// validateShared checks the optional shared-services block. A fully
// empty block disables the feature; any shared key set enables it
// and requires the backend matching runner.type plus an explicit
// per-worktree service scope. Only the matching backend block is
// honored — setting the other backend's block errors so a docker-vs-
// podman typo fails fast instead of silently doing nothing.
func (c *Config) validateShared() error {
	if !c.HasShared() {
		return nil
	}
	backend := c.sharedBackendName()
	if backend == "" {
		return fmt.Errorf("shared services require runner.type docker or podman (got %q)", c.Runner.Type)
	}
	var other SharedComposeConfig
	var otherName string
	if backend == "docker" {
		other, otherName = c.Shared.Podman, "podman"
	} else {
		other, otherName = c.Shared.Docker, "docker"
	}
	if other.isSet() {
		return fmt.Errorf("shared.%s is set but runner.type is %q (only shared.%s applies)", otherName, backend, backend)
	}
	be := c.SharedBackend()
	if len(be.ComposeFiles) == 0 {
		return fmt.Errorf("shared.%s.composeFiles must list at least one compose file", backend)
	}
	for i, f := range be.ComposeFiles {
		if strings.TrimSpace(f) == "" {
			return fmt.Errorf("shared.%s.composeFiles[%d] must not be empty", backend, i)
		}
	}
	if strings.TrimSpace(be.Project) == "" {
		return fmt.Errorf("shared.%s.project must not be empty (fixed compose project for shared services)", backend)
	}
	if len(be.Services) == 0 {
		return fmt.Errorf("shared.%s.services must list at least one service", backend)
	}
	for name, svc := range be.Services {
		if !serviceNameRe.MatchString(name) {
			return fmt.Errorf("shared.%s.services %q is not a valid compose service name", backend, name)
		}
		for i, p := range svc.Ports {
			if strings.TrimSpace(p) == "" {
				return fmt.Errorf("shared.%s.services.%s.ports[%d] must not be empty", backend, name, i)
			}
		}
	}
	if len(c.Shared.WorktreeServices) == 0 {
		return fmt.Errorf("shared.worktreeServices must list at least one service (e.g. [app]); shared services are excluded from per-worktree up")
	}
	for i, s := range c.Shared.WorktreeServices {
		if !serviceNameRe.MatchString(s) {
			return fmt.Errorf("shared.worktreeServices[%d] %q is not a valid compose service name", i, s)
		}
		if _, dup := be.Services[s]; dup {
			return fmt.Errorf("shared.worktreeServices[%d] %q is also a shared service (scopes must not overlap)", i, s)
		}
	}
	if err := c.validateSharedEnv(); err != nil {
		return err
	}
	return nil
}

// validateSharedEnv checks shared.env keys: valid .env identifiers,
// outside the WRK3_ runtime namespace, and colliding with neither
// managed <NAME>_PORT keys nor urls vars (both land in runner env).
func (c *Config) validateSharedEnv() error {
	if len(c.Shared.Env) == 0 {
		return nil
	}
	taken := make(map[string]string, len(c.Ports.Base)+len(c.Urls))
	for name := range c.Ports.Base {
		taken[ports.EnvVarForPort(name)] = "ports.base " + name
	}
	for _, u := range c.Urls {
		taken[strings.TrimSpace(u.Var)] = "urls var"
	}
	// Deterministic error order for multi-key configs.
	keys := make([]string, 0, len(c.Shared.Env))
	for k := range c.Shared.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !isValidEnvVar(k) {
			return fmt.Errorf("shared.env key %q is not a valid .env variable name", k)
		}
		if strings.HasPrefix(k, "WRK3_") {
			return fmt.Errorf("shared.env key %q uses the reserved WRK3_ namespace (runtime provides WRK3_SLUG, WRK3_SHARED_PROJECT, ...)", k)
		}
		if k == "APP_URL" && c.Proxy.Enabled {
			return fmt.Errorf("shared.env key %q collides with the proxy gateway URL (disable proxy or drop the key)", k)
		}
		if owner, ok := taken[k]; ok {
			return fmt.Errorf("shared.env key %q collides with managed %s", k, owner)
		}
	}
	return nil
}

// isValidEnvVar reports whether s is a valid .env variable name:
// starts with a letter, contains only letters, digits, and underscores.
func isValidEnvVar(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if !unicode.IsLetter(r) && r != '_' {
				return false
			}
		} else {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
				return false
			}
		}
	}
	return true
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
	if n < 1024 {
		return fmt.Errorf("proxy.addr port %d requires root; use a port >= 1024 or run with sudo", n)
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
		if clean == ".env" || filepath.Base(clean) == ".env" {
			return fmt.Errorf("source.git.copy[%d] %q must not copy .env", i, p)
		}
	}
	return nil
}
