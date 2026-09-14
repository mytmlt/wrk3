package task

import (
	"fmt"
	"strings"
)

func parseMounts(v any) ([]Mount, error) {
	if v == nil {
		return nil, nil
	}
	items, ok := v.([]any)
	if !ok {
		items = []any{v}
	}
	var out []Mount
	for i, item := range items {
		m, err := parseMount(item)
		if err != nil {
			return nil, fmt.Errorf("volumes[%d]: %w", i, err)
		}
		if m != nil {
			out = append(out, *m)
		}
	}
	return out, nil
}

func parseMount(v any) (*Mount, error) {
	if m, ok := asMap(v); ok {
		typ := strings.ToLower(strings.TrimSpace(asString(m["type"])))
		src := asString(m["source"])
		if src == "" {
			src = asString(m["src"])
		}
		target := asString(m["target"])
		if target == "" {
			target = asString(m["destination"])
		}
		if target == "" {
			return nil, fmt.Errorf("missing target")
		}
		if typ == "" {
			typ = mountType(src)
		}
		return &Mount{
			Type:     typ,
			Source:   src,
			Target:   target,
			ReadOnly: asBool(m["read_only"]) || asBool(m["readOnly"]),
		}, nil
	}
	s := strings.TrimSpace(asString(v))
	if s == "" {
		return nil, nil
	}
	return parseShortMount(s)
}

func parseShortMount(s string) (*Mount, error) {
	ro := false
	parts := strings.Split(s, ":")
	if len(parts) >= 3 {
		mode := strings.ToLower(parts[len(parts)-1])
		if mode == "ro" || mode == "rw" {
			ro = mode == "ro"
			parts = parts[:len(parts)-1]
		}
	}
	switch len(parts) {
	case 1:
		return &Mount{Type: "volume", Source: parts[0], Target: parts[0]}, nil
	case 2:
		src, target := parts[0], parts[1]
		return &Mount{Type: mountType(src), Source: src, Target: target, ReadOnly: ro}, nil
	default:
		// Windows-style or extra colons: join source back except last.
		target := parts[len(parts)-1]
		src := strings.Join(parts[:len(parts)-1], ":")
		return &Mount{Type: mountType(src), Source: src, Target: target, ReadOnly: ro}, nil
	}
}

func mountType(src string) string {
	if src == "" {
		return "volume"
	}
	if src == "." || src == ".." || strings.HasPrefix(src, "./") || strings.HasPrefix(src, "../") || strings.HasPrefix(src, "/") || strings.Contains(src, `\`) {
		return "bind"
	}
	if strings.Contains(src, "/") {
		return "bind"
	}
	return "volume"
}

func formatMount(m Mount) string {
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

func namedVolumeSources(svcs []Service) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, s := range svcs {
		for _, m := range s.Mounts {
			if m.Type != "volume" {
				continue
			}
			if m.Source == "" {
				continue
			}
			if _, ok := seen[m.Source]; ok {
				continue
			}
			seen[m.Source] = struct{}{}
			out = append(out, m.Source)
		}
	}
	return out
}
