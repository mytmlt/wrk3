// Package task is wrk3's environment-independent task definition.
//
// A Definition is built by analyzing a project's docker compose files
// (or a host process plan) and can be rendered for compose, Docker Swarm,
// Portainer, or the host — without modifying the source tree.
package task

// Runtime is how a service should execute.
type Runtime string

const (
	// RuntimeContainer means image or build (compose / swarm / Portainer).
	RuntimeContainer Runtime = "container"
	// RuntimeHost means a process started directly on the machine.
	RuntimeHost Runtime = "host"
)

// SourceKind is how the definition was produced.
type SourceKind string

const (
	SourceCompose SourceKind = "compose"
	SourceHost    SourceKind = "host"
)

// Definition is the internal task IR: services, networks, and volumes
// with no coupling to a particular runner.
type Definition struct {
	Name     string    `json:"name" yaml:"name"`
	Source   Source    `json:"source" yaml:"source"`
	Services []Service `json:"services" yaml:"services"`
	Networks []Network `json:"networks,omitempty" yaml:"networks,omitempty"`
	Volumes  []Volume  `json:"volumes,omitempty" yaml:"volumes,omitempty"`
	Notes    []string  `json:"notes,omitempty" yaml:"notes,omitempty"`
}

// Source records where the definition was derived from. Files are
// repo-relative; they are never rewritten.
type Source struct {
	Kind  SourceKind `json:"kind" yaml:"kind"`
	Files []string   `json:"files,omitempty" yaml:"files,omitempty"`
}

// Service is one unit of work in the definition.
type Service struct {
	Name          string            `json:"name" yaml:"name"`
	Runtime       Runtime           `json:"runtime" yaml:"runtime"`
	Image         string            `json:"image,omitempty" yaml:"image,omitempty"`
	Build         *Build            `json:"build,omitempty" yaml:"build,omitempty"`
	Command       Cmd               `json:"command,omitempty" yaml:"command,omitempty"`
	Entrypoint    Cmd               `json:"entrypoint,omitempty" yaml:"entrypoint,omitempty"`
	Env           map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
	EnvFiles      []string          `json:"envFiles,omitempty" yaml:"envFiles,omitempty"`
	Ports         []Port            `json:"ports,omitempty" yaml:"ports,omitempty"`
	Mounts        []Mount           `json:"mounts,omitempty" yaml:"mounts,omitempty"`
	DependsOn     []string          `json:"dependsOn,omitempty" yaml:"dependsOn,omitempty"`
	WorkingDir    string            `json:"workingDir,omitempty" yaml:"workingDir,omitempty"`
	User          string            `json:"user,omitempty" yaml:"user,omitempty"`
	Restart       string            `json:"restart,omitempty" yaml:"restart,omitempty"`
	Healthcheck   *Healthcheck      `json:"healthcheck,omitempty" yaml:"healthcheck,omitempty"`
	Networks      []string          `json:"networks,omitempty" yaml:"networks,omitempty"`
	Privileged    bool              `json:"privileged,omitempty" yaml:"privileged,omitempty"`
	NetworkMode   string            `json:"networkMode,omitempty" yaml:"networkMode,omitempty"`
	Deploy        *Deploy           `json:"deploy,omitempty" yaml:"deploy,omitempty"`
	ContainerName string            `json:"containerName,omitempty" yaml:"containerName,omitempty"`
}

// Cmd is either an argv list or a shell string (compose allows both).
type Cmd struct {
	Argv  []string `json:"argv,omitempty" yaml:"argv,omitempty"`
	Shell string   `json:"shell,omitempty" yaml:"shell,omitempty"`
}

// Empty reports whether no command was set.
func (c Cmd) Empty() bool {
	return len(c.Argv) == 0 && c.Shell == ""
}

// Build is a compose-style image build.
type Build struct {
	Context    string            `json:"context,omitempty" yaml:"context,omitempty"`
	Dockerfile string            `json:"dockerfile,omitempty" yaml:"dockerfile,omitempty"`
	Args       map[string]string `json:"args,omitempty" yaml:"args,omitempty"`
	Target     string            `json:"target,omitempty" yaml:"target,omitempty"`
}

// Port is a published host → container mapping.
type Port struct {
	HostIP    string `json:"hostIP,omitempty" yaml:"hostIP,omitempty"`
	Published string `json:"published,omitempty" yaml:"published,omitempty"`
	Target    string `json:"target" yaml:"target"`
	Protocol  string `json:"protocol,omitempty" yaml:"protocol,omitempty"`
}

// Mount is a volume or bind mount.
type Mount struct {
	Type     string `json:"type,omitempty" yaml:"type,omitempty"`
	Source   string `json:"source,omitempty" yaml:"source,omitempty"`
	Target   string `json:"target" yaml:"target"`
	ReadOnly bool   `json:"readOnly,omitempty" yaml:"readOnly,omitempty"`
}

// Healthcheck is a compose-style probe.
type Healthcheck struct {
	Test        Cmd    `json:"test,omitempty" yaml:"test,omitempty"`
	Interval    string `json:"interval,omitempty" yaml:"interval,omitempty"`
	Timeout     string `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	Retries     int    `json:"retries,omitempty" yaml:"retries,omitempty"`
	StartPeriod string `json:"startPeriod,omitempty" yaml:"startPeriod,omitempty"`
}

// Deploy is the swarm-oriented replica/restart policy.
type Deploy struct {
	Replicas      int           `json:"replicas,omitempty" yaml:"replicas,omitempty"`
	RestartPolicy RestartPolicy `json:"restartPolicy,omitempty" yaml:"restartPolicy,omitempty"`
}

// RestartPolicy maps compose restart to swarm deploy.restart_policy.
type RestartPolicy struct {
	Condition string `json:"condition,omitempty" yaml:"condition,omitempty"`
}

// Network is a named network.
type Network struct {
	Name     string `json:"name" yaml:"name"`
	Driver   string `json:"driver,omitempty" yaml:"driver,omitempty"`
	External bool   `json:"external,omitempty" yaml:"external,omitempty"`
}

// Volume is a named volume.
type Volume struct {
	Name     string `json:"name" yaml:"name"`
	External bool   `json:"external,omitempty" yaml:"external,omitempty"`
}

// ServiceByName returns the service with name, or nil.
func (d *Definition) ServiceByName(name string) *Service {
	if d == nil {
		return nil
	}
	for i := range d.Services {
		if d.Services[i].Name == name {
			return &d.Services[i]
		}
	}
	return nil
}

// addNote appends a unique note.
func (d *Definition) addNote(s string) {
	if s == "" {
		return
	}
	for _, n := range d.Notes {
		if n == s {
			return
		}
	}
	d.Notes = append(d.Notes, s)
}
