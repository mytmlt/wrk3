package task

import "sort"

// Merge folds other into d as a compose override: same-named services are
// merged field-by-field (other wins on scalars, maps are unioned with
// other's values taking precedence, ports/volumes append and dependencies
// are unioned), and top-level volumes/networks are unioned. It is the
// basis for analyzing a `-f base.yml -f override.yml` stack.
func (d *Definition) Merge(other *Definition) {
	if d == nil || other == nil {
		return
	}
	d.Normalize()
	other.Normalize()
	if other.Name != "" {
		d.Name = other.Name
	}
	for name, osvc := range other.Services {
		if base, ok := d.Services[name]; ok {
			base.merge(osvc)
			continue
		}
		d.Services[name] = osvc
	}
	for name, v := range other.Volumes {
		d.Volumes[name] = v
	}
	for name, n := range other.Networks {
		d.Networks[name] = n
	}
	d.Normalize()
}

func (s *Service) merge(o *Service) {
	if s == nil || o == nil {
		return
	}
	if o.Image != "" {
		s.Image = o.Image
	}
	if o.Build != nil {
		s.Build = o.Build
	}
	if len(o.Command) > 0 {
		s.Command = o.Command
	}
	if len(o.Entrypoint) > 0 {
		s.Entrypoint = o.Entrypoint
	}
	if o.Restart != "" {
		s.Restart = o.Restart
	}
	if o.Healthcheck != nil {
		s.Healthcheck = o.Healthcheck
	}
	if o.Deploy != nil {
		s.Deploy = o.Deploy
	}
	for k, v := range o.Environment {
		s.Environment[k] = v
	}
	for k, v := range o.Labels {
		s.Labels[k] = v
	}
	s.Ports = append(s.Ports, o.Ports...)
	s.Volumes = append(s.Volumes, o.Volumes...)
	s.DependsOn = unionStrings(s.DependsOn, o.DependsOn)
	s.Networks = unionStrings(s.Networks, o.Networks)
}

func unionStrings(a, b []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(a)+len(b))
	for _, s := range append(append([]string(nil), a...), b...) {
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
