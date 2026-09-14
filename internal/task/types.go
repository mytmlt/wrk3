// Package task is the environment-agnostic definition of a stack.
//
// wrk3 analyzes docker compose (or a host plan) into a Task, then
// projects that Task onto compose, swarm, Portainer, or the host
// without modifying the source files.
package task

import (
	"fmt"
	"sort"
	"strings"
)

// Environment is a target execution environment for a Task.
type Environment string

const (
	// EnvInternal is the canonical Task YAML (no projection).
	EnvInternal Environment = "internal"
	// EnvCompose is docker compose YAML.
	EnvCompose Environment = "compose"
	// EnvSwarm is a docker stack / swarm-compatible compose file.
	EnvSwarm Environment = "swarm"
	// EnvPortainer is compose YAML suitable for a Portainer stack.
	EnvPortainer Environment = "portainer"
	// EnvHost is a host run plan (docker run / process commands).
	EnvHost Environment = "host"
)

// SourceKind is how a Task was analyzed.
type SourceKind string

const (
	// SourceCompose means the Task was built from compose files.
	SourceCompose SourceKind = "compose"
	// SourceHost means the Task was built from a host plan.
	SourceHost SourceKind = "host"
)

// Task is the portable stack definition. Runners and renderers consume
// this type; they never need to parse compose or host files themselves.
type Task struct {
	Name     string    `yaml:"name"`
	Source   Source    `yaml:"source"`
	Services []Service `yaml:"services"`
	Networks []Network `yaml:"networks,omitempty"`
	Volumes  []Volume  `yaml:"volumes,omitempty"`
}

// Source records where the Task was analyzed from. Paths stay as given
// (usually relative) so the definition is cwd-portable.
type Source struct {
	Kind  SourceKind `yaml:"kind"`
	Files []string   `yaml:"files,omitempty"`
}

// Service is one unit of work in the stack (a compose service, a
// container, or a host process).
type Service struct {
	Name               string            `yaml:"name"`
	Image              string            `yaml:"image,omitempty"`
	Build              *Build            `yaml:"build,omitempty"`
	Command            []string          `yaml:"command,omitempty"`
	CommandShell       string            `yaml:"commandShell,omitempty"`
	Entrypoint         []string          `yaml:"entrypoint,omitempty"`
	EntrypointShell    string            `yaml:"entrypointShell,omitempty"`
	WorkDir            string            `yaml:"workdir,omitempty"`
	User               string            `yaml:"user,omitempty"`
	Env                map[string]string `yaml:"env,omitempty"`
	EnvFiles           []string          `yaml:"envFiles,omitempty"`
	Ports              []Port            `yaml:"ports,omitempty"`
	Mounts             []Mount           `yaml:"mounts,omitempty"`
	DependsOn          []string          `yaml:"dependsOn,omitempty"`
	Networks           []string          `yaml:"networks,omitempty"`
	Restart            string            `yaml:"restart,omitempty"`
	Healthcheck        *Healthcheck      `yaml:"healthcheck,omitempty"`
	Replicas           int               `yaml:"replicas,omitempty"`
	Privileged         bool              `yaml:"privileged,omitempty"`
	NetworkMode        string            `yaml:"networkMode,omitempty"`
	FixedContainerName string            `yaml:"fixedContainerName,omitempty"`
}

// Build is an image build spec (compose `build:`).
type Build struct {
	Context    string            `yaml:"context"`
	Dockerfile string            `yaml:"dockerfile,omitempty"`
	Target     string            `yaml:"target,omitempty"`
	Args       map[string]string `yaml:"args,omitempty"`
}

// Port is a published or exposed port. HostVar, when set, is the env
// var a renderer should interpolate for the host side
// (`${APP_PORT:-8000}`) so parallel worktrees and remote stacks can
// remap without editing source.
type Port struct {
	Name      string `yaml:"name,omitempty"`
	Host      int    `yaml:"host,omitempty"`
	Container int    `yaml:"container"`
	Protocol  string `yaml:"protocol,omitempty"`
	HostIP    string `yaml:"hostIP,omitempty"`
	HostVar   string `yaml:"hostVar,omitempty"`
}

