package task

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Format is a render target for a Definition.
type Format string

const (
	FormatYAML      Format = "yaml"
	FormatJSON      Format = "json"
	FormatCompose   Format = "compose"
	FormatSwarm     Format = "swarm"
	FormatPortainer Format = "portainer"
	FormatHost      Format = "host"
)

// Formats lists supported --format values.
func Formats() []string {
	return []string{
		string(FormatYAML),
		string(FormatJSON),
		string(FormatCompose),
		string(FormatSwarm),
		string(FormatPortainer),
		string(FormatHost),
	}
}

// ParseFormat maps a user string to Format.
func ParseFormat(s string) (Format, error) {
	f := Format(strings.ToLower(strings.TrimSpace(s)))
	switch f {
	case FormatYAML, FormatJSON, FormatCompose, FormatSwarm, FormatPortainer, FormatHost:
		return f, nil
	case "":
		return FormatYAML, nil
	default:
		return "", fmt.Errorf("unknown task format %q (available formats: %s)", s, strings.Join(Formats(), ", "))
	}
}

// Encode renders the definition. Source files are never written.
func (d *Definition) Encode(format Format, swarm bool) ([]byte, error) {
	if d == nil {
		return nil, fmt.Errorf("task definition is nil")
	}
	switch format {
	case FormatYAML, "":
		return yaml.Marshal(d)
	case FormatJSON:
		return json.MarshalIndent(d, "", "  ")
	case FormatCompose:
		return ToCompose(d)
	case FormatSwarm:
		return ToSwarm(d)
	case FormatPortainer:
		stack, err := ToPortainer(d, swarm)
		if err != nil {
			return nil, err
		}
		return json.MarshalIndent(stack, "", "  ")
	case FormatHost:
		plan, err := ToHost(d)
		if err != nil {
			return nil, err
		}
		return yaml.Marshal(plan)
	default:
		return nil, fmt.Errorf("unknown task format %q (available formats: %s)", format, strings.Join(Formats(), ", "))
	}
}

// ToCompose renders a normalized compose document. container_name is
// omitted so parallel worktrees keep compose project isolation.
func ToCompose(d *Definition) ([]byte, error) {
	if d == nil {
		return nil, fmt.Errorf("task definition is nil")
	}
	doc := composeDocFrom(d, false)
	out, err := yaml.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("encode compose: %w", err)
	}
	return out, nil
}

// ToSwarm renders a compose v3 stack file for `docker stack deploy`
// (Portainer swarm stacks use the same document). Bind mounts, host
// network_mode, and build-without-image are dropped or rewritten with
// notes on the definition copy used for rendering.
func ToSwarm(d *Definition) ([]byte, error) {
	if d == nil {
		return nil, fmt.Errorf("task definition is nil")
	}
	doc := composeDocFrom(d, true)
	out, err := yaml.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("encode swarm stack: %w", err)
	}
	return out, nil
}

// PortainerStack is the payload Portainer expects when creating a stack
// from a file (standalone or swarm). StackFileContent is generated
// compose; the project source is not modified.
type PortainerStack struct {
	Name             string         `json:"Name"`
	StackFileContent string         `json:"StackFileContent"`
	Env              []PortainerEnv `json:"Env,omitempty"`
	Swarm            bool           `json:"Swarm,omitempty"`
}

// PortainerEnv is one interpolation variable for a Portainer stack.
type PortainerEnv struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// ToPortainer builds a Portainer stack payload from the definition.
func ToPortainer(d *Definition, swarm bool) (*PortainerStack, error) {
	if d == nil {
		return nil, fmt.Errorf("task definition is nil")
	}
	var (
		raw []byte
		err error
	)
	if swarm {
		raw, err = ToSwarm(d)
	} else {
		raw, err = ToCompose(d)
	}
	if err != nil {
		return nil, err
	}
	name := d.Name
	if strings.TrimSpace(name) == "" {
		name = "wrk3"
	}
	return &PortainerStack{
		Name:             name,
		StackFileContent: string(raw),
		Env:              interpolationEnv(string(raw)),
		Swarm:            swarm,
	}, nil
}

var interpRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-[^}]*)?\}|\$([A-Za-z_][A-Za-z0-9_]*)`)

func interpolationEnv(content string) []PortainerEnv {
	seen := map[string]struct{}{}
	var names []string
	for _, m := range interpRe.FindAllStringSubmatch(content, -1) {
		name := m[1]
		if name == "" {
			name = m[2]
		}
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]PortainerEnv, 0, len(names))
	for _, n := range names {
		out = append(out, PortainerEnv{Name: n})
	}
	return out
}

type composeFileOut struct {
	Name     string                `yaml:"name,omitempty"`
	Version  string                `yaml:"version,omitempty"`
	Services map[string]composeSvc `yaml:"services"`
	Networks map[string]composeNet `yaml:"networks,omitempty"`
	Volumes  map[string]composeVol `yaml:"volumes,omitempty"`
}

type composeSvc struct {
	Image       string            `yaml:"image,omitempty"`
	Build       any               `yaml:"build,omitempty"`
	Command     any               `yaml:"command,omitempty"`
	Entrypoint  any               `yaml:"entrypoint,omitempty"`
	Environment map[string]string `yaml:"environment,omitempty"`
	EnvFile     []string          `yaml:"env_file,omitempty"`
	Ports       []any             `yaml:"ports,omitempty"`
	Volumes     []any             `yaml:"volumes,omitempty"`
	DependsOn   []string          `yaml:"depends_on,omitempty"`
	WorkingDir  string            `yaml:"working_dir,omitempty"`
	User        string            `yaml:"user,omitempty"`
	Restart     string            `yaml:"restart,omitempty"`
	Healthcheck *composeHealth    `yaml:"healthcheck,omitempty"`
	Networks    []string          `yaml:"networks,omitempty"`
	Privileged  bool              `yaml:"privileged,omitempty"`
	NetworkMode string            `yaml:"network_mode,omitempty"`
	Deploy      *composeDeploy    `yaml:"deploy,omitempty"`
}

type composeHealth struct {
	Test        any    `yaml:"test,omitempty"`
	Interval    string `yaml:"interval,omitempty"`
	Timeout     string `yaml:"timeout,omitempty"`
	Retries     int    `yaml:"retries,omitempty"`
	StartPeriod string `yaml:"start_period,omitempty"`
}

type composeDeploy struct {
	Replicas      int                   `yaml:"replicas,omitempty"`
	RestartPolicy *composeRestartPolicy `yaml:"restart_policy,omitempty"`
}

type composeRestartPolicy struct {
	Condition string `yaml:"condition,omitempty"`
}

type composeNet struct {
	Driver   string `yaml:"driver,omitempty"`
	External bool   `yaml:"external,omitempty"`
}

type composeVol struct {
	External bool `yaml:"external,omitempty"`
}

func composeDocFrom(d *Definition, swarm bool) composeFileOut {
	out := composeFileOut{
		Name:     d.Name,
		Services: map[string]composeSvc{},
	}
	if swarm {
		out.Version = "3.8"
	}
	for _, svc := range d.Services {
		out.Services[svc.Name] = composeSvcFrom(d, svc, swarm)
	}
	if len(d.Networks) > 0 || swarm {
		out.Networks = map[string]composeNet{}
		for _, n := range d.Networks {
			cn := composeNet{Driver: n.Driver, External: n.External}
			if swarm && !n.External && cn.Driver == "" {
				cn.Driver = "overlay"
			}
			out.Networks[n.Name] = cn
		}
		if swarm && len(out.Networks) == 0 {
			out.Networks["default"] = composeNet{Driver: "overlay"}
		}
	}
	if len(d.Volumes) > 0 {
		out.Volumes = map[string]composeVol{}
		for _, v := range d.Volumes {
			out.Volumes[v.Name] = composeVol{External: v.External}
		}
	}
	return out
}

func composeSvcFrom(d *Definition, svc Service, swarm bool) composeSvc {
	cs := composeSvc{
		Image:       svc.Image,
		Command:     cmdValue(svc.Command),
		Entrypoint:  cmdValue(svc.Entrypoint),
		Environment: copyEnv(svc.Env),
		EnvFile:     append([]string(nil), svc.EnvFiles...),
		DependsOn:   append([]string(nil), svc.DependsOn...),
		WorkingDir:  svc.WorkingDir,
		User:        svc.User,
		Restart:     svc.Restart,
		Networks:    append([]string(nil), svc.Networks...),
		Privileged:  svc.Privileged,
		NetworkMode: svc.NetworkMode,
	}
	if svc.Build != nil && !(swarm && svc.Image != "") {
		cs.Build = buildValue(svc.Build)
	}
	if swarm && svc.Image == "" && svc.Build != nil {
		cs.Image = d.Name + "_" + svc.Name + ":latest"
		cs.Build = nil
		d.addNote(fmt.Sprintf("swarm: service %q has no image; using %s (build and push it before stack deploy)", svc.Name, cs.Image))
	}
	for _, p := range svc.Ports {
		if spec := portSpec(p); spec != "" {
			cs.Ports = append(cs.Ports, spec)
		}
	}
	for _, m := range svc.Mounts {
		if swarm && m.Type == "bind" {
			d.addNote(fmt.Sprintf("swarm: service %q bind mount %s:%s must exist on every node", svc.Name, m.Source, m.Target))
		}
		if spec := mountSpec(m); spec != "" {
			cs.Volumes = append(cs.Volumes, spec)
		}
	}
	if svc.Healthcheck != nil {
		cs.Healthcheck = &composeHealth{
			Test:        cmdValue(svc.Healthcheck.Test),
			Interval:    svc.Healthcheck.Interval,
			Timeout:     svc.Healthcheck.Timeout,
			Retries:     svc.Healthcheck.Retries,
			StartPeriod: svc.Healthcheck.StartPeriod,
		}
	}
	if swarm {
		cs.NetworkMode = ""
		if svc.NetworkMode != "" {
			d.addNote(fmt.Sprintf("swarm: dropped network_mode %q on service %q (incompatible with overlay)", svc.NetworkMode, svc.Name))
		}
		cs.Restart = ""
		cs.Deploy = swarmDeploy(svc)
		if len(cs.DependsOn) > 0 {
			d.addNote("swarm: depends_on is ignored by docker stack deploy")
			cs.DependsOn = nil
		}
	} else if svc.Deploy != nil {
		cs.Deploy = swarmDeploy(svc)
	}
	return cs
}

func swarmDeploy(svc Service) *composeDeploy {
	d := &composeDeploy{}
	if svc.Deploy != nil {
		d.Replicas = svc.Deploy.Replicas
		if svc.Deploy.RestartPolicy.Condition != "" {
			d.RestartPolicy = &composeRestartPolicy{Condition: svc.Deploy.RestartPolicy.Condition}
		}
	}
	if d.RestartPolicy == nil {
		if cond := restartCondition(svc.Restart); cond != "" {
			d.RestartPolicy = &composeRestartPolicy{Condition: cond}
		}
	}
	if d.Replicas == 0 && d.RestartPolicy == nil {
		return nil
	}
	return d
}

func restartCondition(restart string) string {
	switch restart {
	case "always", "unless-stopped":
		return "any"
	case "on-failure":
		return "on-failure"
	case "no", "":
		return ""
	default:
		return ""
	}
}

func cmdValue(c Cmd) any {
	if c.Empty() {
		return nil
	}
	if c.Shell != "" {
		return c.Shell
	}
	return c.Argv
}

func buildValue(b *Build) any {
	if b == nil {
		return nil
	}
	if b.Dockerfile == "" && b.Target == "" && len(b.Args) == 0 {
		if b.Context == "" || b.Context == "." {
			return "."
		}
		return b.Context
	}
	m := map[string]any{}
	if b.Context != "" {
		m["context"] = b.Context
	}
	if b.Dockerfile != "" {
		m["dockerfile"] = b.Dockerfile
	}
	if b.Target != "" {
		m["target"] = b.Target
	}
	if len(b.Args) > 0 {
		m["args"] = b.Args
	}
	return m
}

func portSpec(p Port) string {
	if p.Target == "" && p.Published == "" {
		return ""
	}
	s := p.Target
	if p.Published != "" {
		s = p.Published + ":" + p.Target
	}
	if p.HostIP != "" {
		s = p.HostIP + ":" + s
	}
	if p.Protocol != "" && p.Protocol != "tcp" {
		s += "/" + p.Protocol
	}
	return s
}

func mountSpec(m Mount) string {
	if m.Target == "" {
		return ""
	}
	src := m.Source
	if src == "" {
		src = m.Target
	}
	s := src + ":" + m.Target
	if m.ReadOnly {
		s += ":ro"
	}
	return s
}
