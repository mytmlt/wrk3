package task

import (
	"fmt"
	"strconv"
	"strings"
)

func asString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case uint64:
		return strconv.FormatUint(t, 10)
	case float64:
		if t == float64(int(t)) {
			return strconv.Itoa(int(t))
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		if t {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprint(t)
	}
}

func asInt(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case uint64:
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
		s := strings.ToLower(strings.TrimSpace(t))
		return s == "true" || s == "yes" || s == "1"
	default:
		return false
	}
}

func asMap(v any) map[string]any {
	switch t := v.(type) {
	case map[string]any:
		return t
	case map[any]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[asString(k)] = val
		}
		return out
	default:
		return nil
	}
}

func asList(v any) []any {
	switch t := v.(type) {
	case []any:
		return t
	case []string:
		out := make([]any, len(t))
		for i, s := range t {
			out[i] = s
		}
		return out
	default:
		return nil
	}
}

func asCmd(v any) Cmd {
	if v == nil {
		return Cmd{}
	}
	if s, ok := v.(string); ok {
		return Cmd{Shell: s}
	}
	if list := asList(v); list != nil {
		argv := make([]string, 0, len(list))
		for _, item := range list {
			argv = append(argv, asString(item))
		}
		return Cmd{Argv: argv}
	}
	return Cmd{Shell: asString(v)}
}

