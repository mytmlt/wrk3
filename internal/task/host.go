package task

import (
	"fmt"
	"sort"
	"strings"
)

// HostPlan is the machine-native rendering of a Definition: processes
// started with cwd/env/ports, no compose project.
type HostPlan struct {
	Name      string        `json:"name" yaml:"name"`
	Processes []HostProcess `json:"processes" yaml:"processes"`
	Notes     []string      `json:"notes,omitempty" yaml:"notes,omitempty"`
}

// HostProcess is one process (or docker run fallback) on the machine.
type HostProcess struct {
	Name       string            `json:"name" yaml:"name"`
	Command    Cmd               `json:"command,omitempty" yaml:"command,omitempty"`
	Env        map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
	WorkingDir string            `json:"workingDir,omitempty" yaml:"workingDir,omitempty"`
	Ports      []Port            `json:"ports,omitempty" yaml:"ports,omitempty"`
	DependsOn  []string          `json:"dependsOn,omitempty" yaml:"dependsOn,omitempty"`
	Image      string            `json:"image,omitempty" yaml:"image,omitempty"`
	DockerArgs []string          `json:"dockerArgs,omitempty" yaml:"dockerArgs,omitempty"`
}

// ToHost turns a Definition into a host process plan. App-like services
// (command/entrypoint/build) become native processes; image-only
// dependencies become `docker run` argv so they still start on the machine
// without a compose project.
func ToHost(d *Definition) (*HostPlan, error) {
	if d == nil {
		return nil, fmt.Errorf("task definition is nil")
	}
	plan := &HostPlan{Name: d.Name, Notes: append([]string(nil), d.Notes...)}
	for _, svc := range d.Services {
		proc, notes := hostProcess(svc)
		plan.Processes = append(plan.Processes, proc)
		for _, n := range notes {
			plan.Notes = append(plan.Notes, n)
		}
	}
	if len(plan.Processes) == 0 {
		return nil, fmt.Errorf("task %q: no services to run on the host", d.Name)
	}
	return plan, nil
}

func hostProcess(svc Service) (HostProcess, []string) {
	proc := HostProcess{
		Name:      svc.Name,
		Env:       copyEnv(svc.Env),
		Ports:     append([]Port(nil), svc.Ports...),
		DependsOn: append([]string(nil), svc.DependsOn...),
	}
	if proc.Env == nil {
		proc.Env = map[string]string{}
	}
	for _, p := range svc.Ports {
		if p.Published == "" {
			continue
		}
		key := strings.ToUpper(strings.ReplaceAll(svc.Name, "-", "_")) + "_PORT"
		if _, ok := proc.Env[key]; !ok {
			proc.Env[key] = p.Published
		}
		if _, ok := proc.Env["PORT"]; !ok {
			proc.Env["PORT"] = p.Published
		}
	}
	if len(proc.Env) == 0 {
		proc.Env = nil
	}

	cmd := combineCmd(svc.Entrypoint, svc.Command)
	if svc.Runtime == RuntimeHost || svc.Build != nil {
		proc.Command = cmd
		proc.WorkingDir = hostWorkingDir(svc)
		var notes []string
		if cmd.Empty() {
			notes = append(notes, fmt.Sprintf("service %q has build but no command; set command to run it on the host", svc.Name))
		}
		return proc, notes
	}

	proc.Image = svc.Image
	proc.Command = cmd
	proc.DockerArgs = dockerRunArgs(svc)
	note := fmt.Sprintf("service %q is image-only; host plan uses docker run (not compose)", svc.Name)
	return proc, []string{note}
}

func combineCmd(entrypoint, command Cmd) Cmd {
	if entrypoint.Empty() {
		return command
	}
	if command.Empty() {
		return entrypoint
	}
	if entrypoint.Shell != "" || command.Shell != "" {
		return Cmd{Shell: strings.TrimSpace(cmdString(entrypoint) + " " + cmdString(command))}
	}
	out := make([]string, 0, len(entrypoint.Argv)+len(command.Argv))
	out = append(out, entrypoint.Argv...)
	out = append(out, command.Argv...)
	return Cmd{Argv: out}
}

func cmdString(c Cmd) string {
	if c.Shell != "" {
		return c.Shell
	}
	return strings.Join(c.Argv, " ")
}

