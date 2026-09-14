package task

func renderCompose(t Task, portainer bool) ([]byte, error) {
	services := make(orderedMap, 0, len(t.Services))
	for _, s := range t.Services {
		services = append(services, kv{s.Name, composeService(s, false)})
	}
	doc := orderedMap{
		{"name", t.Name},
		{"services", services},
	}
	if nets := composeNetworks(t, false); len(nets) > 0 {
		doc = append(doc, kv{"networks", nets})
	}
	if vols := composeVolumes(t); len(vols) > 0 {
		doc = append(doc, kv{"volumes", vols})
	}
	_ = portainer
	return marshalYAML(doc)
}

func composeService(s Service, swarm bool) orderedMap {
	out := orderedMap{}
	if s.Image != "" {
		out = append(out, kv{"image", s.Image})
	}
	if s.Build != nil && !swarm {
		out = append(out, kv{"build", composeBuild(s.Build)})
	} else if s.Image == "" && !swarm {
		ctx := s.WorkDir
		if ctx == "" {
			ctx = "."
		}
		out = append(out, kv{"build", ctx})
	}
	if v := cmdValue(s.Entrypoint, s.EntrypointShell); v != nil {
		out = append(out, kv{"entrypoint", v})
	}
	if v := cmdValue(s.Command, s.CommandShell); v != nil {
		out = append(out, kv{"command", v})
	}
	if s.WorkDir != "" {
		out = append(out, kv{"working_dir", s.WorkDir})
	}
	if s.User != "" {
		out = append(out, kv{"user", s.User})
	}
	if env := envPairs(s.Env); len(env) > 0 {
		out = append(out, kv{"environment", env})
	}
	if len(s.EnvFiles) > 0 && !swarm {
		out = append(out, kv{"env_file", s.EnvFiles})
	}
	if ports := composePorts(s.Ports, swarm); ports != nil {
		out = append(out, kv{"ports", ports})
	}
	if vols := composeMounts(s.Mounts, swarm); len(vols) > 0 {
		out = append(out, kv{"volumes", vols})
	}
	if len(s.DependsOn) > 0 && !swarm {
		out = append(out, kv{"depends_on", s.DependsOn})
	}
	if len(s.Networks) > 0 {
		out = append(out, kv{"networks", s.Networks})
	}
	if s.Restart != "" && !swarm {
		out = append(out, kv{"restart", s.Restart})
	}
	if s.Healthcheck != nil {
		out = append(out, kv{"healthcheck", composeHealth(s.Healthcheck)})
	}
	if s.Privileged && !swarm {
		out = append(out, kv{"privileged", true})
	}
	if s.NetworkMode != "" && s.NetworkMode != "host" && !swarm {
		out = append(out, kv{"network_mode", s.NetworkMode})
	}
	if swarm {
		replicas := s.Replicas
		if replicas <= 0 {
			replicas = 1
		}
		deploy := orderedMap{{"replicas", replicas}}
		if s.Restart != "" {
			deploy = append(deploy, kv{"restart_policy", orderedMap{{"condition", swarmRestart(s.Restart)}}})
		}
		out = append(out, kv{"deploy", deploy})
	}
	return out
}

func composeBuild(b *Build) any {
	if b.Dockerfile == "" && b.Target == "" && len(b.Args) == 0 {
		return b.Context
	}
	out := orderedMap{{"context", b.Context}}
	if b.Dockerfile != "" {
		out = append(out, kv{"dockerfile", b.Dockerfile})
	}
	if b.Target != "" {
		out = append(out, kv{"target", b.Target})
	}
	if args := envPairs(b.Args); len(args) > 0 {
		out = append(out, kv{"args", args})
	}
	return out
}

func composePorts(ports []Port, swarm bool) any {
	if len(ports) == 0 {
		return nil
	}
	if !swarm {
		out := make([]string, 0, len(ports))
		for _, p := range ports {
			s := formatPublished(p)
			if p.Protocol != "" && p.Protocol != "tcp" {
				s += "/" + p.Protocol
			}
			if p.HostIP != "" {
				s = p.HostIP + ":" + s
			}
			out = append(out, s)
		}
		return out
	}
	out := make([]orderedMap, 0, len(ports))
	for _, p := range ports {
		host := p.Host
		if host <= 0 {
			host = p.Container
		}
		proto := p.Protocol
		if proto == "" {
			proto = "tcp"
		}
		item := orderedMap{
			{"target", p.Container},
			{"published", host},
			{"protocol", proto},
			{"mode", "ingress"},
		}
		out = append(out, item)
	}
	return out
}

func composeMounts(mounts []Mount, swarm bool) []string {
	var out []string
	for _, m := range mounts {
		if swarm && m.Type == "bind" {
			continue
		}
		out = append(out, formatMount(m))
	}
	return out
}

func composeHealth(h *Healthcheck) orderedMap {
	out := orderedMap{}
	if v := cmdValue(h.Test, h.TestShell); v != nil {
		out = append(out, kv{"test", v})
	}
	if h.Interval != "" {
		out = append(out, kv{"interval", h.Interval})
	}
	if h.Timeout != "" {
		out = append(out, kv{"timeout", h.Timeout})
	}
	if h.Retries > 0 {
		out = append(out, kv{"retries", h.Retries})
	}
	if h.StartPeriod != "" {
		out = append(out, kv{"start_period", h.StartPeriod})
	}
	return out
}

func composeNetworks(t Task, overlay bool) orderedMap {
	if len(t.Networks) == 0 {
		return nil
	}
	out := make(orderedMap, 0, len(t.Networks))
	for _, n := range t.Networks {
		m := orderedMap{}
		driver := n.Driver
		if overlay {
			driver = "overlay"
		}
		if driver != "" {
			m = append(m, kv{"driver", driver})
		}
		out = append(out, kv{n.Name, m})
	}
	return out
}

func composeVolumes(t Task) orderedMap {
	if len(t.Volumes) == 0 {
		return nil
	}
	out := make(orderedMap, 0, len(t.Volumes))
	for _, v := range t.Volumes {
		m := orderedMap{}
		if v.Driver != "" {
			m = append(m, kv{"driver", v.Driver})
		}
		if len(m) == 0 {
			out = append(out, kv{v.Name, orderedMap{}})
			continue
		}
		out = append(out, kv{v.Name, m})
	}
	return out
}

func swarmRestart(restart string) string {
	switch restart {
	case "no", "never":
		return "none"
	case "always", "unless-stopped":
		return "any"
	default:
		return "on-failure"
	}
}
