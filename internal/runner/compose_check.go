package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// composeFile mirrors just enough of the compose spec for the
// container_name preflight check: top-level services mapping to
// each service's raw config map.
type composeFile struct {
	Services map[string]map[string]any `yaml:"services"`
}

// CheckComposeFiles scans the compose files (resolved against
// worktreePath) for hardcoded `container_name:` entries and errors
// on the first offending file. A static container_name is global
// on the docker daemon, so it bypasses `compose -p <project>`
// isolation and makes parallel worktrees fail with
// "Conflict. The container name ... is already in use".
//
// Missing or unparsable files are skipped so the later
// `docker compose` invocation can report the real error.
func CheckComposeFiles(worktreePath string, files []string) error {
	for _, f := range files {
		if strings.TrimSpace(f) == "" {
			continue
		}
		p := f
		if !filepath.IsAbs(p) {
			p = filepath.Join(worktreePath, f)
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		offenders, err := containerNameServices(raw)
		if err != nil {
			continue
		}
		if len(offenders) > 0 {
			return fmt.Errorf("compose file %q sets container_name for service(s) %s: remove container_name so parallel worktrees get isolated names (<project>-<service>-1)",
				f, strings.Join(offenders, ", "))
		}
	}
	return nil
}

// containerNameServices returns the sorted names of services in a
// compose document that set a non-empty `container_name:`.
func containerNameServices(raw []byte) ([]string, error) {
	var doc composeFile
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse compose file: %w", err)
	}
	var out []string
	for name, svc := range doc.Services {
		v, ok := svc["container_name"]
		if !ok || v == nil {
			continue
		}
		if s, ok := v.(string); ok && strings.TrimSpace(s) == "" {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}
