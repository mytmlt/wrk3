// Package proxy routes local-only <slug>.<domain> URLs to per-worktree
// app ports with a stdlib reverse proxy (no Caddy dependency).
//
// The gateway resolves the Host header per request against the current
// <worktreeBase>/.wrk3-state.json records, so no route sync step is
// needed: up/down/add/remove take effect on the next request.
package proxy

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Defaults for wrk3.yaml proxy.addr/proxy.domain. Port 8080 avoids the
// sudo needed to bind :80; domain localhost auto-resolves in
// Chrome/Firefox/Edge (Safari/curl need hosts-sync).
const (
	DefaultDomain = "localhost"
	DefaultAddr   = "127.0.0.1:8080"
	// PrivilegedPortCeiling is the first unprivileged TCP port. Binding
	// below it needs root or CAP_NET_BIND_SERVICE on typical Unix systems.
	PrivilegedPortCeiling = 1024
)

// PidFileName tracks the gateway daemon under worktreeBase.
const PidFileName = ".wrk3-proxy.pid"

// HostsMarker brackets lines managed by `wrk3 proxy hosts-sync`.
const HostsMarker = "# Managed by wrk3 proxy"

// NormalizeDomain lowercases and trims a single trailing dot.
func NormalizeDomain(d string) string {
	d = strings.ToLower(strings.TrimSpace(d))
	return strings.TrimSuffix(d, ".")
}

// Validate checks domain/addr when the proxy is enabled.
func Validate(domain, addr string) error {
	d := NormalizeDomain(domain)
	if d == "" {
		return fmt.Errorf("proxy.domain must not be empty")
	}
	for _, part := range strings.Split(d, ".") {
		if part == "" {
			return fmt.Errorf("proxy.domain %q has an empty label", domain)
		}
		if len(part) > 63 {
			return fmt.Errorf("proxy.domain %q label %q too long", domain, part)
		}
		for _, r := range part {
			ok := r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-'
			if !ok {
				return fmt.Errorf("proxy.domain %q must be hostname characters", domain)
			}
		}
		if strings.HasPrefix(part, "-") || strings.HasSuffix(part, "-") {
			return fmt.Errorf("proxy.domain %q label %q must not start/end with -", domain, part)
		}
	}
	host, port, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return fmt.Errorf("proxy.addr %q must be host:port: %w", addr, err)
	}
	if strings.TrimSpace(host) == "" {
		return fmt.Errorf("proxy.addr %q must include a host", addr)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n <= 0 || n > 65535 {
		return fmt.Errorf("proxy.addr %q has invalid port", addr)
	}
	return nil
}

// SlugFromHost extracts the worktree slug from a Host/Host:port header
// for domain. It returns ok=false for apex/bare domains, deeper
// subdomains, or other domains.
func SlugFromHost(host, domain string) (slug string, ok bool) {
	h := strings.ToLower(strings.TrimSpace(host))
	if i := strings.LastIndexByte(h, ':'); i >= 0 {
		if hh, _, err := net.SplitHostPort(h); err == nil {
			h = hh
		} else {
			h = strings.TrimSuffix(h[:i], ".")
		}
	}
	h = strings.TrimSuffix(h, ".")
	d := NormalizeDomain(domain)
	if h == d || !strings.HasSuffix(h, "."+d) {
		return "", false
	}
	slug = strings.TrimSuffix(h, "."+d)
	if slug == "" || strings.Contains(slug, ".") {
		return "", false
	}
	return slug, true
}

// GatewayPort returns the port of a host:port addr, or 0 when invalid.
func GatewayPort(addr string) int {
	_, port, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(port)
	if err != nil || n <= 0 || n > 65535 {
		return 0
	}
	return n
}

// Listen binds tcp addr. Permission denied on ports below
// PrivilegedPortCeiling includes a hint to use DefaultAddr.
func Listen(addr string) (net.Listener, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, WrapListenError(addr, err)
	}
	return ln, nil
}

// CheckListen probes whether addr can be bound, then releases it.
func CheckListen(addr string) error {
	ln, err := Listen(addr)
	if err != nil {
		return err
	}
	return ln.Close()
}

// WrapListenError annotates a net.Listen failure for proxy.addr.
func WrapListenError(addr string, err error) error {
	if err == nil {
		return nil
	}
	port := GatewayPort(addr)
	if isPermissionDenied(err) && port > 0 && port < PrivilegedPortCeiling {
		return fmt.Errorf("proxy listen %s: %w (ports below %d need root or CAP_NET_BIND_SERVICE; set proxy.addr to %s)",
			addr, err, PrivilegedPortCeiling, DefaultAddr)
	}
	return fmt.Errorf("proxy listen %s: %w", addr, err)
}

