package task

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var interpRE = regexp.MustCompile(`^\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-([^}]*))?\}$`)

func parsePorts(v any) ([]Port, []Note, error) {
	if v == nil {
		return nil, nil, nil
	}
	items, ok := v.([]any)
	if !ok {
		items = []any{v}
	}
	var out []Port
	var notes []Note
	for i, item := range items {
		p, note, err := parsePort(item)
		if err != nil {
			return nil, nil, fmt.Errorf("ports[%d]: %w", i, err)
		}
		if p == nil {
			if note != nil {
				notes = append(notes, *note)
			}
			continue
		}
		out = append(out, *p)
		if note != nil {
			notes = append(notes, *note)
		}
	}
	return out, notes, nil
}

func parsePort(v any) (*Port, *Note, error) {
	if m, ok := asMap(v); ok {
		return parseLongPort(m)
	}
	if n, ok := asInt(v); ok {
		if n <= 0 {
			return nil, nil, fmt.Errorf("invalid port %d", n)
		}
		return &Port{Container: n, Host: n, Protocol: "tcp"}, &Note{
			Level:   levelInfo,
			Code:    codeHardcodedPort,
			Message: fmt.Sprintf("published port %d is hardcoded", n),
		}, nil
	}
	s := strings.TrimSpace(asString(v))
	if s == "" {
		return nil, nil, nil
	}
	return parseShortPort(s)
}

func parseLongPort(m map[string]any) (*Port, *Note, error) {
	p := Port{Protocol: "tcp"}
	if proto := strings.TrimSpace(asString(m["protocol"])); proto != "" {
		p.Protocol = strings.ToLower(proto)
	}
	if ip := strings.TrimSpace(asString(m["host_ip"])); ip != "" {
		p.HostIP = ip
	} else if ip := strings.TrimSpace(asString(m["hostIP"])); ip != "" {
		p.HostIP = ip
	}
	p.Name = strings.TrimSpace(asString(m["name"]))
	p.HostVar = strings.TrimSpace(asString(m["hostVar"]))
	if t, ok := asInt(m["target"]); ok {
		p.Container = t
	} else if t, ok := asInt(m["container"]); ok {
		p.Container = t
	}
	if h, ok := asInt(m["host"]); ok {
		p.Host = h
	}
	if pub, ok := m["published"]; ok && pub != nil {
		host, hostVar, def, err := parsePublished(asString(pub))
		if err != nil {
			return nil, nil, err
		}
		if hostVar != "" {
			p.HostVar = hostVar
			p.Host = def
			if p.Host == 0 {
				p.Host = p.Container
			}
		} else if p.Host == 0 {
			p.Host = host
		}
	}
	if p.Container <= 0 && p.Host > 0 {
		p.Container = p.Host
	}
	if p.Container <= 0 {
		return nil, nil, fmt.Errorf("missing target port")
	}
	var note *Note
	if p.Host > 0 && p.HostVar == "" {
		note = &Note{
			Level:   levelInfo,
			Code:    codeHardcodedPort,
			Message: fmt.Sprintf("published port %d is hardcoded", p.Host),
		}
	}
	return &p, note, nil
}

