// Package task provides an intermediate representation (IR) for runnable
// service definitions. Docker Compose files are parsed into a Task; the
// Task can then be converted to other environments (Portainer, Docker
// Swarm, direct host execution) without modifying the original source.
package task

import "time"

// ResourceLimit defines resource constraints for a service.
type ResourceLimit struct {
	CPUs    string  `yaml:"cpus,omitempty" json:"cpus,omitempty"`
	Memory  string  `yaml:"memory,omitempty" json:"memory,omitempty"`
	CPUShares int   `yaml:"cpu_shares,omitempty" json:"cpu_shares,omitempty"`
}

// HealthCheck defines a container health check.
type HealthCheck struct {
	Test        []string      `yaml:"test,omitempty" json:"test,omitempty"`
	Interval    time.Duration `yaml:"interval,omitempty" json:"interval,omitempty"`
	Timeout     time.Duration `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	Retries     int           `yaml:"retries,omitempty" json:"retries,omitempty"`
	StartPeriod time.Duration `yaml:"start_period,omitempty" json:"start_period,omitempty"`
}

// PortMapping describes a port published by a service.
type PortMapping struct {
	Host      string `yaml:"host,omitempty" json:"host,omitempty"`
	Container int    `yaml:"container" json:"container"`
	HostPort  int    `yaml:"host_port,omitempty" json:"host_port,omitempty"`
	Protocol  string `yaml:"protocol,omitempty" json:"protocol,omitempty"`
}

// VolumeMapping describes a volume mount for a service.
type VolumeMapping struct {
	Source   string `yaml:"source" json:"source"`
	Target   string `yaml:"target" json:"target"`
	ReadOnly bool   `yaml:"read_only,omitempty" json:"read_only,omitempty"`
}

// NetworkRef names a network the service connects to.
type NetworkRef struct {
	Name    string   `yaml:"name" json:"name"`
	Aliases []string `yaml:"aliases,omitempty" json:"aliases,omitempty"`
}

// EnvVar is a key=value environment variable.
type EnvVar struct {
	Key   string `yaml:"key" json:"key"`
	Value string `yaml:"value" json:"value"`
}

// Service represents one runnable service in a task definition.
type Service struct {
	Name          string            `yaml:"name" json:"name"`
	Image         string            `yaml:"image,omitempty" json:"image,omitempty"`
	Build         string            `yaml:"build,omitempty" json:"build,omitempty"`
	Command       []string          `yaml:"command,omitempty" json:"command,omitempty"`
	Entrypoint    []string          `yaml:"entrypoint,omitempty" json:"entrypoint,omitempty"`
	Environment   []EnvVar          `yaml:"environment,omitempty" json:"environment,omitempty"`
	Ports         []PortMapping     `yaml:"ports,omitempty" json:"ports,omitempty"`
	Volumes       []VolumeMapping   `yaml:"volumes,omitempty" json:"volumes,omitempty"`
	Networks      []NetworkRef      `yaml:"networks,omitempty" json:"networks,omitempty"`
	DependsOn     []string          `yaml:"depends_on,omitempty" json:"depends_on,omitempty"`
	Restart       string            `yaml:"restart,omitempty" json:"restart,omitempty"`
	WorkingDir    string            `yaml:"working_dir,omitempty" json:"working_dir,omitempty"`
	User          string            `yaml:"user,omitempty" json:"user,omitempty"`
	ResourceLimit *ResourceLimit    `yaml:"resource_limit,omitempty" json:"resource_limit,omitempty"`
	Health        *HealthCheck      `yaml:"health,omitempty" json:"health,omitempty"`
	ContainerName string           `yaml:"container_name,omitempty" json:"container_name,omitempty"`
	Labels        map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"`
	ExtraHosts    []string          `yaml:"extra_hosts,omitempty" json:"extra_hosts,omitempty"`
	DNS           []string          `yaml:"dns,omitempty" json:"dns,omitempty"`
	Privileged    bool              `yaml:"privileged,omitempty" json:"privileged,omitempty"`
}

// Network defines a network in a task definition.
type Network struct {
	Name     string            `yaml:"name" json:"name"`
	Driver   string            `yaml:"driver,omitempty" json:"driver,omitempty"`
	Internal bool              `yaml:"internal,omitempty" json:"internal,omitempty"`
	Labels   map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"`
}

// Volume defines a named volume in a task definition.
type VolumeDef struct {
	Name     string            `yaml:"name" json:"name"`
	Driver   string            `yaml:"driver,omitempty" json:"driver,omitempty"`
	External bool              `yaml:"external,omitempty" json:"external,omitempty"`
	Labels   map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"`
}

// Secret defines a secret reference in a task definition.
type Secret struct {
	Name  string `yaml:"name" json:"name"`
	File  string `yaml:"file,omitempty" json:"file,omitempty"`
	Env   string `yaml:"env,omitempty" json:"env,omitempty"`
	Label string `yaml:"label,omitempty" json:"label,omitempty"`
}

