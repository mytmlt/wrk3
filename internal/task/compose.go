package task

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// composeRaw mirrors the docker compose v2+ YAML structure just enough to
// extract all fields needed for the Task IR. Unknown keys are ignored so
// parsing stays forward-compatible with compose spec extensions.
type composeRaw struct {
	Version  string                   `yaml:"version"`
	Name     string                   `yaml:"name"`
	Services map[string]composeService `yaml:"services"`
	Networks map[string]composeNetwork `yaml:"networks"`
	Volumes  map[string]composeVolume  `yaml:"volumes"`
	Secrets  map[string]composeSecret  `yaml:"secrets"`
}

type composeService struct {
	Image         string            `yaml:"image"`
	Build         *composeBuild     `yaml:"build"`
	Command       any               `yaml:"command"`
	Entrypoint    any               `yaml:"entrypoint"`
	Environment   any               `yaml:"environment"`
	Ports         []string          `yaml:"ports"`
	Volumes       []string          `yaml:"volumes"`
	Networks      any               `yaml:"networks"`
	DependsOn     any               `yaml:"depends_on"`
	Restart       string            `yaml:"restart"`
	WorkingDir    string            `yaml:"working_dir"`
	User          string            `yaml:"user"`
	ContainerName string           `yaml:"container_name"`
	Labels        map[string]string `yaml:"labels"`
	ExtraHosts    []string          `yaml:"extra_hosts"`
	DNS           []string          `yaml:"dns"`
	Privileged    bool              `yaml:"privileged"`
	Deploy        *composeDeploy    `yaml:"deploy"`
	Healthcheck   *composeHealth    `yaml:"healthcheck"`
	// long syntax form fields (keyed by service name in compose v3)
	// are parsed through the raw string ports/volumes/networks for now
}

type composeBuild struct {
	Context    string            `yaml:"context"`
	Dockerfile string            `yaml:"dockerfile"`
	Args       map[string]string `yaml:"args"`
}

type composeDeploy struct {
	Resources *composeResources `yaml:"resources"`
}

type composeResources struct {
	Limits       *composeResourceDef `yaml:"limits"`
	Reservations *composeResourceDef `yaml:"reservations"`
}

type composeResourceDef struct {
	CPUs    string `yaml:"cpus"`
	Memory  string `yaml:"memory"`
	// cpu_shares is parsed as string in compose YAML but we keep it numeric
	// CPUShares string `yaml:"cpu_shares"`
}

type composeHealth struct {
	Test        any    `yaml:"test"`
	Interval    string `yaml:"interval"`
	Timeout     string `yaml:"timeout"`
	Retries     int    `yaml:"retries"`
	StartPeriod string `yaml:"start_period"`
}

type composeNetwork struct {
	Name     string            `yaml:"name"`
	Driver   string            `yaml:"driver"`
	Internal bool              `yaml:"internal"`
	Labels   map[string]string `yaml:"labels"`
}

type composeVolume struct {
	Name     string            `yaml:"name"`
	Driver   string            `yaml:"driver"`
	External bool              `yaml:"external"`
	Labels   map[string]string `yaml:"labels"`
}

type composeSecret struct {
	File  string `yaml:"file"`
	Env   string `yaml:"environment"`
	Label string `yaml:"label"`
}

// ParseCompose reads one compose YAML file from path and returns a Task.
func ParseCompose(path string) (*Task, error) {
	raw, err := composeParseBytes(path)
	if err != nil {
		return nil, err
	}
	return composeToTask(raw)
}

// ParseComposeDir reads all *.yml and *.yaml files in dir (sorted by
// name for determinism) and merges them into a single Task. Only the
// top-level compose file is required; additional overrides found in the
// dir are optional (missing dir is an error though).
func ParseComposeDir(dir string) (*Task, error) {
	entries, err := filepath.Glob(filepath.Join(dir, "*.yml"))
	if err != nil {
		return nil, fmt.Errorf("read compose dir %q: %w", dir, err)
	}
	yamlEntries, _ := filepath.Glob(filepath.Join(dir, "*.yaml"))
	entries = append(entries, yamlEntries...)

	if len(entries) == 0 {
		return nil, fmt.Errorf("no compose files found in %q", dir)
	}

	var merged *Task
	for _, path := range entries {
		t, err := ParseCompose(path)
		if err != nil {
			return nil, fmt.Errorf("parse %q: %w", path, err)
		}
		if merged == nil {
			merged = t
			continue
		}
		merged.Merge(t)
	}
	return merged, nil
}

