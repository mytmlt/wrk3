// Package proxy zrok helpers manage one `zrok2 share public` process per
// worktree (stdlib only, via os/exec — no zrok SDK).
//
// Naming: reserved name = <prefix><slug> in the namespace, served as
// https://<name>.<domain> by the namespace frontend (e.g.
// public:myt-home-demo -> https://myt-home-demo.tunnel.mytmlt.site).
// Install + enable stay outside wrk3; every helper errors with a hint when
// the CLI is missing or the environment is not enabled.
package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Zrok defaults: namespace token, CLI binary, backend mode.
const (
	ZrokDefaultNamespace   = "public"
	ZrokDefaultBinary      = "zrok2"
	ZrokDefaultBackendMode = "proxy"
)

// ZrokBin resolves the CLI binary: $WRK3_ZROK_BIN wins (tests), then the
// configured value, then "zrok2".
func ZrokBin(configured string) string {
	if v := strings.TrimSpace(os.Getenv("WRK3_ZROK_BIN")); v != "" {
		return v
	}
	if strings.TrimSpace(configured) != "" {
		return strings.TrimSpace(configured)
	}
	return ZrokDefaultBinary
}

// ZrokName builds the reserved name (<prefix><slug>, lowercased).
func ZrokName(prefix, slug string) string {
	return strings.ToLower(strings.TrimSpace(prefix + strings.TrimSpace(slug)))
}

// ZrokURL builds https://<name>.<domain> (TLS terminates at the frontend).
func ZrokURL(name, domain string) string {
	d := NormalizeDomain(domain)
	if d == "" {
		d = DefaultDomain
	}
	n := strings.ToLower(strings.Trim(strings.TrimSpace(name), "."))
	return "https://" + n + "." + d
}

// ZrokTarget builds the share backend target for an app port.
func ZrokTarget(appPort int) string {
	return "http://127.0.0.1:" + strconv.Itoa(appPort)
}

// ZrokNameSelection builds the -n flag value (<namespace>:<name>).
func ZrokNameSelection(namespace, name string) string {
	return strings.TrimSpace(namespace) + ":" + strings.TrimSpace(name)
}

// ValidateZrokName checks a reserved name is a single DNS label.
func ValidateZrokName(name string) error {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return fmt.Errorf("zrok name must not be empty")
	}
	if len(n) > 63 {
		return fmt.Errorf("zrok name %q too long (max 63 chars)", name)
	}
	for _, r := range n {
		ok := r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-'
		if !ok {
			return fmt.Errorf("zrok name %q must be hostname characters", name)
		}
	}
	if strings.HasPrefix(n, "-") || strings.HasSuffix(n, "-") {
		return fmt.Errorf("zrok name %q must not start/end with -", name)
	}
	return nil
}

// ZrokPidPath returns <worktreeBase>/.wrk3-zrok-<slug>.pid.
func ZrokPidPath(worktreeBase, slug string) string {
	return filepath.Join(worktreeBase, ".wrk3-zrok-"+strings.ToLower(strings.TrimSpace(slug))+".pid")
}

// ZrokLogPath returns <worktreeBase>/.wrk3-zrok-<slug>.log.
func ZrokLogPath(worktreeBase, slug string) string {
	return filepath.Join(worktreeBase, ".wrk3-zrok-"+strings.ToLower(strings.TrimSpace(slug))+".log")
}

