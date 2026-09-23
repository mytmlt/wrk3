package task

import (
	"fmt"
	"strconv"
	"strings"
)

// ComposeConfigOptions control compose YAML generation from a Task.
type ComposeConfigOptions struct {
	// Indent is the YAML indentation level (default 2).
	Indent int
}

// ToDockerCompose converts a Task back to a Docker Compose v3 YAML
// string.
func (t *Task) ToDockerCompose() string {
	return t.toComposeLike("")
}

// toComposeLike builds a compose-family YAML string. prefix is prepended
// to the top-level "services" key for swarm stack output ("services" vs
// nothing for compose).
func (t *Task) toComposeLike(prefix string) string {
	var b strings.Builder

	b.WriteString("version: \"3.8\"\n")

	if t.Name != "" {
		fmt.Fprintf(&b, "name: %s\n", t.Name)
	}

	b.WriteString(prefix)
	b.WriteString("services:\n")
	for _, svc := range t.Services {
		writeComposeService(&b, svc)
	}

	if len(t.Networks) > 0 {
		b.WriteString(prefix)
		b.WriteString("networks:\n")
		for _, nw := range t.Networks {
			writeComposeNetwork(&b, nw)
		}
	}

	if len(t.Volumes) > 0 {
		b.WriteString(prefix)
		b.WriteString("volumes:\n")
		for _, vol := range t.Volumes {
			writeComposeVolume(&b, vol)
		}
	}

	if len(t.Secrets) > 0 {
		b.WriteString(prefix)
		b.WriteString("secrets:\n")
		for _, sec := range t.Secrets {
			fmt.Fprintf(&b, "  %s:\n", sec.Name)
			if sec.File != "" {
				fmt.Fprintf(&b, "    file: %s\n", sec.File)
			}
			if sec.Env != "" {
				fmt.Fprintf(&b, "    environment: %s\n", sec.Env)
			}
		}
	}

	return b.String()
}

func writeComposeService(b *strings.Builder, svc Service) {
	fmt.Fprintf(&b, "  %s:\n", svc.Name)

	if svc.Image != "" {
		fmt.Fprintf(&b, "    image: %s\n", svc.Image)
	}
	if svc.Build != "" {
		fmt.Fprintf(&b, "    build: %s\n", svc.Build)
	}
	if svc.ContainerName != "" {
		fmt.Fprintf(&b, "    container_name: %s\n", svc.ContainerName)
	}
	if svc.Restart != "" {
		fmt.Fprintf(&b, "    restart: %s\n", svc.Restart)
	}
	if svc.WorkingDir != "" {
		fmt.Fprintf(&b, "    working_dir: %s\n", svc.WorkingDir)
	}
	if svc.User != "" {
		fmt.Fprintf(&b, "    user: %s\n", svc.User)
	}
	if svc.Command != nil {
		fmt.Fprintf(&b, "    command: [%s]\n", joinQuoted(svc.Command))
	}
	if svc.Entrypoint != nil {
		fmt.Fprintf(&b, "    entrypoint: [%s]\n", joinQuoted(svc.Entrypoint))
	}

	if len(svc.Environment) > 0 {
		b.WriteString("    environment:\n")
		for _, e := range svc.Environment {
			fmt.Fprintf(&b, "      %s=%s\n", e.Key, e.Value)
		}
	}

	if len(svc.Ports) > 0 {
		b.WriteString("    ports:\n")
		for _, p := range svc.Ports {
			fmt.Fprintf(&b, "      - \"%s\"\n", formatPort(p))
		}
	}

	if len(svc.Volumes) > 0 {
		b.WriteString("    volumes:\n")
		for _, v := range svc.Volumes {
			fmt.Fprintf(&b, "      - %s\n", formatVolume(v))
		}
	}

	if len(svc.Networks) > 0 {
		b.WriteString("    networks:\n")
		for _, nw := range svc.Networks {
			fmt.Fprintf(&b, "      - %s\n", nw.Name)
		}
	}

	if len(svc.DependsOn) > 0 {
		b.WriteString("    depends_on:\n")
		for _, d := range svc.DependsOn {
			fmt.Fprintf(&b, "      - %s\n", d)
		}
	}

	if len(svc.ExtraHosts) > 0 {
		b.WriteString("    extra_hosts:\n")
		for _, h := range svc.ExtraHosts {
			fmt.Fprintf(&b, "      - %s\n", h)
		}
	}

	if len(svc.DNS) > 0 {
		b.WriteString("    dns:\n")
		for _, d := range svc.DNS {
			fmt.Fprintf(&b, "      - %s\n", d)
		}
	}

	if svc.Privileged {
		b.WriteString("    privileged: true\n")
	}

	if svc.ResourceLimit != nil {
		b.WriteString("    deploy:\n")
		b.WriteString("      resources:\n")
		b.WriteString("        limits:\n")
		if svc.ResourceLimit.CPUs != "" {
			fmt.Fprintf(&b, "          cpus: %s\n", svc.ResourceLimit.CPUs)
		}
		if svc.ResourceLimit.Memory != "" {
			fmt.Fprintf(&b, "          memory: %s\n", svc.ResourceLimit.Memory)
		}
	}

	if svc.Health != nil {
		b.WriteString("    healthcheck:\n")
		if len(svc.Health.Test) > 0 {
			fmt.Fprintf(&b, "      test: [%s]\n", joinQuoted(svc.Health.Test))
		}
		if svc.Health.Interval > 0 {
			fmt.Fprintf(&b, "      interval: %s\n", svc.Health.Interval)
		}
		if svc.Health.Timeout > 0 {
			fmt.Fprintf(&b, "      timeout: %s\n", svc.Health.Timeout)
		}
		if svc.Health.Retries > 0 {
			fmt.Fprintf(&b, "      retries: %d\n", svc.Health.Retries)
		}
		if svc.Health.StartPeriod > 0 {
			fmt.Fprintf(&b, "      start_period: %s\n", svc.Health.StartPeriod)
		}
	}

	if len(svc.Labels) > 0 {
		b.WriteString("    labels:\n")
		for k, v := range svc.Labels {
			fmt.Fprintf(&b, "      %s: \"%s\"\n", k, escapeYAML(v))
		}
	}
}

