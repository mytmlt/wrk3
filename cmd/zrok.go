package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/spf13/cobra"

	"github.com/mytmlt/wrk3/internal/config"
	"github.com/mytmlt/wrk3/internal/ports"
	"github.com/mytmlt/wrk3/internal/proxy"
	"github.com/mytmlt/wrk3/internal/syslog"
)

// zrokNameFor returns the reserved name (<prefix><slug>) for a worktree.
func zrokNameFor(cfg *config.Config, slug string) string {
	return proxy.ZrokName(cfg.ZrokNamePrefix(), slug)
}

// zrokURLFor returns https://<name>.<domain> for a worktree slug.
func zrokURLFor(cfg *config.Config, slug string) string {
	return proxy.ZrokURL(zrokNameFor(cfg, slug), cfg.ProxyDomain())
}

// zrokTargetFor picks the share backend target: the worktree app port.
func zrokTargetFor(rec ports.WorktreeRecord) (string, error) {
	app, ok := rec.Ports[ports.PortApp]
	if !ok || app <= 0 || app > 65535 {
		return "", fmt.Errorf("worktree %q has no app port (see status)", rec.Branch)
	}
	return proxy.ZrokTarget(app), nil
}

// zrokPrereq checks the CLI is present + enabled. Install/enable stay
// outside wrk3; failures hint at `zrok2 enable`.
func zrokPrereq(ctx context.Context, cfg *config.Config) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return proxy.ZrokCheckInstalled(ctx, proxy.ZrokBin(cfg.ZrokBinary()))
}

// ensureZrokName reserves the name (idempotent). Warns, never fails hard
// beyond returning the error for the caller to log.
func ensureZrokName(ctx context.Context, cfg *config.Config, slug string) error {
	bin := proxy.ZrokBin(cfg.ZrokBinary())
	name := zrokNameFor(cfg, slug)
	ctx2, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	return proxy.ZrokCreateName(ctx2, bin, cfg.ZrokNamespace(), name)
}

// ensureZrokShare ensures the name + background share for rec. It is
// best-effort like the local gateway: errors come back for the caller to
// log as warnings; the worktree stays reachable via localhost ports.
func ensureZrokShare(ctx context.Context, cfg *config.Config, rec ports.WorktreeRecord) (msg, warned string) {
	if cfg == nil || !cfg.Proxy.Enabled || !cfg.IsZrok() {
		return "", ""
	}
	slug := rec.Slug
	if proxy.ZrokShareRunning(cfg.AbsWorktreeBase(), slug) {
		return "", ""
	}
	if err := zrokPrereq(ctx, cfg); err != nil {
		return "", fmt.Sprintf("zrok share for %q not started (%v)", slug, err)
	}
	name := zrokNameFor(cfg, slug)
	if err := ensureZrokName(ctx, cfg, slug); err != nil {
		return "", fmt.Sprintf("zrok share for %q not started (%v)", slug, err)
	}
	target, err := zrokTargetFor(rec)
	if err != nil {
		return "", fmt.Sprintf("zrok share for %q not started (%v)", slug, err)
	}
	if err := startZrokShareDetached(cfg, slug, name, target); err != nil {
		return "", fmt.Sprintf("zrok share for %q not started (%v)", slug, err)
	}
	return fmt.Sprintf("zrok share up for %q -> %s", slug, zrokURLFor(cfg, slug)), ""
}

// startZrokShareDetached spawns `zrok2 share public ... --headless
// --force-local` detached with logs next to state, tracking the pid per slug.
func startZrokShareDetached(cfg *config.Config, slug, name, target string) error {
	bin := proxy.ZrokBin(cfg.ZrokBinary())
	args := proxy.ZrokShareArgs(target, cfg.ZrokNamespace(), name, cfg.ZrokBackendMode())
	if err := os.MkdirAll(cfg.AbsWorktreeBase(), 0o755); err != nil {
		return fmt.Errorf("create worktree base: %w", err)
	}
	logF, err := os.OpenFile(proxy.ZrokLogPath(cfg.AbsWorktreeBase(), slug), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open zrok log: %w", err)
	}
	defer func() { _ = logF.Close() }()
	cmd := exec.Command(bin, args...)
	cmd.Dir = cfg.RepoPath()
	cmd.Stdout = logF
	cmd.Stderr = logF
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("spawn zrok share: %w", err)
	}
	pid := 0
	if cmd.Process != nil {
		pid = cmd.Process.Pid
	}
	_ = cmd.Process.Release()
	if pid <= 0 {
		return fmt.Errorf("spawn zrok share: no pid")
	}
	return proxy.ZrokWritePid(cfg.AbsWorktreeBase(), slug, pid, name, target)
}

// stopZrokShare kills the per-slug share process, keeping the reserved
// name for fast re-up. Missing process is not an error.
func stopZrokShare(cfg *config.Config, slug string) {
	rawPidPath := proxy.ZrokPidPath(cfg.AbsWorktreeBase(), slug)
	raw, err := os.ReadFile(rawPidPath)
	if err != nil {
		return
	}
	var p struct {
		PID int `json:"pid"`
	}
	if err := json.Unmarshal(raw, &p); err != nil || p.PID <= 0 {
		proxy.ZrokRemovePid(cfg.AbsWorktreeBase(), slug)
		return
	}
	if proc, err := os.FindProcess(p.PID); err == nil {
		_ = proc.Kill()
	}
	proxy.ZrokRemovePid(cfg.AbsWorktreeBase(), slug)
}

// releaseZrokShare stops the share and deletes the reserved name. Names
// outside the configured prefix are never deleted.
func releaseZrokShare(ctx context.Context, cfg *config.Config, slug string) error {
	stopZrokShare(cfg, slug)
	if cfg == nil || !cfg.IsZrok() {
		return nil
	}
	bin := proxy.ZrokBin(cfg.ZrokBinary())
	name := zrokNameFor(cfg, slug)
	prefix := cfg.ZrokNamePrefix()
	ctx2, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	return proxy.ZrokDeleteName(ctx2, bin, cfg.ZrokNamespace(), prefix, name)
}

// logZrokResult logs a per-worktree share ensure outcome (warnings only;
// shares are best-effort like the local gateway).
func logZrokResult(cmd *cobra.Command, r *resolved, msg, warn string) {
	if msg != "" {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), msg)
	} else if warn != "" {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", warn)
	}
	if r == nil {
		return
	}
	switch {
	case msg != "":
		r.oplog(syslog.Entry{Level: syslog.LevelInfo, Op: "zrok", Msg: msg})
	case warn != "":
		r.oplog(syslog.Entry{Level: syslog.LevelWarn, Op: "zrok", Msg: "warning: " + warn})
	}
}