func parseShortPort(s string) (*Port, *Note, error) {
	proto := "tcp"
	if i := strings.LastIndex(s, "/"); i >= 0 {
		proto = strings.ToLower(strings.TrimSpace(s[i+1:]))
		s = s[:i]
	}
	if strings.Contains(s, "-") && !strings.Contains(s, "${") {
		return nil, &Note{
			Level:   levelWarn,
			Code:    codePortRange,
			Message: fmt.Sprintf("skipping port range %q", s),
		}, nil
	}
	parts := splitPortParts(s)
	p := Port{Protocol: proto}
	var note *Note
	switch len(parts) {
	case 1:
		n, hostVar, def, err := parsePublished(parts[0])
		if err != nil {
			return nil, nil, err
		}
		if hostVar != "" {
			p.HostVar = hostVar
			p.Host = def
			p.Container = def
		} else {
			p.Container = n
			p.Host = n
			note = hardcodedNote(n)
		}
	case 2:
		host, hostVar, def, err := parsePublished(parts[0])
		if err != nil {
			return nil, nil, err
		}
		c, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil || c <= 0 {
			return nil, nil, fmt.Errorf("invalid container port %q", parts[1])
		}
		p.Container = c
		if hostVar != "" {
			p.HostVar = hostVar
			p.Host = def
			if p.Host == 0 {
				p.Host = c
			}
		} else {
			p.Host = host
			note = hardcodedNote(host)
		}
	case 3:
		p.HostIP = parts[0]
		host, hostVar, def, err := parsePublished(parts[1])
		if err != nil {
			return nil, nil, err
		}
		c, err := strconv.Atoi(strings.TrimSpace(parts[2]))
		if err != nil || c <= 0 {
			return nil, nil, fmt.Errorf("invalid container port %q", parts[2])
		}
		p.Container = c
		if hostVar != "" {
			p.HostVar = hostVar
			p.Host = def
			if p.Host == 0 {
				p.Host = c
			}
		} else {
			p.Host = host
			note = hardcodedNote(host)
		}
	default:
		return nil, nil, fmt.Errorf("invalid port %q", s)
	}
	if p.Container <= 0 {
		if p.Host > 0 {
			p.Container = p.Host
		} else {
			return nil, nil, fmt.Errorf("invalid port %q", s)
		}
	}
	return &p, note, nil
}

func splitPortParts(s string) []string {
	var parts []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			if depth > 0 {
				depth--
			}
		case ':':
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, s[start:])
	return parts
}

func hardcodedNote(n int) *Note {
	if n <= 0 {
		return nil
	}
	return &Note{
		Level:   levelInfo,
		Code:    codeHardcodedPort,
		Message: fmt.Sprintf("published port %d is hardcoded", n),
	}
}

func parsePublished(s string) (host int, hostVar string, def int, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, "", 0, nil
	}
	if m := interpRE.FindStringSubmatch(s); m != nil {
		hostVar = m[1]
		if m[2] != "" {
			def, err = strconv.Atoi(strings.TrimSpace(m[2]))
			if err != nil {
				return 0, "", 0, fmt.Errorf("invalid port default %q", m[2])
			}
		}
		return 0, hostVar, def, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, "", 0, fmt.Errorf("invalid published port %q", s)
	}
	return n, "", 0, nil
}

func inferPortNames(svcs []Service) {
	used := map[string]struct{}{}
	primary := primaryServiceName(svcs)
	for i := range svcs {
		for j := range svcs[i].Ports {
			p := &svcs[i].Ports[j]
			if p.HostVar != "" && p.Name == "" {
				p.Name = portNameFromVar(p.HostVar)
			}
			if p.Name != "" {
				used[p.Name] = struct{}{}
			}
		}
	}
	for i := range svcs {
		for j := range svcs[i].Ports {
			p := &svcs[i].Ports[j]
			if p.Name != "" {
				continue
			}
			if p.Host <= 0 && p.HostVar == "" {
				continue
			}
			name := svcs[i].Name
			if svcs[i].Name == primary && j == 0 {
				name = "app"
			}
			base := name
			n := 2
			for {
				if _, ok := used[name]; !ok {
					break
				}
				name = fmt.Sprintf("%s-%d", base, n)
				n++
			}
			p.Name = name
			used[name] = struct{}{}
			if p.HostVar == "" {
				p.HostVar = envKey(name)
			}
		}
	}
}

func primaryServiceName(svcs []Service) string {
	prefer := []string{"app", "web", "api", "frontend", "backend", "server"}
	names := map[string]struct{}{}
	for _, s := range svcs {
		names[s.Name] = struct{}{}
	}
	for _, n := range prefer {
		if _, ok := names[n]; ok {
			return n
		}
	}
	for _, s := range svcs {
		if s.Build != nil {
			return s.Name
		}
	}
	if len(svcs) > 0 {
		return svcs[0].Name
	}
	return ""
}

func formatPublished(p Port) string {
	host := p.Host
	if host <= 0 {
		host = p.Container
	}
	if p.HostVar != "" {
		return fmt.Sprintf("${%s:-%d}:%d", p.HostVar, host, p.Container)
	}
	return fmt.Sprintf("%d:%d", host, p.Container)
}
