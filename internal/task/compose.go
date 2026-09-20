package task

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// composeDoc is the raw shape of a compose document. Service, volume and
// network bodies stay as generic maps because compose accepts several
// syntaxes per field (string or list or mapping).
type composeDoc struct {
	Name     string                    `yaml:"name"`
	Services map[string]map[string]any `yaml:"services"`
	Volumes  map[string]map[string]any `yaml:"volumes"`
	Networks map[string]map[string]any `yaml:"networks"`
}

// LoadCompose reads and parses a docker compose file into a Definition.
func LoadCompose(path string) (*Definition, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read compose file %q: %w", path, err)
	}
	def, err := ParseCompose(raw)
	if err != nil {
		return nil, fmt.Errorf("parse compose file %q: %w", path, err)
	}
	return def, nil
}

// ParseCompose converts a docker compose document into a Definition.
// Unknown fields are ignored so a superset compose file still analyzes.
func ParseCompose(raw []byte) (*Definition, error) {
	var doc composeDoc
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse compose yaml: %w", err)
	}
	if len(doc.Services) == 0 {
		return nil, fmt.Errorf("compose file defines no services")
	}
	def := &Definition{
		Name:     strings.TrimSpace(doc.Name),
		Services: make(map[string]*Service, len(doc.Services)),
		Volumes:  map[string]*Volume{},
		Networks: map[string]*Network{},
	}
	for name, body := range doc.Volumes {
		driver, external := parseTopResource(body)
		def.Volumes[name] = &Volume{Driver: driver, External: external}
	}
	for name, body := range doc.Networks {
		driver, external := parseTopResource(body)
		def.Networks[name] = &Network{Driver: driver, External: external}
	}
	for name, body := range doc.Services {
		svc, err := parseComposeService(name, body)
		if err != nil {
			return nil, err
		}
		def.Services[name] = svc
	}
	// Compose requires a top-level entry for every named volume/network a
	// service references; be lenient and synthesize missing ones so an
	// analysis never fails on implicit resources.
	synthesizeResources(def)
	def.Normalize()
	return def, nil
}

func parseComposeService(name string, body map[string]any) (*Service, error) {
	svc := &Service{
		Image:       asString(body["image"]),
		Command:     asStringList(body["command"]),
		Entrypoint:  asStringList(body["entrypoint"]),
		Environment: asKeyValues(body["environment"]),
		Labels:      asKeyValues(body["labels"]),
		Restart:     asString(body["restart"]),
	}
	if b, ok := body["build"]; ok {
		svc.Build = parseBuild(b)
	}
	ports, err := parsePorts(body["ports"])
	if err != nil {
		return nil, fmt.Errorf("service %q: %w", name, err)
	}
	svc.Ports = ports
	vols, err := parseMounts(body["volumes"])
	if err != nil {
		return nil, fmt.Errorf("service %q: %w", name, err)
	}
	svc.Volumes = vols
	svc.DependsOn = parseDependsOn(body["depends_on"])
	svc.Networks = parseNameOrMap(body["networks"])
	if hc, ok := body["healthcheck"]; ok {
		svc.Healthcheck = parseHealthcheck(hc)
	}
	if dep, ok := body["deploy"]; ok {
		svc.Deploy = parseDeploy(dep)
	}
	return svc, nil
}

func parseBuild(v any) *Build {
	switch t := v.(type) {
	case string:
		return &Build{Context: strings.TrimSpace(t)}
	case map[string]any:
		b := &Build{
			Context:    asString(t["context"]),
			Dockerfile: asString(t["dockerfile"]),
			Target:     asString(t["target"]),
			Args:       asKeyValues(t["args"]),
		}
		if b.Context == "" {
			b.Context = "."
		}
		return b
	default:
		return nil
	}
}

// parsePorts accepts the compose short string form ("8080:80/udp",
// "127.0.0.1:8080:80") and the long mapping form.
func parsePorts(v any) ([]Port, error) {
	items, ok := v.([]any)
	if !ok {
		return nil, nil
	}
	var out []Port
	for i, item := range items {
		switch t := item.(type) {
		case string:
			p, err := parsePortString(t)
			if err != nil {
				return nil, fmt.Errorf("ports[%d]: %w", i, err)
			}
			out = append(out, p)
		case int:
			out = append(out, Port{Published: t, Target: t, Protocol: ProtocolTCP})
		case map[string]any:
			p := Port{
				HostIP:    asString(t["host_ip"]),
				Published: asInt(t["published"]),
				Target:    asInt(t["target"]),
				Protocol:  asString(t["protocol"]),
				Mode:      asString(t["mode"]),
			}
			if p.Protocol == "" {
				p.Protocol = ProtocolTCP
			}
			if p.Target == 0 {
				p.Target = p.Published
			}
			out = append(out, p)
		default:
			return nil, fmt.Errorf("ports[%d]: unsupported value %T", i, item)
		}
	}
	return out, nil
}