func hostWorkingDir(svc Service) string {
	for _, m := range svc.Mounts {
		if m.Type != "bind" {
			continue
		}
		if svc.WorkingDir != "" && m.Target == svc.WorkingDir {
			return m.Source
		}
	}
	for _, m := range svc.Mounts {
		if m.Type == "bind" && (m.Source == "." || m.Source == "./") {
			return "."
		}
	}
	if svc.Build != nil && svc.Build.Context != "" {
		return svc.Build.Context
	}
	if svc.WorkingDir != "" && !strings.HasPrefix(svc.WorkingDir, "/") {
		return svc.WorkingDir
	}
	return "."
}

func dockerRunArgs(svc Service) []string {
	args := []string{"run", "--rm", "-d", "--name", svc.Name}
	keys := make([]string, 0, len(svc.Env))
	for k := range svc.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, "-e", k+"="+svc.Env[k])
	}
	for _, f := range svc.EnvFiles {
		args = append(args, "--env-file", f)
	}
	for _, p := range svc.Ports {
		spec := portSpec(p)
		if spec != "" {
			args = append(args, "-p", spec)
		}
	}
	for _, m := range svc.Mounts {
		spec := mountSpec(m)
		if spec != "" {
			args = append(args, "-v", spec)
		}
	}
	if svc.WorkingDir != "" {
		args = append(args, "-w", svc.WorkingDir)
	}
	if svc.User != "" {
		args = append(args, "-u", svc.User)
	}
	if svc.NetworkMode != "" {
		args = append(args, "--network", svc.NetworkMode)
	}
	if svc.Privileged {
		args = append(args, "--privileged")
	}
	if svc.Restart != "" && svc.Restart != "no" {
		args = append(args, "--restart", svc.Restart)
	}
	if svc.Image != "" {
		args = append(args, svc.Image)
	}
	if !svc.Entrypoint.Empty() {
		if svc.Entrypoint.Shell != "" {
			args = append(args, "--entrypoint", svc.Entrypoint.Shell)
		} else if len(svc.Entrypoint.Argv) > 0 {
			args = append(args, "--entrypoint", svc.Entrypoint.Argv[0])
		}
	}
	if !svc.Command.Empty() {
		if svc.Command.Shell != "" {
			args = append(args, "sh", "-c", svc.Command.Shell)
		} else {
			args = append(args, svc.Command.Argv...)
		}
	}
	return args
}

func copyEnv(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// FromHost builds a Definition from a host process plan so the same
// task can be rendered as compose / swarm / Portainer.
func FromHost(plan *HostPlan) (*Definition, error) {
	if plan == nil {
		return nil, fmt.Errorf("host plan is nil")
	}
	if strings.TrimSpace(plan.Name) == "" {
		return nil, fmt.Errorf("host plan name must not be empty")
	}
	if len(plan.Processes) == 0 {
		return nil, fmt.Errorf("host plan %q: no processes", plan.Name)
	}
	def := &Definition{
		Name:   plan.Name,
		Source: Source{Kind: SourceHost},
		Notes:  append([]string(nil), plan.Notes...),
	}
	seen := map[string]struct{}{}
	for _, p := range plan.Processes {
		name := strings.TrimSpace(p.Name)
		if name == "" {
			return nil, fmt.Errorf("host plan %q: process name must not be empty", plan.Name)
		}
		if _, ok := seen[name]; ok {
			return nil, fmt.Errorf("host plan %q: duplicate process %q", plan.Name, name)
		}
		seen[name] = struct{}{}
		svc := Service{
			Name:      name,
			Command:   p.Command,
			Env:       copyEnv(p.Env),
			Ports:     append([]Port(nil), p.Ports...),
			DependsOn: append([]string(nil), p.DependsOn...),
			Image:     strings.TrimSpace(p.Image),
		}
		if svc.Image != "" {
			svc.Runtime = RuntimeContainer
		} else {
			svc.Runtime = RuntimeHost
			wd := p.WorkingDir
			if wd == "" {
				wd = "."
			}
			svc.Build = &Build{Context: wd}
			svc.WorkingDir = "/app"
			svc.Mounts = []Mount{{
				Type:   "bind",
				Source: wd,
				Target: "/app",
			}}
		}
		if p.WorkingDir != "" && svc.Runtime == RuntimeContainer {
			svc.WorkingDir = p.WorkingDir
		}
		def.Services = append(def.Services, svc)
	}
	sort.Slice(def.Services, func(i, j int) bool { return def.Services[i].Name < def.Services[j].Name })
	return def, nil
}
