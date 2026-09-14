package task

import (
	"fmt"
	"sort"
	"strings"
)

func renderHost(t Task) ([]byte, error) {
	services := make(orderedMap, 0, len(t.Services))
	for _, s := range t.Services {
		item := orderedMap{}
		if s.Image != "" {
			item = append(item, kv{"image", s.Image})
		}
		if s.Build != nil {
			item = append(item, kv{"build", composeBuild(s.Build)})
		}
		if s.WorkDir != "" {
			item = append(item, kv{"workdir", s.WorkDir})
		}
		if env := envPairs(s.Env); len(env) > 0 {
			item = append(item, kv{"env", env})
		}
		if len(s.Ports) > 0 {
			item = append(item, kv{"ports", s.Ports})
		}
		if len(s.Mounts) > 0 {
			item = append(item, kv{"mounts", s.Mounts})
		}
		if len(s.DependsOn) > 0 {
			item = append(item, kv{"dependsOn", s.DependsOn})
		}
		if v := cmdValue(s.Command, s.CommandShell); v != nil && s.Image == "" && s.Build == nil {
			item = append(item, kv{"command", v})
			item = append(item, kv{"kind", "process"})
		} else {
			item = append(item, kv{"kind", "container"})
		}
		item = append(item, kv{"exec", hostExec(t.Name, s)})
		services = append(services, kv{s.Name, item})
	}
	doc := orderedMap{
		{"name", t.Name},
		{"services", services},
	}
	return marshalYAML(doc)
}

func hostExec(taskName string, s Service) string {
	if s.Image == "" && s.Build == nil {
		return hostProcessExec(s)
	}
	return hostDockerExec(taskName, s)
}

func hostProcessExec(s Service) string {
	if s.CommandShell != "" {
		if s.WorkDir != "" {
			return "sh -c " + shellQuote("cd "+shellQuote(s.WorkDir)+" && "+s.CommandShell)
		}
		return s.CommandShell
	}
	parts := make([]string, 0, len(s.Command))
	for _, p := range s.Command {
		parts = append(parts, shellQuote(p))
	}
	cmd := strings.Join(parts, " ")
	if s.WorkDir != "" {
		return "sh -c " + shellQuote("cd "+shellQuote(s.WorkDir)+" && "+cmd)
	}
	return cmd
}

func hostDockerExec(taskName string, s Service) string {
	var b strings.Builder
	tag := fmt.Sprintf("${COMPOSE_PROJECT_NAME:-%s}-%s", sanitizeIdent(taskName), sanitizeIdent(s.Name))
	if s.Build != nil {
		ctx := s.Build.Context
		if ctx == "" {
			ctx = "."
		}
		b.WriteString("docker build -t ")
		b.WriteString(tag)
		if s.Build.Dockerfile != "" {
			b.WriteString(" -f ")
			b.WriteString(shellQuote(s.Build.Dockerfile))
		}
		if s.Build.Target != "" {
			b.WriteString(" --target ")
			b.WriteString(shellQuote(s.Build.Target))
		}
		keys := make([]string, 0, len(s.Build.Args))
		for k := range s.Build.Args {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			b.WriteString(" --build-arg ")
			b.WriteString(shellQuote(k + "=" + s.Build.Args[k]))
		}
		b.WriteByte(' ')
		b.WriteString(shellQuote(ctx))
		b.WriteString(" && ")
	}
	b.WriteString("docker run --rm --name ")
	b.WriteString(tag)
	keys := make([]string, 0, len(s.Env))
	for k := range s.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b.WriteString(" -e ")
		b.WriteString(shellQuote(k + "=" + s.Env[k]))
	}
	for _, p := range s.Ports {
		b.WriteString(" -p ")
		if p.HostIP != "" {
			b.WriteString(p.HostIP)
			b.WriteByte(':')
		}
		b.WriteString(formatPublished(p))
		if p.Protocol != "" && p.Protocol != "tcp" {
			b.WriteByte('/')
			b.WriteString(p.Protocol)
		}
	}
	for _, m := range s.Mounts {
		b.WriteString(" -v ")
		b.WriteString(shellQuote(formatMount(m)))
	}
	if s.WorkDir != "" {
		b.WriteString(" -w ")
		b.WriteString(shellQuote(s.WorkDir))
	}
	if s.User != "" {
		b.WriteString(" -u ")
		b.WriteString(shellQuote(s.User))
	}
	if s.Privileged {
		b.WriteString(" --privileged")
	}
	b.WriteByte(' ')
	if s.Image != "" {
		b.WriteString(shellQuote(s.Image))
	} else {
		b.WriteString(tag)
	}
	if s.CommandShell != "" {
		b.WriteString(" sh -c ")
		b.WriteString(shellQuote(s.CommandShell))
	} else if len(s.Command) > 0 {
		for _, p := range s.Command {
			b.WriteByte(' ')
			b.WriteString(shellQuote(p))
		}
	}
	return b.String()
}

func sanitizeIdent(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	prev := true
	for _, r := range s {
		ok := r >= 'a' && r <= 'z' || r >= '0' && r <= '9'
		if ok {
			b.WriteRune(r)
			prev = false
			continue
		}
		if !prev {
			b.WriteByte('-')
			prev = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "wrk3"
	}
	return out
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	safe := true
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			continue
		}
		switch r {
		case '-', '_', '.', '/', ':', ',', '=', '+', '@', '{', '}', '$':
			continue
		}
		safe = false
		break
	}
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}