// ZrokCheckInstalled errors when the CLI is missing or the environment is
// not enabled. Install/enable always stay outside wrk3.
func ZrokCheckInstalled(ctx context.Context, binary string) error {
	if _, err := exec.LookPath(binary); err != nil {
		return fmt.Errorf("zrok CLI %q not found (install zrok2 and run `zrok2 enable` outside wrk3): %w", binary, err)
	}
	cmd := exec.CommandContext(ctx, binary, "status")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("zrok CLI %q not enabled (run `zrok2 enable` outside wrk3): %w: %s", binary, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// zrokNameEntry mirrors `zrok2 list names --json` rows (subset).
type zrokNameEntry struct {
	Name           string `json:"name"`
	NamespaceToken string `json:"namespaceToken"`
}

// ZrokNameExists reports whether <name> is reserved in <namespace>.
func ZrokNameExists(ctx context.Context, binary, namespace, name string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "list", "names", "--json")
	if strings.TrimSpace(namespace) != "" {
		cmd.Args = append(cmd.Args, "-n", strings.TrimSpace(namespace))
	}
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return false, fmt.Errorf("zrok list names: %w: %s", err, strings.TrimSpace(string(ee.Stderr)))
		}
		return false, fmt.Errorf("zrok list names: %w", err)
	}
	var entries []zrokNameEntry
	if err := json.Unmarshal(out, &entries); err != nil {
		return false, fmt.Errorf("zrok list names: parse output: %w", err)
	}
	want := strings.ToLower(strings.TrimSpace(name))
	for _, e := range entries {
		if strings.ToLower(strings.TrimSpace(e.Name)) == want {
			return true, nil
		}
	}
	return false, nil
}

// ZrokCreateName reserves <name> in <namespace> (idempotent: exists -> nil).
func ZrokCreateName(ctx context.Context, binary, namespace, name string) error {
	if err := ValidateZrokName(name); err != nil {
		return err
	}
	exists, err := ZrokNameExists(ctx, binary, namespace, name)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "create", "name", name, "-n", strings.TrimSpace(namespace))
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("zrok create name %q: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ZrokDeleteName releases <name> (refuses names outside prefix guard — the
// caller passes only prefix-derived names; empty prefix deletes nothing).
func ZrokDeleteName(ctx context.Context, binary, namespace, prefix, name string) error {
	if strings.TrimSpace(prefix) != "" && !strings.HasPrefix(strings.ToLower(name), strings.ToLower(prefix)) {
		return fmt.Errorf("refusing to delete zrok name %q outside prefix %q", name, prefix)
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "delete", "name", name, "-n", strings.TrimSpace(namespace))
	if out, err := cmd.CombinedOutput(); err != nil {
		s := strings.TrimSpace(string(out))
		if strings.Contains(strings.ToLower(s), "not found") {
			return nil
		}
		return fmt.Errorf("zrok delete name %q: %w: %s", name, err, s)
	}
	return nil
}

// ZrokShareArgs builds `zrok2 share public` args for a target + selection.
func ZrokShareArgs(target, namespace, name, backendMode string) []string {
	mode := strings.TrimSpace(backendMode)
	if mode == "" {
		mode = ZrokDefaultBackendMode
	}
	return []string{"share", "public", target,
		"-n", ZrokNameSelection(namespace, name),
		"--backend-mode", mode, "--headless", "--force-local"}
}

// ZrokShareRunning reports whether the per-slug share process is alive
// (pid file + signal 0). Missing pid file means stopped.
func ZrokShareRunning(worktreeBase, slug string) bool {
	raw, err := os.ReadFile(ZrokPidPath(worktreeBase, slug))
	if err != nil {
		return false
	}
	var p struct {
		PID int `json:"pid"`
	}
	if err := json.Unmarshal(raw, &p); err != nil || p.PID <= 0 {
		return false
	}
	proc, err := os.FindProcess(p.PID)
	if err != nil {
		return false
	}
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		_ = os.Remove(ZrokPidPath(worktreeBase, slug))
		return false
	}
	return true
}

// ZrokWritePid writes the share pid file.
func ZrokWritePid(worktreeBase, slug string, pid int, name, target string) error {
	raw, _ := json.Marshal(map[string]any{"pid": pid, "name": name, "target": target})
	if err := os.MkdirAll(worktreeBase, 0o755); err != nil {
		return fmt.Errorf("create worktree base: %w", err)
	}
	if err := os.WriteFile(ZrokPidPath(worktreeBase, slug), append(raw, '\n'), 0o644); err != nil {
		return fmt.Errorf("write zrok pid: %w", err)
	}
	return nil
}

// ZrokRemovePid drops the pid file best-effort.
func ZrokRemovePid(worktreeBase, slug string) {
	_ = os.Remove(ZrokPidPath(worktreeBase, slug))
}
