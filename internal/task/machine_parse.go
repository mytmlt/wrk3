package task

import (
	"fmt"
	"strings"
)

// ParseRunCommands reconstructs a Definition from a list of `docker run`
// (or `podman run`) commands, e.g. a script previously rendered by the
// machine target or produced by another tool. This is the reverse of
// Definition.Machine: machine -> Definition -> compose.
//
// Lines that are not run commands are ignored, but `docker network create
// <name>` lines are honored so parsed networks keep their internal vs
// external distinction. Commands without --name get a service name
// derived from the image.
func ParseRunCommands(raw []byte) (*Definition, error) {
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	text = strings.ReplaceAll(text, "\\\n", " ")
	lines := strings.Split(text, "\n")

	created := map[string]struct{}{}
	for _, line := range lines {
		if fields, err := shellFields(line); err == nil {
			if name, ok := networkCreateName(fields); ok {
				created[name] = struct{}{}
			}
		}
	}

	def := &Definition{
		Services: map[string]*Service{},
		Volumes:  map[string]*Volume{},
		Networks: map[string]*Network{},
	}
	taken := map[string]struct{}{}
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields, err := shellFields(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		args, ok := runArgs(fields)
		if !ok {
			continue
		}
		svc, explicitName, err := parseRunArgs(args, created, def)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		def.Services[svcName(explicitName, svc.Image, taken)] = svc
	}
	if len(def.Services) == 0 {
		return nil, fmt.Errorf("no `docker run` commands found")
	}
	def.Normalize()
	return def, nil
}

// networkCreateName matches `docker|podman network create [-d ...] <name>`.
func networkCreateName(fields []string) (string, bool) {
	engine := engineIndex(fields)
	if engine < 0 || engine+2 >= len(fields) || fields[engine+1] != "network" || fields[engine+2] != "create" {
		return "", false
	}
	// The network name is the trailing positional argument; flag values
	// (e.g. `-d bridge`) also look positional, so scan from the end.
	for i := len(fields) - 1; i >= engine+3; i-- {
		if f := fields[i]; !strings.HasPrefix(f, "-") {
			return f, true
		}
	}
	return "", false
}

// engineIndex returns the index of the docker/podman executable in fields.
func engineIndex(fields []string) int {
	for i, f := range fields {
		if f == "docker" || f == "podman" {
			return i
		}
	}
	return -1
}

// runArgs returns the arguments after `run` for a docker/podman run line.
func runArgs(fields []string) ([]string, bool) {
	i := engineIndex(fields)
	if i < 0 {
		return nil, false
	}
	i++
	if i < len(fields) && fields[i] == "container" {
		i++
	}
	if i >= len(fields) || fields[i] != "run" {
		return nil, false
	}
	return fields[i+1:], true
}

// runFlags maps long flag names to whether they consume a value. Short
// flags are handled by valueShortFlags/booleanShortFlags.
var runValueFlags = map[string]bool{
	"--name": true, "--env": true, "--publish": true, "--volume": true,
	"--mount": true, "--restart": true, "--network": true, "--net": true,
	"--label": true, "--entrypoint": true, "--workdir": true, "--user": true,
	"--hostname": true, "--env-file": true, "--expose": true, "--add-host": true,
	"--health-cmd": true, "--health-interval": true, "--health-timeout": true,
	"--health-retries": true, "--health-start-period": true, "--memory": true,
	"--cpus": true, "--platform": true, "--pull": true, "--log-driver": true,
	"--log-opt": true, "--ulimit": true, "--security-opt": true, "--sysctl": true,
	"--tmpfs": true, "--ip": true, "--mac-address": true, "--stop-signal": true,
	"--stop-timeout": true, "--shm-size": true, "--gpus": true, "--device": true,
	"--dns": true, "--cap-add": true, "--cap-drop": true, "--link": true,
	"--volumes-from": true, "--pid": true, "--ipc": true, "--uts": true,
	"--userns": true, "--cgroupns": true, "--runtime": true, "--storage-opt": true,
	"--group-add": true,
}