// ParseComposeBytes parses YAML bytes directly (used by tests and
// converters).
func ParseComposeBytes(data []byte) (*Task, error) {
	raw, err := composeParseYAML(data)
	if err != nil {
		return nil, err
	}
	return composeToTask(raw)
}

func composeParseBytes(path string) (*composeRaw, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read compose file %q: %w", path, err)
	}
	return composeParseYAML(raw)
}

func composeParseYAML(data []byte) (*composeRaw, error) {
	var raw composeRaw
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse compose YAML: %w", err)
	}
	return &raw, nil
}

func composeToTask(raw *composeRaw) (*Task, error) {
	t := &Task{
		Name:    raw.Name,
		Version: raw.Version,
	}

	for name, svc := range raw.Services {
		s := buildService(name, svc)
		t.Services = append(t.Services, s)
	}

	for name, nw := range raw.Networks {
		t.Networks = append(t.Networks, Network{
			Name:     coalesce(nw.Name, name),
			Driver:   nw.Driver,
			Internal: nw.Internal,
			Labels:   nw.Labels,
		})
	}

	for name, vol := range raw.Volumes {
		t.Volumes = append(t.Volumes, VolumeDef{
			Name:     coalesce(vol.Name, name),
			Driver:   vol.Driver,
			External: vol.External,
			Labels:   vol.Labels,
		})
	}

	for name, sec := range raw.Secrets {
		t.Secrets = append(t.Secrets, Secret{
			Name:  name,
			File:  sec.File,
			Env:   sec.Env,
			Label: sec.Label,
		})
	}

	return t, nil
}

func buildService(name string, raw composeService) Service {
	s := Service{
		Name:          name,
		Image:         raw.Image,
		Restart:       raw.Restart,
		WorkingDir:    raw.WorkingDir,
		User:          raw.User,
		ContainerName: raw.ContainerName,
		Labels:        raw.Labels,
		ExtraHosts:    raw.ExtraHosts,
		DNS:           raw.DNS,
		Privileged:    raw.Privileged,
	}

	if raw.Build != nil {
		ctx := raw.Build.Context
		if ctx != "" {
			s.Build = ctx
		} else {
			s.Build = "."
		}
	}

	s.Command = toStringSlice(raw.Command)
	s.Entrypoint = toStringSlice(raw.Entrypoint)

	s.Environment = parseEnv(raw.Environment)
	s.Ports = parsePorts(raw.Ports)
	s.Volumes = parseVolumes(raw.Volumes)
	s.Networks = parseNetworks(raw.Networks)
	s.DependsOn = parseDependsOn(raw.DependsOn)

	if raw.Deploy != nil && raw.Deploy.Resources != nil && raw.Deploy.Resources.Limits != nil {
		s.ResourceLimit = &ResourceLimit{
			CPUs:   raw.Deploy.Resources.Limits.CPUs,
			Memory: raw.Deploy.Resources.Limits.Memory,
		}
	}

	s.Health = parseHealth(raw.Healthcheck)

	return s
}

func toStringSlice(v any) []string {
	switch val := v.(type) {
	case nil:
		return nil
	case string:
		return []string{val}
	case []any:
		out := make([]string, 0, len(val))
		for _, e := range val {
			out = append(out, fmt.Sprint(e))
		}
		return out
	case []string:
		return val
	default:
		return []string{fmt.Sprint(v)}
	}
}

func parseEnv(v any) []EnvVar {
	switch val := v.(type) {
	case nil:
		return nil
	case []any:
		var out []EnvVar
		for _, e := range val {
			switch entry := e.(type) {
			case string:
				if k, v, ok := strings.Cut(entry, "="); ok {
					out = append(out, EnvVar{Key: k, Value: v})
				} else {
					out = append(out, EnvVar{Key: entry})
				}
			case map[string]any:
				for k, kv := range entry {
					out = append(out, EnvVar{Key: k, Value: fmt.Sprint(kv)})
				}
			}
		}
		return out
	case map[string]any:
		var out []EnvVar
		for k, kv := range val {
			out = append(out, EnvVar{Key: k, Value: fmt.Sprint(kv)})
		}
		return out
	default:
		return nil
	}
}

