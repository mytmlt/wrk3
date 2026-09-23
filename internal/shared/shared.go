// Package shared implements wrk3 shared services: long-lived compose
// services (databases, brokers) that run once per repo in a fixed
// compose project while each worktree runs only its own app services.
//
// The package is intentionally backend-agnostic: it expands
// per-worktree template variables, renders compose overlay files
// (published ports for shared services, environment overrides for
// worktree services), and waits for published host ports to accept
// TCP. It never shells out and never hardcodes stack-specific
// commands — isolation hooks stay user-authored `shared.setup`
// entry strings run via the Runner like any other entry command.
package shared

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Template variables expanded in shared env values and setup commands.
// Only the braced form expands, so values containing bare `$` (e.g.
// passwords, connection strings) pass through untouched.
const (
	// VarSlug is the raw worktree slug (e.g. "feature-foo").
	VarSlug = "slug"
	// VarSlugUnderscore is the slug with every non-alphanumeric run
	// mapped to a single underscore (e.g. "feature_foo"): safe for
	// database names and most identifiers.
	VarSlugUnderscore = "slug_underscore"
	// VarSlugDash is the slug with every non-alphanumeric run mapped
	// to a single dash (e.g. "feature-foo"): safe for vhost-style names.
	VarSlugDash = "slug_dash"
	// VarSharedProject is the sanitized shared compose project name.
	VarSharedProject = "shared_project"
)

// SlugUnderscore maps slug to its identifier-safe underscore form:
// lowercase alphanumerics kept, every other run becomes one "_",
// leading/trailing underscores trimmed.
func SlugUnderscore(slug string) string {
	return mapSlug(slug, '_')
}

// SlugDash maps slug to its dash form: lowercase alphanumerics kept,
// every other run becomes one "-", leading/trailing dashes trimmed.
func SlugDash(slug string) string {
	return mapSlug(slug, '-')
}

func mapSlug(slug string, sep rune) string {
	lower := strings.ToLower(slug)
	var b strings.Builder
	b.Grow(len(lower))
	prevSep := false
	for _, r := range lower {
		ok := r >= 'a' && r <= 'z' || r >= '0' && r <= '9'
		if ok {
			b.WriteRune(r)
			prevSep = false
			continue
		}
		if !prevSep {
			b.WriteRune(sep)
			prevSep = true
		}
	}
	return strings.Trim(b.String(), string(sep))
}

// Context carries the per-worktree values available to Expand.
type Context struct {
	Slug          string
	SharedProject string
}

// Expand replaces ${slug}, ${slug_underscore}, ${slug_dash} and
// ${shared_project} in s. Anything else — bare $vars, $$ escapes,
// lone dollars, unknown ${names} — passes through verbatim, so
// passwords, compose references, and shell snippets survive.
func Expand(s string, ctx Context) string {
	repl := map[string]string{
		VarSlug:           ctx.Slug,
		VarSlugUnderscore: SlugUnderscore(ctx.Slug),
		VarSlugDash:       SlugDash(ctx.Slug),
		VarSharedProject:  ctx.SharedProject,
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] != '$' || i+1 >= len(s) || s[i+1] != '{' {
			b.WriteByte(s[i])
			i++
			continue
		}
		end := strings.IndexByte(s[i+2:], '}')
		if end < 0 {
			b.WriteString(s[i:])
			break
		}
		name := s[i+2 : i+2+end]
		if v, ok := repl[name]; ok {
			b.WriteString(v)
		} else {
			b.WriteString(s[i : i+2+end+1])
		}
		i += 2 + end + 1
	}
	return b.String()
}

// ExpandMap expands every value in m with ctx, returning a copy.
// Keys pass through untouched.
func ExpandMap(m map[string]string, ctx Context) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = Expand(v, ctx)
	}
	return out
}

// ServicePorts describes one shared service: host:container publishes
// the shared overlay adds so worktree containers can reach it over
// the host gateway without editing the base compose file.
type ServicePorts struct {
	Ports []string
}

// SharedOverlay renders the shared-project overlay: published ports
// per shared service. Services without ports are omitted; when no
// service publishes ports it returns nil (no overlay file needed).
func SharedOverlay(services map[string]ServicePorts) ([]byte, error) {
	svcs := map[string]any{}
	for _, name := range sortedServiceNames(services) {
		ports := services[name].Ports
		if len(ports) == 0 {
			continue
		}
		svcs[name] = map[string]any{"ports": append([]string(nil), ports...)}
	}
	if len(svcs) == 0 {
		return nil, nil
	}
	raw, err := yaml.Marshal(map[string]any{"services": svcs})
	if err != nil {
		return nil, fmt.Errorf("marshal shared overlay: %w", err)
	}
	return raw, nil
}

