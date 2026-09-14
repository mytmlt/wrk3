package task

import "fmt"

// NotesFor returns projection diagnostics for env. Source-analysis
// notes (hardcoded ports, container_name) are included for every env.
func (t Task) NotesFor(env Environment, extra []Note) []Note {
	var out []Note
	out = append(out, extra...)
	for _, s := range t.Services {
		if s.FixedContainerName != "" {
			out = append(out, Note{
				Level:   levelInfo,
				Service: s.Name,
				Code:    codeContainerName,
				Message: fmt.Sprintf("container_name %q will not be emitted (source unchanged)", s.FixedContainerName),
			})
		}
		if s.NetworkMode == "host" {
			level := levelWarn
			if env == EnvSwarm {
				level = levelBlock
			}
			out = append(out, Note{
				Level:   level,
				Service: s.Name,
				Code:    codeHostNetwork,
				Message: "network_mode host cannot be projected onto isolated environments",
			})
		}
		if s.Privileged && (env == EnvSwarm || env == EnvPortainer) {
			out = append(out, Note{
				Level:   levelWarn,
				Service: s.Name,
				Code:    codePrivileged,
				Message: "privileged: true is often rejected on remote orchestrators",
			})
		}
		if env == EnvSwarm && s.Image == "" {
			out = append(out, Note{
				Level:   levelBlock,
				Service: s.Name,
				Code:    codeMissingImage,
				Message: "swarm requires an image (compose build is not used by docker stack deploy)",
			})
		}
		if env == EnvSwarm {
			for _, m := range s.Mounts {
				if m.Type == "bind" {
					out = append(out, Note{
						Level:   levelWarn,
						Service: s.Name,
						Code:    codeBindMount,
						Message: fmt.Sprintf("bind mount %s -> %s dropped for swarm (not portable)", m.Source, m.Target),
					})
				}
			}
		}
		if env == EnvHost && s.Image == "" && s.Build != nil && s.CommandShell == "" && len(s.Command) == 0 {
			out = append(out, Note{
				Level:   levelInfo,
				Service: s.Name,
				Code:    "build_only",
				Message: "host plan will docker build then docker run the resulting tag",
			})
		}
	}
	return dedupeNotes(out)
}

func blocking(notes []Note) []Note {
	var out []Note
	for _, n := range notes {
		if n.Level == levelBlock {
			out = append(out, n)
		}
	}
	return out
}

func dedupeNotes(in []Note) []Note {
	type key struct{ level, service, code, message string }
	seen := map[key]struct{}{}
	var out []Note
	for _, n := range in {
		k := key{n.Level, n.Service, n.Code, n.Message}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, n)
	}
	return out
}

func blockError(env Environment, notes []Note) error {
	blocks := blocking(notes)
	if len(blocks) == 0 {
		return nil
	}
	n := blocks[0]
	svc := n.Service
	if svc == "" {
		svc = "stack"
	}
	return fmt.Errorf("cannot render %s for %s: %s", env, svc, n.Message)
}