var runBooleanFlags = map[string]bool{
	"--detach": true, "--rm": true, "--privileged": true, "--publish-all": true,
	"--init": true, "--read-only": true, "--tty": true, "--interactive": true,
	"--no-healthcheck": true, "--oom-kill-disable": true,
}

var shortValueFlags = map[byte]bool{
	'e': true, 'p': true, 'v': true, 'l': true, 'w': true, 'u': true,
	'h': true, 'm': true,
}

// parseRunArgs parses the argv after `run` into a Service plus the
// explicit --name when one was given.
func parseRunArgs(args []string, created map[string]struct{}, def *Definition) (*Service, string, error) {
	svc := &Service{Environment: map[string]string{}, Labels: map[string]string{}}
	explicitName := ""
	i := 0
	for i < len(args) {
		tok := args[i]
		if tok == "" {
			i++
			continue
		}
		if !strings.HasPrefix(tok, "-") || tok == "-" {
			break
		}
		flag, val, hasVal, consumed, err := nextFlag(args, i)
		if err != nil {
			return nil, "", err
		}
		if hasVal {
			if flag == "--name" {
				explicitName = val
			} else if err := applyRunFlag(svc, flag, val, created, def); err != nil {
				return nil, "", err
			}
		}
		i += consumed
	}
	if i >= len(args) {
		return nil, "", fmt.Errorf("run command has no image")
	}
	svc.Image = args[i]
	svc.Command = append([]string(nil), args[i+1:]...)
	return svc, explicitName, nil
}

// nextFlag extracts the flag and its value at args[i]. It returns the
// number of argv elements consumed.
func nextFlag(args []string, i int) (flag, val string, hasVal bool, consumed int, err error) {
	tok := args[i]
	if strings.HasPrefix(tok, "--") {
		if name, value, found := strings.Cut(tok, "="); found {
			return name, value, true, 1, nil
		}
		if runBooleanFlags[tok] {
			return tok, "", false, 1, nil
		}
		if runValueFlags[tok] {
			if i+1 >= len(args) {
				return "", "", false, 0, fmt.Errorf("flag %q needs a value", tok)
			}
			return tok, args[i+1], true, 2, nil
		}
		// Unknown long flag: assume boolean without a value.
		return tok, "", false, 1, nil
	}
	// Short flag cluster: -d, -it, -p8080:80, -eFOO=bar.
	body := tok[1:]
	for j := 0; j < len(body); j++ {
		short := body[j]
		if shortValueFlags[short] {
			rest := body[j+1:]
			if rest != "" {
				return "-" + string(short), rest, true, 1, nil
			}
			if i+1 >= len(args) {
				return "", "", false, 0, fmt.Errorf("flag -%c needs a value", short)
			}
			return "-" + string(short), args[i+1], true, 2, nil
		}
		// Boolean or unknown short flag: keep scanning the cluster.
	}
	return tok, "", false, 1, nil
}

// applyRunFlag folds one run flag into svc (and def for resources).
func applyRunFlag(svc *Service, flag, val string, created map[string]struct{}, def *Definition) error {
	switch flag {
	case "-e", "--env":
		if k, v, found := strings.Cut(val, "="); found {
			svc.Environment[strings.TrimSpace(k)] = v
		} else if k := strings.TrimSpace(val); k != "" {
			svc.Environment[k] = "${" + k + "}"
		}
	case "-p", "--publish":
		p, err := parsePortString(val)
		if err != nil {
			return err
		}
		svc.Ports = append(svc.Ports, p)
	case "-v", "--volume":
		m, err := parseMountString(val)
		if err != nil {
			return err
		}
		addRunMount(svc, m, def)
	case "--mount":
		addRunMount(svc, parseMountOption(val), def)
	case "--restart":
		svc.Restart = val
	case "--network", "--net":
		svc.Networks = append(svc.Networks, val)
		if _, ok := def.Networks[val]; !ok {
			_, internal := created[val]
			def.Networks[val] = &Network{External: !internal}
		}
	case "-l", "--label":
		if k, v, found := strings.Cut(val, "="); found {
			svc.Labels[strings.TrimSpace(k)] = v
		}
	case "--entrypoint":
		svc.Entrypoint = []string{val}
	}
	return nil
}

