// Package task defines an environment-agnostic description of a runnable
// workload — the "task definition" — plus importers and exporters that
// move it between source formats and target environments.
//
// Definition is the pivot: docker compose documents and `docker run`
// command lists both parse into it, and the compose, swarm, Portainer and
// bare-machine targets render from it. That lets wrk3 analyze an existing
// compose project and run it unchanged in another environment (and back
// again) without editing the source.
package task

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Protocol values for Port.Protocol.
const (
	ProtocolTCP = "tcp"
	ProtocolUDP = "udp"
)

// Port modes mirror the compose `ports[].mode` field (swarm routing).
const (
	ModeHost    = "host"
	ModeIngress = "ingress"
)

// Mount types mirror the compose `volumes[].type` field.
const (
	MountVolume = "volume"
	MountBind   = "bind"
	MountTmpfs  = "tmpfs"
)

// Deploy modes mirror the compose `deploy.mode` field.
const (
	DeployModeReplicated = "replicated"
	DeployModeGlobal     = "global"
)

// Definition is an environment-agnostic task graph. Services are keyed by
// name; Volumes and Networks are the named top-level resources they
// reference.
type Definition struct {
	// Name is an optional project/stack name used by targets that need
	// one (swarm stack name, Portainer stack name, machine prefix).
	Name     string
	Services map[string]*Service
	Volumes  map[string]*Volume
	Networks map[string]*Network
}

// Service is one runnable unit of a Definition.
type Service struct {
	// Image is the container image reference. Build, when set, means the
	// image is produced from source instead of pulled.
	Image string
	Build *Build
	// Command and Entrypoint override the image defaults. A single
	// element is the compose "shell form" string.
	Command    []string
	Entrypoint []string
	// Environment holds literal KEY=value pairs. Values may reference
	// runtime variables via ${VAR}; they are not interpolated here.
	Environment map[string]string
	Ports       []Port
	Volumes     []Mount
	// DependsOn lists service names this service starts after, in
	// deterministic order after Normalize.
	DependsOn   []string
	Restart     string
	Labels      map[string]string
	Healthcheck *Healthcheck
	Deploy      *Deploy
	// Networks lists named top-level networks this service joins. Empty
	// means the target default network.
	Networks []string
}

// Build describes an image built from source.
type Build struct {
	Context    string
	Dockerfile string
	Target     string
	Args       map[string]string
}

// Port is one published/target port pair.
type Port struct {
	// HostIP is an optional bind address (compose "127.0.0.1:8080:80").
	HostIP string
	// Published is the host port; 0 means an ephemeral/unspecified port.
	Published int
	// Target is the container port. Always set for a valid port.
	Target   int
	Protocol string
	// Mode is "", ModeHost or ModeIngress.
	Mode string
}

// Mount is one volume/bind/tmpfs attachment.
type Mount struct {
	// Type is MountVolume, MountBind or MountTmpfs.
	Type string
	// Source is the named volume or host path; empty for tmpfs/anonymous.
	Source   string
	Target   string
	ReadOnly bool
}

// Healthcheck mirrors the compose healthcheck block.
type Healthcheck struct {
	// Test is the probe argv (compose list form) or a single shell
	// string (compose string form).
	Test        []string
	Interval    string
	Timeout     string
	Retries     int
	StartPeriod string
}

// Deploy mirrors the subset of compose `deploy` needed by swarm stacks.
type Deploy struct {
	// Replicas is 0 when unspecified.
	Replicas int
	// Mode is "", "replicated" or "global".
	Mode string
}

// Volume is a named top-level volume.
type Volume struct {
	Driver   string
	External bool
}

// Network is a named top-level network.
type Network struct {
	Driver   string
	External bool
}

