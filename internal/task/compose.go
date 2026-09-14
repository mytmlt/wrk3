package task

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type rawCompose struct {
	Name     string         `yaml:"name"`
	Services map[string]any `yaml:"services"`
	Networks map[string]any `yaml:"networks"`
	Volumes  map[string]any `yaml:"volumes"`
}

func parseComposeBytes(raw []byte) (rawCompose, error) {
	var doc rawCompose
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return rawCompose{}, fmt.Errorf("parse compose: %w", err)
	}
	return doc, nil
}

func mergeCompose(dst *rawCompose, src rawCompose) {
	if strings.TrimSpace(src.Name) != "" {
		dst.Name = src.Name
	}
	if dst.Services == nil {
		dst.Services = map[string]any{}
	}
	for name, svc := range src.Services {
		existing, ok := dst.Services[name]
		srcMap, srcOK := asMap(svc)
		dstMap, dstOK := asMap(existing)
		if ok && srcOK && dstOK {
			merged := map[string]any{}
			for k, v := range dstMap {
				merged[k] = v
			}
			for k, v := range srcMap {
				merged[k] = v
			}
			dst.Services[name] = merged
			continue
		}
		dst.Services[name] = svc
	}
	if dst.Networks == nil {
		dst.Networks = map[string]any{}
	}
	for name, n := range src.Networks {
		dst.Networks[name] = n
	}
	if dst.Volumes == nil {
		dst.Volumes = map[string]any{}
	}
	for name, v := range src.Volumes {
		dst.Volumes[name] = v
	}
}

func loadCompose(paths []string) (rawCompose, error) {
	var doc rawCompose
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			return rawCompose{}, fmt.Errorf("read compose %q: %w", p, err)
		}
		next, err := parseComposeBytes(raw)
		if err != nil {
			return rawCompose{}, fmt.Errorf("%s: %w", p, err)
		}
		mergeCompose(&doc, next)
	}
	return doc, nil
}

func servicesFromCompose(doc rawCompose) ([]Service, []Note, error) {
	names := sortedKeys(doc.Services)
	svcs := make([]Service, 0, len(names))
	var notes []Note
	for _, name := range names {
		svc, svcNotes, err := serviceFromCompose(name, doc.Services[name])
		if err != nil {
			return nil, nil, fmt.Errorf("service %q: %w", name, err)
		}
		svcs = append(svcs, svc)
		notes = append(notes, svcNotes...)
	}
	inferPortNames(svcs)
	return svcs, notes, nil
}

func serviceFromCompose(name string, raw any) (Service, []Note, error) {
	m, ok := asMap(raw)
	if !ok || m == nil {
		return Service{Name: name}, nil, nil
	}
	s := Service{Name: name}
	s.Image = strings.TrimSpace(asString(m["image"]))
	if b, err := parseBuild(m["build"]); err != nil {
		return Service{}, nil, err
	} else {
		s.Build = b
	}
	s.Command, s.CommandShell = commandSpec(m["command"])
	s.Entrypoint, s.EntrypointShell = commandSpec(m["entrypoint"])
	s.WorkDir = strings.TrimSpace(asString(m["working_dir"]))
	s.User = strings.TrimSpace(asString(m["user"]))
	s.Env = stringMap(m["environment"])
	s.EnvFiles = asStringSlice(m["env_file"])
	ports, portNotes, err := parsePorts(m["ports"])
	if err != nil {
		return Service{}, nil, err
	}
	s.Ports = ports
	mounts, err := parseMounts(m["volumes"])
	if err != nil {
		return Service{}, nil, err
	}
	s.Mounts = mounts
	s.DependsOn = dependsOn(m["depends_on"])
	s.Networks = networkNames(m["networks"])
	s.Restart = strings.TrimSpace(asString(m["restart"]))
	s.Healthcheck = parseHealthcheck(m["healthcheck"])
	s.Privileged = asBool(m["privileged"])
	s.NetworkMode = strings.TrimSpace(asString(m["network_mode"]))
	s.FixedContainerName = strings.TrimSpace(asString(m["container_name"]))
	if d, ok := asMap(m["deploy"]); ok {
		if r, ok := asInt(d["replicas"]); ok {
			s.Replicas = r
		}
	}
	var notes []Note
	for _, n := range portNotes {
		n.Service = name
		notes = append(notes, n)
	}
	if s.FixedContainerName != "" {
		notes = append(notes, Note{
			Level:   levelInfo,
			Service: name,
			Code:    codeContainerName,
			Message: fmt.Sprintf("container_name %q is stripped on project (source unchanged)", s.FixedContainerName),
		})
	}
	if s.NetworkMode == "host" {
		notes = append(notes, Note{
			Level:   levelWarn,
			Service: name,
			Code:    codeHostNetwork,
			Message: "network_mode host is incompatible with isolated published ports",
		})
	}
	return s, notes, nil
}

func parseBuild(v any) (*Build, error) {
	if v == nil {
		return nil, nil
	}
	if s, ok := v.(string); ok {
		s = strings.TrimSpace(s)
		if s == "" {
			return nil, nil
		}
		return &Build{Context: s}, nil
	}
	m, ok := asMap(v)
	if !ok {
		return nil, fmt.Errorf("invalid build spec")
	}
	b := &Build{
		Context:    strings.TrimSpace(asString(m["context"])),
		Dockerfile: strings.TrimSpace(asString(m["dockerfile"])),
		Target:     strings.TrimSpace(asString(m["target"])),
		Args:       stringMap(m["args"]),
	}
	if b.Context == "" {
		b.Context = "."
	}
	return b, nil
}

func parseHealthcheck(v any) *Healthcheck {
	m, ok := asMap(v)
	if !ok || m == nil {
		return nil
	}
	h := &Healthcheck{
		Interval:    strings.TrimSpace(asString(m["interval"])),
		Timeout:     strings.TrimSpace(asString(m["timeout"])),
		StartPeriod: strings.TrimSpace(asString(m["start_period"])),
	}
	if n, ok := asInt(m["retries"]); ok {
		h.Retries = n
	}
	h.Test, h.TestShell = commandSpec(m["test"])
	if len(h.Test) == 0 && h.TestShell == "" && h.Interval == "" && h.Timeout == "" && h.Retries == 0 && h.StartPeriod == "" {
		return nil
	}
	return h
}

func networkNames(v any) []string {
	if v == nil {
		return nil
	}
	if m, ok := asMap(v); ok {
		return sortedKeys(m)
	}
	return asStringSlice(v)
}

func networksFromCompose(doc rawCompose) []Network {
	if len(doc.Networks) == 0 {
		return nil
	}
	var out []Network
	for _, name := range sortedKeys(doc.Networks) {
		n := Network{Name: name}
		if m, ok := asMap(doc.Networks[name]); ok {
			n.Driver = strings.TrimSpace(asString(m["driver"]))
		}
		out = append(out, n)
	}
	return out
}

func volumesFromCompose(doc rawCompose, svcs []Service) []Volume {
	seen := map[string]struct{}{}
	var out []Volume
	add := func(name, driver string) {
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		out = append(out, Volume{Name: name, Driver: driver})
	}
	for _, name := range sortedKeys(doc.Volumes) {
		driver := ""
		if m, ok := asMap(doc.Volumes[name]); ok {
			driver = strings.TrimSpace(asString(m["driver"]))
		}
		add(name, driver)
	}
	for _, src := range namedVolumeSources(svcs) {
		add(src, "")
	}
	return out
}