func parsePorts(raw []string) []PortMapping {
	var out []PortMapping
	for _, p := range raw {
		if strings.TrimSpace(p) == "" {
			continue
		}
		pm := parsePort(p)
		out = append(out, pm)
	}
	return out
}

func parsePort(spec string) PortMapping {
	pm := PortMapping{Protocol: "tcp"}
	// Format: [host:]container[/protocol]
	parts := strings.Split(spec, ":")
	if len(parts) == 2 {
		pm.HostPort, _ = strconv.Atoi(parts[0])
		containerPart := parts[1]
		container, proto := splitProtocol(containerPart)
		pm.Container = safeAtoi(container)
		if proto != "" {
			pm.Protocol = proto
		}
	} else if len(parts) == 3 {
		pm.Host = parts[0]
		pm.HostPort, _ = strconv.Atoi(parts[1])
		container, proto := splitProtocol(parts[2])
		pm.Container = safeAtoi(container)
		if proto != "" {
			pm.Protocol = proto
		}
	} else {
		// bare port: "8080" or "8080/tcp"
		container, proto := splitProtocol(parts[0])
		pm.Container = safeAtoi(container)
		if proto != "" {
			pm.Protocol = proto
		}
	}
	return pm
}

func splitProtocol(s string) (string, string) {
	if idx := strings.IndexByte(s, '/'); idx >= 0 {
		return s[:idx], s[idx+1:]
	}
	return s, ""
}

func safeAtoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

func parseVolumes(raw []string) []VolumeMapping {
	var out []VolumeMapping
	for _, v := range raw {
		if strings.TrimSpace(v) == "" {
			continue
		}
		vm := parseVolume(v)
		out = append(out, vm)
	}
	return out
}

func parseVolume(spec string) VolumeMapping {
	vm := VolumeMapping{}
	// support both short-syntax "src:dst[:ro]" and basic long-syntax prefix
	// long syntax would need YAML unrolling but compose v2+ uses the same
	// string for common cases.
	parts := strings.Split(spec, ":")
	switch len(parts) {
	case 1:
		vm.Target = parts[0]
	case 2:
		vm.Source = parts[0]
		vm.Target = parts[1]
	case 3:
		vm.Source = parts[0]
		vm.Target = parts[1]
		vm.ReadOnly = strings.EqualFold(parts[2], "ro")
	}
	return vm
}

func parseNetworks(v any) []NetworkRef {
	switch val := v.(type) {
	case nil:
		return nil
	case []any:
		var out []NetworkRef
		for _, e := range val {
			switch entry := e.(type) {
			case string:
				out = append(out, NetworkRef{Name: entry})
			case map[string]any:
				for name, cfg := range entry {
					nr := NetworkRef{Name: name}
					if m, ok := cfg.(map[string]any); ok {
						if aliases, ok := m["aliases"]; ok {
							nr.Aliases = toStringSlice(aliases)
						}
					}
					out = append(out, nr)
				}
			}
		}
		return out
	case map[string]any:
		var out []NetworkRef
		for name := range val {
			out = append(out, NetworkRef{Name: name})
		}
		return out
	default:
		return nil
	}
}

func parseDependsOn(v any) []string {
	switch val := v.(type) {
	case nil:
		return nil
	case []any:
		var out []string
		for _, e := range val {
			out = append(out, fmt.Sprint(e))
		}
		return out
	case map[string]any:
		var out []string
		for k := range val {
			out = append(out, k)
		}
		return out
	case []string:
		return val
	default:
		return []string{fmt.Sprint(v)}
	}
}

func parseHealth(raw *composeHealth) *HealthCheck {
	if raw == nil {
		return nil
	}
	return &HealthCheck{
		Test:        parseHealthTest(raw.Test),
		Interval:    parseDuration(raw.Interval),
		Timeout:     parseDuration(raw.Timeout),
		Retries:     raw.Retries,
		StartPeriod: parseDuration(raw.StartPeriod),
	}
}

func parseHealthTest(v any) []string {
	switch val := v.(type) {
	case nil:
		return nil
	case string:
		return []string{val}
	case []any:
		out := make([]string, 0, len(val))
		for _, e := range val {
			out = append(out, fmt.Sprint(e))
		}
		return out
	default:
		return []string{fmt.Sprint(v)}
	}
}

func parseDuration(s string) time.Duration {
	if s == "" {
		return 0
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0
	}
	return d
}

func coalesce(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return strings.TrimSpace(a)
	}
	return strings.TrimSpace(b)
}