func writeComposeNetwork(b *strings.Builder, nw Network) {
	fmt.Fprintf(&b, "  %s:\n", nw.Name)
	if nw.Driver != "" {
		fmt.Fprintf(&b, "    driver: %s\n", nw.Driver)
	}
	if nw.Internal {
		b.WriteString("    internal: true\n")
	}
}

func writeComposeVolume(b *strings.Builder, vol VolumeDef) {
	fmt.Fprintf(&b, "  %s:\n", vol.Name)
	if vol.Driver != "" {
		fmt.Fprintf(&b, "    driver: %s\n", vol.Driver)
	}
	if vol.External {
		b.WriteString("    external: true\n")
	}
}

func formatPort(p PortMapping) string {
	var b strings.Builder
	if p.Host != "" {
		b.WriteString(p.Host)
		b.WriteByte(':')
		b.WriteString(strconv.Itoa(p.HostPort))
		b.WriteByte(':')
	} else if p.HostPort > 0 {
		b.WriteString(strconv.Itoa(p.HostPort))
		b.WriteByte(':')
	}
	b.WriteString(strconv.Itoa(p.Container))
	if p.Protocol != "" && p.Protocol != "tcp" {
		b.WriteByte('/')
		b.WriteString(p.Protocol)
	}
	return b.String()
}

func formatVolume(v VolumeMapping) string {
	var b strings.Builder
	if v.Source != "" {
		b.WriteString(v.Source)
		b.WriteByte(':')
	}
	b.WriteString(v.Target)
	if v.ReadOnly {
		b.WriteString(":ro")
	}
	return b.String()
}

func joinQuoted(parts []string) string {
	quoted := make([]string, len(parts))
	for i, p := range parts {
		quoted[i] = strconv.Quote(p)
	}
	return strings.Join(quoted, ", ")
}

func escapeYAML(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

// ToDockerSwarm converts a Task to a Docker Swarm stack YAML string.
// Swarm stack format is almost identical to compose v3, so the converter
// reuses the compose output. Differences (notable in deploy) are
// preserved through the IR.
func (t *Task) ToDockerSwarm() string {
	return t.toComposeLike("")
}

// ToPortainerStackJSON converts a Task to a Portainer stack JSON string.
// Portainer stacks are valid compose YAML embedded in a JSON API payload
// with the compose body as a base64 or inline string. This produces the
// inline form.
func (t *Task) ToPortainerStackJSON() string {
	compose := t.ToDockerCompose()
	escaped := strconv.Quote(compose)
	return fmt.Sprintf("{\n  \"StackFileContent\": %s\n}", escaped)
}

// ToDirectRun generates a sequence of shell commands to run the task
// directly on the host (without containers). Each service is translated
// into a foreground command using env vars and the defined ports. This
// is a best-effort converter — services that depend on container images
// cannot be run natively.
func (t *Task) ToDirectRun() string {
	var b strings.Builder

	b.WriteString("#!/bin/sh\n")
	b.WriteString("# Generated by wrk3 task direct-run converter\n")
	b.WriteString("# Run each service directly on the host.\n\n")

	for _, svc := range t.Services {
		fmt.Fprintf(&b, "# --- service: %s ---\n", svc.Name)

		for _, e := range svc.Environment {
			fmt.Fprintf(&b, "export %s=%s\n", e.Key, shellQuote(e.Value))
		}

		if len(svc.Ports) > 0 {
			for _, p := range svc.Ports {
				key := strings.ToUpper(svc.Name) + "_PORT"
				fmt.Fprintf(&b, "export %s=%d\n", key, p.Container)
			}
		}

		if svc.Build != "" {
			b.WriteString("# build context: ")
			b.WriteString(svc.Build)
			b.WriteByte('\n')
		}

		if len(svc.Command) > 0 {
			fmt.Fprintf(&b, "%s &\n", strings.Join(svc.Command, " "))
		} else if svc.Image != "" {
			fmt.Fprintf(&b, "echo 'direct-run: service %q uses image %q; cannot run natively'\n",
				svc.Name, svc.Image)
		}

		if len(svc.DependsOn) > 0 {
			fmt.Fprintf(&b, "# depends on: %s\n",
				strings.Join(svc.DependsOn, ", "))
		}

		b.WriteByte('\n')
	}

	b.WriteString("wait\n")
	return b.String()
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t\n\"'$`\\*?[]{}()&|;<>#!~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