// addRunMount records a mount and synthesizes its named volume so the
// parsed Definition validates and re-renders to compose.
func addRunMount(svc *Service, m Mount, def *Definition) {
	svc.Volumes = append(svc.Volumes, m)
	if m.Type != MountVolume || m.Source == "" {
		return
	}
	if _, ok := def.Volumes[m.Source]; !ok {
		def.Volumes[m.Source] = &Volume{}
	}
}

// svcName picks the service name: the explicit --name when present,
// otherwise a sanitized image name made unique against taken.
func svcName(explicit, image string, taken map[string]struct{}) string {
	name := strings.TrimSpace(explicit)
	if name == "" {
		name = imageBase(image)
	}
	name = sanitizeName(name)
	if _, ok := taken[name]; !ok {
		taken[name] = struct{}{}
		return name
	}
	for n := 2; ; n++ {
		cand := fmt.Sprintf("%s-%d", name, n)
		if _, ok := taken[cand]; !ok {
			taken[cand] = struct{}{}
			return cand
		}
	}
}

// imageBase returns the last path segment of an image reference with any
// tag/digest stripped.
func imageBase(image string) string {
	s := image
	if i := strings.IndexAny(s, "@"); i >= 0 {
		s = s[:i]
	}
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	if i := strings.LastIndex(s, ":"); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		return "service"
	}
	return s
}

// sanitizeName lowercases and replaces characters outside [a-z0-9_.-].
func sanitizeName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		ok := r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '.' || r == '-'
		if ok {
			b.WriteRune(r)
			prevDash = r == '-'
			continue
		}
		if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	out := strings.Trim(b.String(), "-_.")
	if out == "" {
		return "service"
	}
	return out
}

// parseMountOption parses the docker --mount key=value form.
func parseMountOption(s string) Mount {
	m := Mount{}
	for _, part := range strings.Split(s, ",") {
		k, v, found := strings.Cut(part, "=")
		k = strings.TrimSpace(k)
		if !found {
			if k == "readonly" || k == "ro" {
				m.ReadOnly = true
			}
			continue
		}
		switch k {
		case "type":
			m.Type = strings.TrimSpace(v)
		case "source", "src":
			m.Source = strings.TrimSpace(v)
		case "target", "destination", "dst":
			m.Target = strings.TrimSpace(v)
		case "readonly", "ro":
			m.ReadOnly = strings.EqualFold(strings.TrimSpace(v), "true") || strings.TrimSpace(v) == "1"
		}
	}
	if m.Type == "" {
		m.Type = inferMountType(m.Source)
	}
	return m
}

// shellFields splits a command line into fields honoring single quotes,
// double quotes and backslash escapes.
func shellFields(s string) ([]string, error) {
	var fields []string
	var cur strings.Builder
	inField := false
	i := 0
	for i < len(s) {
		c := s[i]
		switch c {
		case ' ', '\t':
			if inField {
				fields = append(fields, cur.String())
				cur.Reset()
				inField = false
			}
			i++
		case '\'':
			inField = true
			i++
			end := strings.IndexByte(s[i:], '\'')
			if end < 0 {
				return nil, fmt.Errorf("unterminated single quote")
			}
			cur.WriteString(s[i : i+end])
			i += end + 1
		case '"':
			inField = true
			i++
			for i < len(s) && s[i] != '"' {
				if s[i] == '\\' && i+1 < len(s) {
					i++
				}
				cur.WriteByte(s[i])
				i++
			}
			if i >= len(s) {
				return nil, fmt.Errorf("unterminated double quote")
			}
			i++
		case '\\':
			inField = true
			if i+1 < len(s) {
				cur.WriteByte(s[i+1])
				i += 2
			} else {
				i++
			}
		default:
			inField = true
			cur.WriteByte(c)
			i++
		}
	}
	if inField {
		fields = append(fields, cur.String())
	}
	return fields, nil
}