// IsUnprivilegedListen reports a listen bind that failed because the
// process cannot use a privileged port. Expected user/environment
// condition, not a crash.
func IsUnprivilegedListen(err error) bool {
	if err == nil || !isPermissionDenied(err) {
		return false
	}
	msg := err.Error()
	if !strings.Contains(msg, "listen") {
		return false
	}
	return strings.Contains(msg, "bind") || strings.Contains(msg, "proxy listen")
}

func isPermissionDenied(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrPermission) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "permission denied") || strings.Contains(msg, "access is denied")
}

// URLForSlug builds the public URL for a worktree. Port 80 is omitted.
func URLForSlug(slug, domain, addr string) string {
	d := NormalizeDomain(domain)
	if d == "" {
		d = DefaultDomain
	}
	p := GatewayPort(addr)
	if p == 80 {
		return "http://" + slug + "." + d
	}
	if p <= 0 {
		return "http://" + slug + "." + d
	}
	return "http://" + slug + "." + d + ":" + strconv.Itoa(p)
}

// PidPath returns <worktreeBase>/.wrk3-proxy.pid (absolute).
func PidPath(worktreeBase string) string {
	return filepath.Join(worktreeBase, PidFileName)
}

// NewHandler returns a reverse proxy routing <slug>.<domain> to
// 127.0.0.1:<appPort>. load is called per request so state edits apply
// immediately. Unknown slugs get a 404 hinting at proxy status.
func NewHandler(domain string, load func() map[string]int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slug, ok := SlugFromHost(r.Host, domain)
		if !ok {
			http.Error(w, "unknown worktree (see wrk3 proxy status)", http.StatusNotFound)
			return
		}
		targets := load()
		port, ok := targets[slug]
		if !ok || port <= 0 || port > 65535 {
			http.Error(w, fmt.Sprintf("unknown worktree %q (see wrk3 proxy status)", slug), http.StatusNotFound)
			return
		}
		target := &url.URL{Scheme: "http", Host: "127.0.0.1:" + strconv.Itoa(port)}
		rp := httputil.NewSingleHostReverseProxy(target)
		// Preserve the original Host for apps doing Host checks while
		// still dialing the target (SingleHostReverseProxy sets
		// r.URL.Host; keep r.Host as the public name).
		rp.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
			http.Error(w, fmt.Sprintf("worktree %q unreachable on 127.0.0.1:%d: %v", slug, port, err), http.StatusBadGateway)
		}
		rp.ServeHTTP(w, r)
	})
}

// MissingHosts reports hostnames absent from hosts file content.
func MissingHosts(content string, hostnames []string) []string {
	present := map[string]struct{}{}
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if i := strings.IndexByte(trimmed, '#'); i >= 0 {
			trimmed = strings.TrimSpace(trimmed[:i])
		}
		fields := strings.Fields(trimmed)
		if len(fields) < 2 {
			continue
		}
		for _, f := range fields[1:] {
			present[strings.ToLower(f)] = struct{}{}
		}
	}
	var missing []string
	for _, h := range hostnames {
		if _, ok := present[strings.ToLower(h)]; !ok {
			missing = append(missing, h)
		}
	}
	return missing
}

// SyncHostsContent adds missing 127.0.0.1 hostnames under the wrk3 marker.
// It is idempotent and preserves all other lines verbatim.
func SyncHostsContent(content string, hostnames []string) string {
	missing := MissingHosts(content, hostnames)
	if len(missing) == 0 {
		return content
	}
	body := strings.ReplaceAll(content, "\r\n", "\n")
	hasMarker := false
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == HostsMarker {
			hasMarker = true
			break
		}
	}
	var b strings.Builder
	b.WriteString(body)
	if body != "" && !strings.HasSuffix(body, "\n") {
		b.WriteString("\n")
	}
	if !hasMarker {
		b.WriteString(HostsMarker + "\n")
	}
	for _, h := range missing {
		b.WriteString("127.0.0.1 " + h + "\n")
	}
	return b.String()
}

// SyncHosts appends missing hostnames to the hosts file at path.
func SyncHosts(path string, hostnames []string) (added []string, err error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read hosts %q: %w", path, err)
	}
	missing := MissingHosts(string(raw), hostnames)
	if len(missing) == 0 {
		return nil, nil
	}
	updated := SyncHostsContent(string(raw), hostnames)
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		return nil, fmt.Errorf("write hosts %q: %w", path, err)
	}
	return missing, nil
}