// parsePortString parses the compose short port syntax, tolerating an
// optional host IP (including bracketed IPv6) and protocol suffix.
func parsePortString(s string) (Port, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Port{}, fmt.Errorf("empty port")
	}
	p := Port{Protocol: ProtocolTCP}
	if i := strings.LastIndex(s, "/"); i >= 0 {
		p.Protocol = strings.ToLower(strings.TrimSpace(s[i+1:]))
		s = s[:i]
		if p.Protocol == "" {
			p.Protocol = ProtocolTCP
		}
	}
	if strings.HasPrefix(s, "[") {
		end := strings.Index(s, "]")
		if end < 0 {
			return Port{}, fmt.Errorf("unterminated IPv6 host in %q", s)
		}
		p.HostIP = s[1:end]
		s = strings.TrimPrefix(s[end+1:], ":")
	}
	parts := strings.Split(s, ":")
	switch len(parts) {
	case 1:
		n, err := parsePortInt(parts[0])
		if err != nil {
			return Port{}, err
		}
		p.Published, p.Target = n, n
	case 2:
		pub, err := parsePortInt(parts[0])
		if err != nil {
			return Port{}, err
		}
		tgt, err := parsePortInt(parts[1])
		if err != nil {
			return Port{}, err
		}
		p.Published, p.Target = pub, tgt
	case 3:
		p.HostIP = parts[0]
		pub, err := parsePortInt(parts[1])
		if err != nil {
			return Port{}, err
		}
		tgt, err := parsePortInt(parts[2])
		if err != nil {
			return Port{}, err
		}
		p.Published, p.Target = pub, tgt
	default:
		return Port{}, fmt.Errorf("invalid port %q", s)
	}
	return p, nil
}

func parsePortInt(s string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, fmt.Errorf("invalid port %q", s)
	}
	if n < 0 || n > 65535 {
		return 0, fmt.Errorf("port %d out of range 0-65535", n)
	}
	return n, nil
}

// parseMounts accepts the short "src:dst[:opts]" form and the long
// mapping form. Ambiguous host paths (e.g. Windows drive letters) should
// use the long form, as compose itself recommends.
func parseMounts(v any) ([]Mount, error) {
	items, ok := v.([]any)
	if !ok {
		return nil, nil
	}
	var out []Mount
	for i, item := range items {
		switch t := item.(type) {
		case string:
			m, err := parseMountString(t)
			if err != nil {
				return nil, fmt.Errorf("volumes[%d]: %w", i, err)
			}
			out = append(out, m)
		case map[string]any:
			m := Mount{
				Type:     asString(t["type"]),
				Source:   asString(t["source"]),
				Target:   asString(t["target"]),
				ReadOnly: asBool(t["read_only"]),
			}
			if m.Type == "" {
				m.Type = inferMountType(m.Source)
			}
			out = append(out, m)
		default:
			return nil, fmt.Errorf("volumes[%d]: unsupported value %T", i, item)
		}
	}
	return out, nil
}

func parseMountString(s string) (Mount, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Mount{}, fmt.Errorf("empty volume")
	}
	// Preserve IPv6-less, drive-less common cases: split on ":" but only
	// from the right for the read-only suffix.
	opts := ""
	body := s
	if strings.Count(s, ":") >= 2 {
		idx := strings.LastIndex(s, ":")
		maybe := s[idx+1:]
		if isMountOpts(maybe) {
			opts = maybe
			body = s[:idx]
		}
	}
	parts := strings.SplitN(body, ":", 2)
	m := Mount{Target: parts[0]}
	if len(parts) == 2 {
		m.Source = parts[0]
		m.Target = parts[1]
	}
	m.Type = inferMountType(m.Source)
	m.ReadOnly = strings.Contains(opts, "ro")
	return m, nil
}