func asStringSlice(v any) []string {
	if v == nil {
		return nil
	}
	if s, ok := v.(string); ok {
		s = strings.TrimSpace(s)
		if s == "" {
			return nil
		}
		return []string{s}
	}
	list := asList(v)
	if list == nil {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		s := strings.TrimSpace(asString(item))
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func asEnvMap(v any) map[string]string {
	if v == nil {
		return nil
	}
	if m := asMap(v); m != nil {
		out := make(map[string]string, len(m))
		for k, val := range m {
			if strings.TrimSpace(k) == "" {
				continue
			}
			out[k] = asString(val)
		}
		return out
	}
	list := asList(v)
	if list == nil {
		return nil
	}
	out := map[string]string{}
	for _, item := range list {
		s := asString(item)
		if s == "" {
			continue
		}
		k, val, ok := strings.Cut(s, "=")
		if !ok {
			out[s] = ""
			continue
		}
		out[k] = val
	}
	return out
}

func asBuild(v any) *Build {
	if v == nil {
		return nil
	}
	if s, ok := v.(string); ok {
		s = strings.TrimSpace(s)
		if s == "" {
			return nil
		}
		return &Build{Context: s}
	}
	m := asMap(v)
	if m == nil {
		return nil
	}
	b := &Build{
		Context:    strings.TrimSpace(asString(m["context"])),
		Dockerfile: strings.TrimSpace(asString(m["dockerfile"])),
		Target:     strings.TrimSpace(asString(m["target"])),
		Args:       asEnvMap(m["args"]),
	}
	if b.Context == "" && b.Dockerfile == "" && b.Target == "" && len(b.Args) == 0 {
		return nil
	}
	if b.Context == "" {
		b.Context = "."
	}
	return b
}

func asDependsOn(v any) []string {
	if v == nil {
		return nil
	}
	if list := asList(v); list != nil {
		return asStringSlice(list)
	}
	m := asMap(v)
	if m == nil {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		if strings.TrimSpace(k) != "" {
			out = append(out, k)
		}
	}
	return out
}

func asPorts(v any) []Port {
	if v == nil {
		return nil
	}
	if m := asMap(v); m != nil {
		p := portFromMap(m)
		if p.Target == "" && p.Published == "" {
			return nil
		}
		return []Port{p}
	}
	list := asList(v)
	if list == nil {
		s := strings.TrimSpace(asString(v))
		if s == "" {
			return nil
		}
		return []Port{parsePortString(s)}
	}
	var out []Port
	for _, item := range list {
		if m := asMap(item); m != nil {
			p := portFromMap(m)
			if p.Target == "" && p.Published == "" {
				continue
			}
			out = append(out, p)
			continue
		}
		s := strings.TrimSpace(asString(item))
		if s == "" {
			continue
		}
		out = append(out, parsePortString(s))
	}
	return out
}

func portFromMap(m map[string]any) Port {
	proto := strings.TrimSpace(asString(m["protocol"]))
	return Port{
		HostIP:    strings.TrimSpace(asString(m["host_ip"])),
		Published: strings.TrimSpace(asString(m["published"])),
		Target:    strings.TrimSpace(asString(m["target"])),
		Protocol:  proto,
	}
}

func parsePortString(s string) Port {
	s = strings.TrimSpace(s)
	proto := ""
	if i := strings.LastIndex(s, "/"); i >= 0 {
		tail := s[i+1:]
		if tail == "tcp" || tail == "udp" || tail == "sctp" {
			proto = tail
			s = s[:i]
		}
	}
	parts := strings.Split(s, ":")
	p := Port{Protocol: proto}
	switch len(parts) {
	case 1:
		p.Target = parts[0]
	case 2:
		p.Published, p.Target = parts[0], parts[1]
	default:
		p.HostIP = strings.Join(parts[:len(parts)-2], ":")
		p.Published = parts[len(parts)-2]
		p.Target = parts[len(parts)-1]
	}
	return p
}

func asMounts(v any) []Mount {
	if v == nil {
		return nil
	}
	if m := asMap(v); m != nil {
		mount := mountFromMap(m)
		if mount.Target == "" && mount.Source == "" {
			return nil
		}
		return []Mount{mount}
	}
	list := asList(v)
	if list == nil {
		s := strings.TrimSpace(asString(v))
		if s == "" {
			return nil
		}
		return []Mount{parseMountString(s)}
	}
	var out []Mount
	for _, item := range list {
		if m := asMap(item); m != nil {
			mount := mountFromMap(m)
			if mount.Target == "" && mount.Source == "" {
				continue
			}
			out = append(out, mount)
			continue
		}
		s := strings.TrimSpace(asString(item))
		if s == "" {
			continue
		}
		out = append(out, parseMountString(s))
	}
	return out
}

func mountFromMap(m map[string]any) Mount {
	typ := strings.TrimSpace(asString(m["type"]))
	src := strings.TrimSpace(asString(m["source"]))
	if src == "" {
		src = strings.TrimSpace(asString(m["src"]))
	}
	target := strings.TrimSpace(asString(m["target"]))
	if target == "" {
		target = strings.TrimSpace(asString(m["destination"]))
	}
	if typ == "" {
		typ = inferMountType(src)
	}
	return Mount{
		Type:     typ,
		Source:   src,
		Target:   target,
		ReadOnly: asBool(m["read_only"]) || asBool(m["readOnly"]),
	}
}

func parseMountString(s string) Mount {
	parts := strings.Split(s, ":")
	m := Mount{}
	switch {
	case len(parts) == 1:
		m.Source = parts[0]
		m.Target = parts[0]
	case len(parts) >= 2:
		m.Source = parts[0]
		m.Target = parts[1]
		if len(parts) >= 3 && hasRO(parts[2]) {
			m.ReadOnly = true
		}
	}
	m.Type = inferMountType(m.Source)
	return m
}

func hasRO(opts string) bool {
	for _, o := range strings.Split(opts, ",") {
		if strings.TrimSpace(o) == "ro" {
			return true
		}
	}
	return false
}

func inferMountType(source string) string {
	if source == "" {
		return "volume"
	}
	if source == "." || strings.HasPrefix(source, ".") || strings.HasPrefix(source, "/") || strings.Contains(source, "/") {
		return "bind"
	}
	return "volume"
}

func asHealthcheck(v any) *Healthcheck {
	m := asMap(v)
	if m == nil {
		return nil
	}
	h := &Healthcheck{
		Test:        asCmd(m["test"]),
		Interval:    strings.TrimSpace(asString(m["interval"])),
		Timeout:     strings.TrimSpace(asString(m["timeout"])),
		Retries:     asInt(m["retries"]),
		StartPeriod: strings.TrimSpace(asString(m["start_period"])),
	}
	if h.Test.Empty() && h.Interval == "" && h.Timeout == "" && h.Retries == 0 && h.StartPeriod == "" {
		return nil
	}
	return h
}

func asDeploy(v any) *Deploy {
	m := asMap(v)
	if m == nil {
		return nil
	}
	d := &Deploy{Replicas: asInt(m["replicas"])}
	if rp := asMap(m["restart_policy"]); rp != nil {
		d.RestartPolicy.Condition = strings.TrimSpace(asString(rp["condition"]))
	}
	if d.Replicas == 0 && d.RestartPolicy.Condition == "" {
		return nil
	}
	return d
}

func asServiceNetworks(v any) []string {
	if v == nil {
		return nil
	}
	if list := asList(v); list != nil {
		return asStringSlice(list)
	}
	m := asMap(v)
	if m == nil {
		s := strings.TrimSpace(asString(v))
		if s == "" {
			return nil
		}
		return []string{s}
	}
	out := make([]string, 0, len(m))
	for k := range m {
		if strings.TrimSpace(k) != "" {
			out = append(out, k)
		}
	}
	return out
}
