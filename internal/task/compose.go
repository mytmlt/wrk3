package task

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// DefaultComposeFiles is the compose-spec search order used when the
// caller does not name files (compose.yaml preferred).
var DefaultComposeFiles = []string{
	"compose.yaml",
	"compose.yml",
	"docker-compose.yml",
	"docker-compose.yaml",
}

// DiscoverComposeFiles returns the first default compose filename that
// exists in dir. Empty when none are present.
func DiscoverComposeFiles(dir string) []string {
	for _, name := range DefaultComposeFiles {
		p := filepath.Join(dir, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return []string{name}
		}
	}
	return nil
}

// LoadCompose analyzes compose files under dir into a Definition.
// Relative files resolve against dir. Source files are only read.
// Later files overlay earlier ones (compose mapping-merge, sequence replace).
func LoadCompose(dir string, files []string) (*Definition, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, fmt.Errorf("compose dir must not be empty")
	}
	if len(files) == 0 {
		files = DiscoverComposeFiles(dir)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no compose file found in %s (looked for %s)", dir, strings.Join(DefaultComposeFiles, ", "))
	}

	merged := map[string]any{}
	var used []string
	for _, f := range files {
		if strings.TrimSpace(f) == "" {
			continue
		}
		p := f
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, f)
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("read compose file %q: %w", f, err)
		}
		var doc map[string]any
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			return nil, fmt.Errorf("parse compose file %q: %w", f, err)
		}
		if doc == nil {
			continue
		}
		mergeDoc(merged, doc)
		used = append(used, f)
	}
	if len(used) == 0 {
		return nil, fmt.Errorf("no compose file found in %s", dir)
	}

	def, err := definitionFromDoc(merged)
	if err != nil {
		return nil, err
	}
	def.Source = Source{Kind: SourceCompose, Files: used}
	if def.Name == "" {
		def.Name = filepath.Base(filepath.Clean(dir))
	}
	if len(def.Services) == 0 {
		return nil, fmt.Errorf("compose files %s: no services", strings.Join(used, ", "))
	}
	return def, nil
}

func mergeDoc(dst, src map[string]any) {
	for k, v := range src {
		dv, ok := dst[k]
		if !ok {
			dst[k] = v
			continue
		}
		dm := asMap(dv)
		sm := asMap(v)
		if dm != nil && sm != nil {
			mergeDoc(dm, sm)
			dst[k] = dm
			continue
		}
		dst[k] = v
	}
}

func definitionFromDoc(doc map[string]any) (*Definition, error) {
	def := &Definition{
		Name: strings.TrimSpace(asString(doc["name"])),
	}
	svcMap := asMap(doc["services"])
	if svcMap == nil {
		return def, nil
	}
	names := make([]string, 0, len(svcMap))
	for name := range svcMap {
		if strings.TrimSpace(name) != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		raw := asMap(svcMap[name])
		if raw == nil {
			raw = map[string]any{}
		}
		def.Services = append(def.Services, serviceFromMap(name, raw))
	}

	if nets := asMap(doc["networks"]); nets != nil {
		nnames := make([]string, 0, len(nets))
		for n := range nets {
			if strings.TrimSpace(n) != "" {
				nnames = append(nnames, n)
			}
		}
		sort.Strings(nnames)
		for _, n := range nnames {
			def.Networks = append(def.Networks, networkFromValue(n, nets[n]))
		}
	}
	if vols := asMap(doc["volumes"]); vols != nil {
		vnames := make([]string, 0, len(vols))
		for n := range vols {
			if strings.TrimSpace(n) != "" {
				vnames = append(vnames, n)
			}
		}
		sort.Strings(vnames)
		for _, n := range vnames {
			def.Volumes = append(def.Volumes, volumeFromValue(n, vols[n]))
		}
	}
	return def, nil
}

func serviceFromMap(name string, m map[string]any) Service {
	s := Service{
		Name:          name,
		Runtime:       RuntimeContainer,
		Image:         strings.TrimSpace(asString(m["image"])),
		Build:         asBuild(m["build"]),
		Command:       asCmd(m["command"]),
		Entrypoint:    asCmd(m["entrypoint"]),
		Env:           asEnvMap(m["environment"]),
		EnvFiles:      asStringSlice(m["env_file"]),
		Ports:         asPorts(m["ports"]),
		Mounts:        asMounts(m["volumes"]),
		DependsOn:     asDependsOn(m["depends_on"]),
		WorkingDir:    strings.TrimSpace(asString(m["working_dir"])),
		User:          strings.TrimSpace(asString(m["user"])),
		Restart:       strings.TrimSpace(asString(m["restart"])),
		Healthcheck:   asHealthcheck(m["healthcheck"]),
		Networks:      asServiceNetworks(m["networks"]),
		Privileged:    asBool(m["privileged"]),
		NetworkMode:   strings.TrimSpace(asString(m["network_mode"])),
		Deploy:        asDeploy(m["deploy"]),
		ContainerName: strings.TrimSpace(asString(m["container_name"])),
	}
	sort.Strings(s.DependsOn)
	sort.Strings(s.Networks)
	return s
}

func networkFromValue(name string, v any) Network {
	n := Network{Name: name}
	m := asMap(v)
	if m == nil {
		return n
	}
	n.Driver = strings.TrimSpace(asString(m["driver"]))
	if ext := m["external"]; ext != nil {
		if em := asMap(ext); em != nil {
			n.External = true
		} else {
			n.External = asBool(ext)
		}
	}
	return n
}

func volumeFromValue(name string, v any) Volume {
	vol := Volume{Name: name}
	m := asMap(v)
	if m == nil {
		return vol
	}
	if ext := m["external"]; ext != nil {
		if em := asMap(ext); em != nil {
			vol.External = true
		} else {
			vol.External = asBool(ext)
		}
	}
	return vol
}