func isMountOpts(s string) bool {
	if s == "" {
		return false
	}
	for _, opt := range strings.Split(s, ",") {
		switch strings.TrimSpace(opt) {
		case "ro", "rw", "z", "Z", "cached", "delegated", "consistent", "nocopy", "private", "rprivate", "shared", "rshared", "slave", "rslave":
		default:
			return false
		}
	}
	return true
}

// parseDependsOn accepts the list form and the mapping form
// (`depends_on: {db: {condition: service_healthy}}`).
func parseDependsOn(v any) []string {
	switch t := v.(type) {
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s := asString(item); s != "" {
				out = append(out, s)
			}
		}
		return out
	case map[string]any:
		out := make([]string, 0, len(t))
		for k := range t {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	default:
		return nil
	}
}

// parseNameOrMap accepts a list of names or a mapping keyed by name.
func parseNameOrMap(v any) []string {
	switch t := v.(type) {
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s := asString(item); s != "" {
				out = append(out, s)
			}
		}
		return out
	case map[string]any:
		out := make([]string, 0, len(t))
		for k := range t {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	default:
		return nil
	}
}

func parseHealthcheck(v any) *Healthcheck {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	hc := &Healthcheck{
		Test:        asStringList(m["test"]),
		Interval:    asString(m["interval"]),
		Timeout:     asString(m["timeout"]),
		Retries:     asInt(m["retries"]),
		StartPeriod: asString(m["start_period"]),
	}
	if hc.Test == nil && hc.Interval == "" && hc.Timeout == "" && hc.Retries == 0 && hc.StartPeriod == "" {
		return nil
	}
	return hc
}

func parseDeploy(v any) *Deploy {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	d := &Deploy{Replicas: asInt(m["replicas"]), Mode: asString(m["mode"])}
	if d.Replicas == 0 && d.Mode == "" {
		return nil
	}
	return d
}

// parseTopResource reads driver/external from a top-level volume or
// network body. `external` may be a bool or a mapping (`{name: x}`).
func parseTopResource(body map[string]any) (driver string, external bool) {
	if body == nil {
		return "", false
	}
	driver = asString(body["driver"])
	switch t := body["external"].(type) {
	case bool:
		external = t
	case map[string]any:
		external = true
	case string:
		external = strings.EqualFold(strings.TrimSpace(t), "true")
	}
	return driver, external
}

// synthesizeResources creates placeholder top-level entries for named
// volumes/networks referenced only by services.
func synthesizeResources(def *Definition) {
	for _, svc := range def.Services {
		for _, m := range svc.Volumes {
			if m.Type != MountVolume || m.Source == "" {
				continue
			}
			if _, ok := def.Volumes[m.Source]; !ok {
				def.Volumes[m.Source] = &Volume{}
			}
		}
		for _, net := range svc.Networks {
			if _, ok := def.Networks[net]; !ok {
				def.Networks[net] = &Network{}
			}
		}
	}
}

// asString renders a YAML scalar as a string. Non-scalars yield "".
func asString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(t)
	case bool:
		return strconv.FormatBool(t)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		return ""
	}
}

func asInt(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(t))
		if err != nil {
			return 0
		}
		return n
	default:
		return 0
	}
}

func asBool(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		b, err := strconv.ParseBool(strings.TrimSpace(t))
		if err != nil {
			return false
		}
		return b
	default:
		return false
	}
}

// asStringList converts a scalar to a single-element list and a YAML
// sequence to a string list. This preserves the compose "shell form"
// (string) as one element.
func asStringList(v any) []string {
	switch t := v.(type) {
	case nil:
		return nil
	case string:
		if strings.TrimSpace(t) == "" {
			return nil
		}
		return []string{t}
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok {
				out = append(out, s)
			} else if s := asString(item); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// asKeyValues converts the compose mapping form (KEY: value) and list
// form (KEY=value, or bare KEY meaning "inherit from the host") into a
// map. Bare keys inherit via ${KEY} so intent survives a round trip.
func asKeyValues(v any) map[string]string {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]string, len(t))
		for k, val := range t {
			out[k] = asString(val)
		}
		return out
	case []any:
		out := make(map[string]string, len(t))
		for _, item := range t {
			s, ok := item.(string)
			if !ok {
				continue
			}
			if k, val, found := strings.Cut(s, "="); found {
				out[strings.TrimSpace(k)] = val
			} else if k = strings.TrimSpace(s); k != "" {
				out[k] = "${" + k + "}"
			}
		}
		return out
	default:
		return nil
	}
}
