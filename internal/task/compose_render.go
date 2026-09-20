package task

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// composeOut is the ordered compose document shape. Field order and
// yaml.v3's sorted map keys keep rendered output deterministic.
type composeOut struct {
	Name     string                       `yaml:"name,omitempty"`
	Services map[string]composeServiceOut `yaml:"services"`
	Volumes  map[string]composeTopOut     `yaml:"volumes,omitempty"`
	Networks map[string]composeTopOut     `yaml:"networks,omitempty"`
}

type composeServiceOut struct {
	Image       string            `yaml:"image,omitempty"`
	Build       *composeBuildOut  `yaml:"build,omitempty"`
	Command     any               `yaml:"command,omitempty"`
	Entrypoint  any               `yaml:"entrypoint,omitempty"`
	Environment map[string]string `yaml:"environment,omitempty"`
	Ports       []composePortOut  `yaml:"ports,omitempty"`
	Volumes     []composeMountOut `yaml:"volumes,omitempty"`
	DependsOn   []string          `yaml:"depends_on,omitempty"`
	Restart     string            `yaml:"restart,omitempty"`
	Labels      map[string]string `yaml:"labels,omitempty"`
	Healthcheck *composeHCOut     `yaml:"healthcheck,omitempty"`
	Deploy      *composeDeployOut `yaml:"deploy,omitempty"`
	Networks    []string          `yaml:"networks,omitempty"`
}

type composeBuildOut struct {
	Context    string            `yaml:"context,omitempty"`
	Dockerfile string            `yaml:"dockerfile,omitempty"`
	Target     string            `yaml:"target,omitempty"`
	Args       map[string]string `yaml:"args,omitempty"`
}

type composePortOut struct {
	HostIP    string `yaml:"host_ip,omitempty"`
	Target    int    `yaml:"target"`
	Published int    `yaml:"published,omitempty"`
	Protocol  string `yaml:"protocol,omitempty"`
	Mode      string `yaml:"mode,omitempty"`
}

type composeMountOut struct {
	Type     string `yaml:"type"`
	Source   string `yaml:"source,omitempty"`
	Target   string `yaml:"target"`
	ReadOnly bool   `yaml:"read_only,omitempty"`
}

type composeHCOut struct {
	Test        any    `yaml:"test,omitempty"`
	Interval    string `yaml:"interval,omitempty"`
	Timeout     string `yaml:"timeout,omitempty"`
	Retries     int    `yaml:"retries,omitempty"`
	StartPeriod string `yaml:"start_period,omitempty"`
}

type composeDeployOut struct {
	Replicas int    `yaml:"replicas,omitempty"`
	Mode     string `yaml:"mode,omitempty"`
}

type composeTopOut struct {
	Driver   string `yaml:"driver,omitempty"`
	External bool   `yaml:"external,omitempty"`
}

// composeTarget is the "compose" environment: plain docker/podman compose.
type composeTarget struct{}

func (composeTarget) Name() string { return "compose" }

func (composeTarget) Render(d *Definition) ([]byte, error) { return d.Compose() }

func init() { RegisterTarget(composeTarget{}) }

// Compose renders the Definition as a docker compose document. It is the
// inverse of ParseCompose for the fields this package models, so
// compose -> Definition -> compose is lossless for those fields.
func (d *Definition) Compose() ([]byte, error) {
	return renderCompose(d, false)
}

// renderCompose renders compose YAML. With swarm=true it emits a stack
// file: build-only services are rejected (swarm cannot build) and every
// service gets an explicit deploy block.
func renderCompose(d *Definition, swarm bool) ([]byte, error) {
	if d == nil {
		return nil, fmt.Errorf("task definition is nil")
	}
	d.Normalize()
	if err := d.Validate(); err != nil {
		return nil, err
	}
	if swarm {
		if err := validateSwarm(d); err != nil {
			return nil, err
		}
	}
	out := composeOut{
		Name:     d.Name,
		Services: make(map[string]composeServiceOut, len(d.Services)),
	}
	for name, svc := range d.Services {
		rendered := composeService(svc)
		if swarm {
			rendered.Restart = ""
			rendered.Deploy = swarmDeploy(svc.Deploy)
		}
		out.Services[name] = rendered
	}
	if len(d.Volumes) > 0 {
		out.Volumes = make(map[string]composeTopOut, len(d.Volumes))
		for name, v := range d.Volumes {
			out.Volumes[name] = composeTopOut{Driver: v.Driver, External: v.External}
		}
	}
	if len(d.Networks) > 0 {
		out.Networks = make(map[string]composeTopOut, len(d.Networks))
		for name, n := range d.Networks {
			out.Networks[name] = composeTopOut{Driver: n.Driver, External: n.External}
		}
	}
	raw, err := yaml.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("render compose: %w", err)
	}
	return raw, nil
}

func composeService(svc *Service) composeServiceOut {
	out := composeServiceOut{
		Image:       svc.Image,
		Command:     shellOrList(svc.Command),
		Entrypoint:  shellOrList(svc.Entrypoint),
		Environment: nonEmptyMap(svc.Environment),
		DependsOn:   svc.DependsOn,
		Restart:     svc.Restart,
		Labels:      nonEmptyMap(svc.Labels),
		Networks:    svc.Networks,
	}
	if svc.Build != nil {
		out.Build = &composeBuildOut{
			Context:    svc.Build.Context,
			Dockerfile: svc.Build.Dockerfile,
			Target:     svc.Build.Target,
			Args:       nonEmptyMap(svc.Build.Args),
		}
	}
	for _, p := range svc.Ports {
		out.Ports = append(out.Ports, composePortOut{
			HostIP:    p.HostIP,
			Target:    p.Target,
			Published: p.Published,
			Protocol:  p.Protocol,
			Mode:      p.Mode,
		})
	}
	for _, m := range svc.Volumes {
		out.Volumes = append(out.Volumes, composeMountOut(m))
	}
	if svc.Healthcheck != nil {
		out.Healthcheck = &composeHCOut{
			Test:        shellOrList(svc.Healthcheck.Test),
			Interval:    svc.Healthcheck.Interval,
			Timeout:     svc.Healthcheck.Timeout,
			Retries:     svc.Healthcheck.Retries,
			StartPeriod: svc.Healthcheck.StartPeriod,
		}
	}
	if svc.Deploy != nil {
		out.Deploy = &composeDeployOut{Replicas: svc.Deploy.Replicas, Mode: svc.Deploy.Mode}
	}
	return out
}

// shellOrList renders a one-element command as the compose shell string
// and longer commands as a list. Empty input yields nil so omitempty
// drops the field.
func shellOrList(s []string) any {
	switch len(s) {
	case 0:
		return nil
	case 1:
		return s[0]
	default:
		return s
	}
}

func nonEmptyMap(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	return m
}