// Task is the intermediate representation of a multi-service runnable
// task. It is produced by parsing Docker Compose YAML and can be
// converted to any target environment (compose, swarm, portainer,
// direct host execution).
type Task struct {
	Name     string            `yaml:"name" json:"name"`
	Version  string            `yaml:"version,omitempty" json:"version,omitempty"`
	Services []Service         `yaml:"services" json:"services"`
	Networks []Network         `yaml:"networks,omitempty" json:"networks,omitempty"`
	Volumes  []VolumeDef       `yaml:"volumes,omitempty" json:"volumes,omitempty"`
	Secrets  []Secret          `yaml:"secrets,omitempty" json:"secrets,omitempty"`
	Labels   map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"`
}

// ServiceByName returns a pointer to the named service or nil.
func (t *Task) ServiceByName(name string) *Service {
	for i := range t.Services {
		if t.Services[i].Name == name {
			return &t.Services[i]
		}
	}
	return nil
}

// NetworkByName returns a pointer to the named network or nil.
func (t *Task) NetworkByName(name string) *Network {
	for i := range t.Networks {
		if t.Networks[i].Name == name {
			return &t.Networks[i]
		}
	}
	return nil
}

// VolumeByName returns a pointer to the named volume or nil.
func (t *Task) VolumeByName(name string) *VolumeDef {
	for i := range t.Volumes {
		if t.Volumes[i].Name == name {
			return &t.Volumes[i]
		}
	}
	return nil
}

// Merge overlays another Task's services, networks, volumes, and secrets
// onto t. Existing services are updated in place (overlay semantics);
// new services are appended. Networks, volumes, and secrets deduplicate
// by name.
func (t *Task) Merge(other *Task) {
	if other == nil {
		return
	}
	if other.Version != "" {
		t.Version = other.Version
	}
	if other.Name != "" {
		t.Name = other.Name
	}

	svcIdx := make(map[string]int, len(t.Services))
	for i := range t.Services {
		svcIdx[t.Services[i].Name] = i
	}
	for _, svc := range other.Services {
		if idx, ok := svcIdx[svc.Name]; ok {
			t.Services[idx] = mergeService(t.Services[idx], svc)
		} else {
			t.Services = append(t.Services, svc)
		}
	}

	for _, nw := range other.Networks {
		if t.NetworkByName(nw.Name) == nil {
			t.Networks = append(t.Networks, nw)
		}
	}
	for _, vol := range other.Volumes {
		if t.VolumeByName(vol.Name) == nil {
			t.Volumes = append(t.Volumes, vol)
		}
	}
	for _, sec := range other.Secrets {
		found := false
		for _, s := range t.Secrets {
			if s.Name == sec.Name {
				found = true
				break
			}
		}
		if !found {
			t.Secrets = append(t.Secrets, sec)
		}
	}
	if other.Labels != nil {
		if t.Labels == nil {
			t.Labels = make(map[string]string, len(other.Labels))
		}
		for k, v := range other.Labels {
			t.Labels[k] = v
		}
	}
}

func mergeService(base, overlay Service) Service {
	if overlay.Image != "" {
		base.Image = overlay.Image
	}
	if overlay.Build != "" {
		base.Build = overlay.Build
	}
	if overlay.Command != nil {
		base.Command = overlay.Command
	}
	if overlay.Entrypoint != nil {
		base.Entrypoint = overlay.Entrypoint
	}
	if overlay.Environment != nil {
		base.Environment = mergeEnv(base.Environment, overlay.Environment)
	}
	if overlay.Ports != nil {
		base.Ports = overlay.Ports
	}
	if overlay.Volumes != nil {
		base.Volumes = overlay.Volumes
	}
	if overlay.Networks != nil {
		base.Networks = overlay.Networks
	}
	if overlay.DependsOn != nil {
		base.DependsOn = overlay.DependsOn
	}
	if overlay.Restart != "" {
		base.Restart = overlay.Restart
	}
	if overlay.WorkingDir != "" {
		base.WorkingDir = overlay.WorkingDir
	}
	if overlay.User != "" {
		base.User = overlay.User
	}
	if overlay.ResourceLimit != nil {
		base.ResourceLimit = overlay.ResourceLimit
	}
	if overlay.Health != nil {
		base.Health = overlay.Health
	}
	if overlay.ContainerName != "" {
		base.ContainerName = overlay.ContainerName
	}
	if overlay.Labels != nil {
		if base.Labels == nil {
			base.Labels = make(map[string]string, len(overlay.Labels))
		}
		for k, v := range overlay.Labels {
			base.Labels[k] = v
		}
	}
	if overlay.ExtraHosts != nil {
		base.ExtraHosts = overlay.ExtraHosts
	}
	if overlay.DNS != nil {
		base.DNS = overlay.DNS
	}
	// privileged is a bool; overlay wins
	if overlay.Privileged {
		base.Privileged = true
	}
	return base
}

func mergeEnv(base, overlay []EnvVar) []EnvVar {
	idx := make(map[string]int, len(base))
	for i, e := range base {
		idx[e.Key] = i
	}
	for _, e := range overlay {
		if i, ok := idx[e.Key]; ok {
			base[i] = e
		} else {
			base = append(base, e)
		}
	}
	return base
}