// Mount is a bind, named volume, or tmpfs.
type Mount struct {
	Type     string `yaml:"type"`
	Source   string `yaml:"source,omitempty"`
	Target   string `yaml:"target"`
	ReadOnly bool   `yaml:"readOnly,omitempty"`
}

// Network is a user-defined network.
type Network struct {
	Name   string `yaml:"name"`
	Driver string `yaml:"driver,omitempty"`
}

// Volume is a named volume.
type Volume struct {
	Name   string `yaml:"name"`
	Driver string `yaml:"driver,omitempty"`
}

// Healthcheck is a service health probe.
type Healthcheck struct {
	Test        []string `yaml:"test,omitempty"`
	TestShell   string   `yaml:"testShell,omitempty"`
	Interval    string   `yaml:"interval,omitempty"`
	Timeout     string   `yaml:"timeout,omitempty"`
	Retries     int      `yaml:"retries,omitempty"`
	StartPeriod string   `yaml:"startPeriod,omitempty"`
}

// Note is an analysis/projection diagnostic. Block-level notes make
// Render fail for that environment; warn/info are advisory.
type Note struct {
	Level   string `yaml:"level"`
	Service string `yaml:"service,omitempty"`
	Code    string `yaml:"code"`
	Message string `yaml:"message"`
}

const (
	levelInfo  = "info"
	levelWarn  = "warn"
	levelBlock = "block"
)

const (
	codeContainerName = "container_name"
	codeHostNetwork   = "host_network"
	codeHardcodedPort = "hardcoded_port"
	codeBindMount     = "bind_mount"
	codeMissingImage  = "missing_image"
	codePrivileged    = "privileged"
	codePortRange     = "port_range"
)

// Available returns the sorted target environment names.
func Available() []string {
	names := []string{
		string(EnvInternal),
		string(EnvCompose),
		string(EnvSwarm),
		string(EnvPortainer),
		string(EnvHost),
	}
	sort.Strings(names)
	return names
}

// ParseEnvironment resolves name or errors listing available options.
func ParseEnvironment(name string) (Environment, error) {
	n := strings.ToLower(strings.TrimSpace(name))
	switch Environment(n) {
	case EnvInternal, EnvCompose, EnvSwarm, EnvPortainer, EnvHost:
		return Environment(n), nil
	}
	return "", fmt.Errorf("unknown environment %q (available environments: %v)", name, Available())
}

// Validate checks the Task has a name and unique named services.
func (t Task) Validate() error {
	if strings.TrimSpace(t.Name) == "" {
		return fmt.Errorf("task name must not be empty")
	}
	if len(t.Services) == 0 {
		return fmt.Errorf("task %q has no services", t.Name)
	}
	seen := map[string]struct{}{}
	for i, s := range t.Services {
		name := strings.TrimSpace(s.Name)
		if name == "" {
			return fmt.Errorf("task %q services[%d]: name must not be empty", t.Name, i)
		}
		if _, ok := seen[name]; ok {
			return fmt.Errorf("task %q: duplicate service %q", t.Name, name)
		}
		seen[name] = struct{}{}
		if s.Image == "" && s.Build == nil && len(s.Command) == 0 && s.CommandShell == "" {
			return fmt.Errorf("task %q service %q: need image, build, or command", t.Name, name)
		}
		for j, p := range s.Ports {
			if p.Container <= 0 {
				return fmt.Errorf("task %q service %q ports[%d]: container port must be positive", t.Name, name, j)
			}
		}
	}
	return nil
}

// ServiceNames returns service names in Task order.
func (t Task) ServiceNames() []string {
	out := make([]string, 0, len(t.Services))
	for _, s := range t.Services {
		out = append(out, s.Name)
	}
	return out
}

// LookupService returns the named service.
func (t Task) LookupService(name string) (Service, bool) {
	for _, s := range t.Services {
		if s.Name == name {
			return s, true
		}
	}
	return Service{}, false
}
