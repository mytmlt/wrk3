package task

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type rawHost struct {
	Name     string         `yaml:"name"`
	Services map[string]any `yaml:"services"`
}

func parseHostBytes(raw []byte) (rawHost, error) {
	var doc rawHost
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return rawHost{}, fmt.Errorf("parse host plan: %w", err)
	}
	return doc, nil
}

func loadHost(paths []string) ([]Service, string, error) {
	var svcs []Service
	name := ""
	seen := map[string]int{}
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil, "", fmt.Errorf("read host plan %q: %w", p, err)
		}
		var t Task
		if err := yaml.Unmarshal(raw, &t); err == nil && len(t.Services) > 0 && t.Services[0].Name != "" {
			if strings.TrimSpace(t.Name) != "" {
				name = t.Name
			}
			for _, s := range t.Services {
				if i, ok := seen[s.Name]; ok {
					svcs[i] = s
					continue
				}
				seen[s.Name] = len(svcs)
				svcs = append(svcs, s)
			}
			continue
		}
		doc, err := parseHostBytes(raw)
		if err != nil {
			return nil, "", fmt.Errorf("%s: %w", p, err)
		}
		if strings.TrimSpace(doc.Name) != "" {
			name = doc.Name
		}
		for _, svcName := range sortedKeys(doc.Services) {
			s, err := serviceFromHost(svcName, doc.Services[svcName])
			if err != nil {
				return nil, "", fmt.Errorf("%s service %q: %w", p, svcName, err)
			}
			if i, ok := seen[s.Name]; ok {
				svcs[i] = s
				continue
			}
			seen[s.Name] = len(svcs)
			svcs = append(svcs, s)
		}
	}
	inferPortNames(svcs)
	return svcs, name, nil
}

func serviceFromHost(name string, raw any) (Service, error) {
	m, ok := asMap(raw)
	if !ok || m == nil {
		return Service{Name: name}, nil
	}
	s := Service{Name: name}
	s.Image = strings.TrimSpace(asString(m["image"]))
	if b, err := parseBuild(m["build"]); err != nil {
		return Service{}, err
	} else {
		s.Build = b
	}
	if _, ok := m["command"]; ok {
		s.Command, s.CommandShell = commandSpec(m["command"])
	} else if _, ok := m["exec"]; ok {
		s.Command, s.CommandShell = commandSpec(m["exec"])
	}
	s.WorkDir = strings.TrimSpace(asString(m["workdir"]))
	if s.WorkDir == "" {
		s.WorkDir = strings.TrimSpace(asString(m["working_dir"]))
	}
	s.Env = stringMap(m["environment"])
	if s.Env == nil {
		s.Env = stringMap(m["env"])
	}
	if v, ok := m["ports"]; ok {
		ports, _, err := parsePorts(v)
		if err != nil {
			return Service{}, err
		}
		s.Ports = ports
	} else if v, ok := m["port"]; ok {
		ports, _, err := parsePorts(v)
		if err != nil {
			return Service{}, err
		}
		s.Ports = ports
	}
	mounts, err := parseMounts(m["volumes"])
	if err != nil {
		return Service{}, err
	}
	if mounts == nil {
		mounts, err = parseMounts(m["mounts"])
		if err != nil {
			return Service{}, err
		}
	}
	s.Mounts = mounts
	s.DependsOn = dependsOn(m["dependsOn"])
	if s.DependsOn == nil {
		s.DependsOn = dependsOn(m["depends_on"])
	}
	return s, nil
}