// WorktreeOverlay renders the per-worktree overlay: environment
// overrides applied to each worktree service. A nil env still
// renders the service stubs (so callers can anchor other overrides
// later); an empty service list returns nil.
func WorktreeOverlay(worktreeServices []string, env map[string]string) ([]byte, error) {
	if len(worktreeServices) == 0 {
		return nil, nil
	}
	sorted := append([]string(nil), worktreeServices...)
	sort.Strings(sorted)
	svcs := map[string]any{}
	for _, name := range sorted {
		svc := map[string]any{}
		if len(env) > 0 {
			svc["environment"] = copyStrings(env)
		}
		svcs[name] = svc
	}
	raw, err := yaml.Marshal(map[string]any{"services": svcs})
	if err != nil {
		return nil, fmt.Errorf("marshal worktree overlay: %w", err)
	}
	return raw, nil
}

func sortedServiceNames[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func copyStrings(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// OverlayDir returns the directory holding generated overlay files:
// <worktreeBase>/.wrk3-overlays (absolute, cwd-independent, gitignored
// alongside the state file).
func OverlayDir(worktreeBase string) string {
	return filepath.Join(worktreeBase, ".wrk3-overlays")
}

// SharedOverlayPath returns the shared-project overlay path for project.
func SharedOverlayPath(worktreeBase, project string) string {
	return filepath.Join(OverlayDir(worktreeBase), "shared-"+project+".yml")
}

// WorktreeOverlayPath returns the per-slug worktree overlay path.
func WorktreeOverlayPath(worktreeBase, slug string) string {
	return filepath.Join(OverlayDir(worktreeBase), "worktree-"+slug+".yml")
}

// WriteFile writes raw to path (creating parents) only when the
// content differs, so parallel ups do not churn mtimes. A nil raw
// removes a previously written file and is otherwise a no-op.
func WriteFile(path string, raw []byte) error {
	if len(raw) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove overlay %q: %w", path, err)
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create overlay dir for %q: %w", path, err)
	}
	if cur, err := os.ReadFile(path); err == nil && string(cur) == string(raw) {
		return nil
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read overlay %q: %w", path, err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return fmt.Errorf("write overlay %q: %w", path, err)
	}
	return nil
}

// HostPorts extracts explicit numeric host ports from short-syntax
// publish strings ("host:container", "ip:host:container", or
// "[ip:]host:container[/proto]"). Container-only ("8080") and
// unparsable entries are skipped: ephemeral publishes have no stable
// port to wait on.
func HostPorts(publish []string) []int {
	var out []int
	for _, p := range publish {
		base := p
		if i := strings.LastIndex(base, "/"); i >= 0 {
			base = base[:i]
		}
		base = strings.TrimSpace(base)
		if base == "" {
			continue
		}
		parts := strings.Split(base, ":")
		var host string
		switch len(parts) {
		case 1:
			continue
		case 2:
			host = parts[0]
		case 3:
			host = parts[1]
		default:
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(host))
		if err != nil || n <= 0 || n > 65535 {
			continue
		}
		out = append(out, n)
	}
	return out
}

// readyTimeout bounds the post-up readiness wait: long enough for a
// cold postgres/rabbitmq to accept connections, short enough to fail
// fast on real misconfiguration.
const readyTimeout = 2 * time.Minute

// readyPollInterval spaces TCP dial attempts during the wait.
const readyPollInterval = 500 * time.Millisecond

// WaitReady dials each host port on 127.0.0.1 until all accept (or
// the timeout elapses). It is the backend-agnostic readiness gate
// for shared services: no `--wait` flag exists across compose
// implementations, but every shared TCP service publishes a host
// port. An empty list returns immediately.
func WaitReady(hostPorts []int) error {
	return WaitReadyWithTimeout(hostPorts, readyTimeout)
}

// WaitReadyWithTimeout is WaitReady with an explicit timeout,
// honoring sub-second test timeouts.
func WaitReadyWithTimeout(hostPorts []int, timeout time.Duration) error {
	if len(hostPorts) == 0 {
		return nil
	}
	deadline := time.Now().Add(timeout)
	var last int
	for _, port := range hostPorts {
		for {
			conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 2*time.Second)
			if err == nil {
				_ = conn.Close()
				break
			}
			last = port
			if time.Now().After(deadline) {
				return fmt.Errorf("shared service port 127.0.0.1:%d not accepting connections after %s: %w", last, timeout, err)
			}
			time.Sleep(readyPollInterval)
		}
	}
	return nil
}