// ServiceNames returns the service names in stable (sorted) order.
func (d *Definition) ServiceNames() []string {
	if d == nil {
		return nil
	}
	names := make([]string, 0, len(d.Services))
	for name := range d.Services {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Normalize fills defaults and sorts slice fields so rendered output is
// deterministic. It is idempotent and safe on nil receivers.
func (d *Definition) Normalize() {
	if d == nil {
		return
	}
	if d.Services == nil {
		d.Services = map[string]*Service{}
	}
	if d.Volumes == nil {
		d.Volumes = map[string]*Volume{}
	}
	if d.Networks == nil {
		d.Networks = map[string]*Network{}
	}
	for _, svc := range d.Services {
		if svc == nil {
			continue
		}
		if svc.Environment == nil {
			svc.Environment = map[string]string{}
		}
		if svc.Labels == nil {
			svc.Labels = map[string]string{}
		}
		for i := range svc.Ports {
			if svc.Ports[i].Protocol == "" {
				svc.Ports[i].Protocol = ProtocolTCP
			}
		}
		sort.Slice(svc.Ports, func(i, j int) bool { return portKey(svc.Ports[i]) < portKey(svc.Ports[j]) })
		for i := range svc.Volumes {
			if svc.Volumes[i].Type == "" {
				svc.Volumes[i].Type = inferMountType(svc.Volumes[i].Source)
			}
		}
		sort.Slice(svc.Volumes, func(i, j int) bool { return mountKey(svc.Volumes[i]) < mountKey(svc.Volumes[j]) })
		sort.Strings(svc.DependsOn)
		sort.Strings(svc.Networks)
	}
}

// inferMountType guesses the compose mount type from the source path:
// absolute or relative paths are binds, everything else is a named volume.
func inferMountType(source string) string {
	if source == "" {
		return MountVolume
	}
	if strings.HasPrefix(source, "/") || strings.HasPrefix(source, ".") || strings.HasPrefix(source, "~") {
		return MountBind
	}
	return MountVolume
}

// Validate checks structural invariants: every service has an image or a
// build context, ports/healthchecks are well-formed, and dependency,
// volume and network references resolve.
func (d *Definition) Validate() error {
	if d == nil {
		return fmt.Errorf("task definition is nil")
	}
	for _, name := range d.ServiceNames() {
		svc := d.Services[name]
		if svc == nil {
			return fmt.Errorf("service %q is empty", name)
		}
		if err := svc.validate(name); err != nil {
			return err
		}
		for _, dep := range svc.DependsOn {
			if _, ok := d.Services[dep]; !ok {
				return fmt.Errorf("service %q depends on unknown service %q", name, dep)
			}
		}
		for _, net := range svc.Networks {
			if _, ok := d.Networks[net]; !ok {
				return fmt.Errorf("service %q references unknown network %q", name, net)
			}
		}
		for _, m := range svc.Volumes {
			if m.Type != MountVolume || m.Source == "" {
				continue
			}
			if _, ok := d.Volumes[m.Source]; !ok {
				return fmt.Errorf("service %q references unknown volume %q", name, m.Source)
			}
		}
	}
	return nil
}

func (s *Service) validate(name string) error {
	if strings.TrimSpace(s.Image) == "" && (s.Build == nil || strings.TrimSpace(s.Build.Context) == "") {
		return fmt.Errorf("service %q must set image or build.context", name)
	}
	for i, p := range s.Ports {
		if p.Target <= 0 || p.Target > 65535 {
			return fmt.Errorf("service %q ports[%d]: target %d out of range 1-65535", name, i, p.Target)
		}
		if p.Published < 0 || p.Published > 65535 {
			return fmt.Errorf("service %q ports[%d]: published %d out of range 0-65535", name, i, p.Published)
		}
		switch p.Protocol {
		case "", ProtocolTCP, ProtocolUDP:
		default:
			return fmt.Errorf("service %q ports[%d]: unknown protocol %q", name, i, p.Protocol)
		}
		switch p.Mode {
		case "", ModeHost, ModeIngress:
		default:
			return fmt.Errorf("service %q ports[%d]: unknown mode %q", name, i, p.Mode)
		}
	}
	for i, m := range s.Volumes {
		if strings.TrimSpace(m.Target) == "" {
			return fmt.Errorf("service %q volumes[%d]: target must not be empty", name, i)
		}
		switch m.Type {
		case "", MountVolume, MountBind, MountTmpfs:
		default:
			return fmt.Errorf("service %q volumes[%d]: unknown type %q", name, i, m.Type)
		}
	}
	return nil
}

// Order returns service names in dependency order (dependencies first),
// breaking ties alphabetically so output is deterministic. Cycles are
// broken at the first repeated node rather than failing.
func (d *Definition) Order() []string {
	if d == nil {
		return nil
	}
	visited := map[string]int{} // 0=unseen, 1=visiting, 2=done
	var out []string
	var visit func(string)
	visit = func(name string) {
		switch visited[name] {
		case 1, 2:
			return
		}
		visited[name] = 1
		svc := d.Services[name]
		if svc != nil {
			deps := append([]string(nil), svc.DependsOn...)
			sort.Strings(deps)
			for _, dep := range deps {
				if _, ok := d.Services[dep]; ok {
					visit(dep)
				}
			}
		}
		visited[name] = 2
		out = append(out, name)
	}
	for _, name := range d.ServiceNames() {
		visit(name)
	}
	return out
}

// portKey is the sort key for deterministic port ordering.
func portKey(p Port) string {
	return fmt.Sprintf("%05d:%05d/%s/%s/%s", p.Published, p.Target, p.Protocol, p.Mode, p.HostIP)
}

// mountKey is the sort key for deterministic mount ordering.
func mountKey(m Mount) string {
	return strings.Join([]string{m.Type, m.Source, m.Target, strconv.FormatBool(m.ReadOnly)}, "\x00")
}
