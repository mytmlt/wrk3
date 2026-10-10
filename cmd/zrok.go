package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/mytmlt/wrk3/internal/config"
	"github.com/mytmlt/wrk3/internal/ports"
	"github.com/mytmlt/wrk3/internal/proxy"
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
// A live share whose stored {name,target,namespace} differs from the desired
// values (prefix change, port reallocation, namespace change) is restarted
// so status/APP_URL never point at a stale share. backendMode is not
// compared: config validation pins it to "proxy".
func ensureZrokShare(ctx context.Context, cfg *config.Config, rec ports.WorktreeRecord) (msg, warned string) {
	if cfg == nil || !cfg.Proxy.Enabled || !cfg.IsZrok() {
		return "", ""
	}
	slug := rec.Slug
	name := zrokNameFor(cfg, slug)
	target, err := zrokTargetFor(rec)
	if err != nil {
		return "", fmt.Sprintf("zrok share for %q not started (%v)", slug, err)
	}
	namespace := cfg.ZrokNamespace()
	if proxy.ZrokShareRunning(cfg.AbsWorktreeBase(), slug) {
		if storedName, storedTarget, storedNamespace, ok := proxy.ZrokShareIdentity(cfg.AbsWorktreeBase(), slug); ok &&
			storedName == name && storedTarget == target && storedNamespace == namespace {
			return "", ""
		}
		stopZrokShare(cfg, slug)
		if proxy.ZrokShareRunning(cfg.AbsWorktreeBase(), slug) {
			return "", fmt.Sprintf("zrok share for %q still running (stop failed; not starting a second share)", slug)
		}
	}
	if err := zrokPrereq(ctx, cfg); err != nil {
		return "", fmt.Sprintf("zrok share for %q not started (%v)", slug, err)
	}
	if err := ensureZrokName(ctx, cfg, slug); err != nil {
		return "", fmt.Sprintf("zrok share for %q not started (%v)", slug, err)
	}
	if err := startZrokShareDetached(cfg, slug, name, target); err != nil {
		return "", fmt.Sprintf("zrok share for %q not started (%v)", slug, err)
	}
	// Brief grace so an instantly-exiting share (bad target, name race,
	// frontend error) surfaces now instead of on the next status poll —
	// mirrors the local gateway dial grace in ensureProxyForCfg. Settles
	// first, then requires the share to stay alive across the window: a
	// share that exits 50-500ms after spawn must warn, not report success.
	time.Sleep(500 * time.Millisecond)
	alive := proxy.ZrokShareRunning(cfg.AbsWorktreeBase(), slug)
	for i := 0; i < 5 && alive; i++ {
		time.Sleep(100 * time.Millisecond)
		alive = proxy.ZrokShareRunning(cfg.AbsWorktreeBase(), slug)
	}
	if !alive {
		return "", fmt.Sprintf("zrok share for %q exited immediately (see %s)", slug, proxy.ZrokLogPath(cfg.AbsWorktreeBase(), slug))
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
	return proxy.ZrokWritePid(cfg.AbsWorktreeBase(), slug, pid, name, target, cfg.ZrokNamespace())
}

// stopZrokShare kills the per-slug share process, keeping the reserved
// name for fast re-up. Missing process is not an error. A pid file
// pointing at a non-zrok process is dropped without killing (stale PID
// reuse must never kill an unrelated process). A failed Kill keeps the
// pid file so callers do not spawn a second share for the same name.
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
	if !proxy.ZrokOwnsPID(p.PID) {
		proxy.ZrokRemovePid(cfg.AbsWorktreeBase(), slug)
		return
	}
	if proc, err := os.FindProcess(p.PID); err == nil {
		if err := proc.Kill(); err != nil && !os.IsNotExist(err) {
			return
		}
	}
	proxy.ZrokRemovePid(cfg.AbsWorktreeBase(), slug)
}

// releaseZrokShare stops the share and deletes the reserved name. The name
// is read from the pid file first (falling back to the config-derived one
// when no pid file exists) so a prefix change with a live share cannot
// orphan the old reservation. Recomputed fallback names outside the
// configured prefix are never deleted; stored names skip the guard (the pid
// file proves wrk3 created them). Limitation: down stops the share and
// removes the pid file while keeping the reservation, so a prefix change
// between down and remove falls back to the current-prefix name and may
// orphan the old reservation — run remove before renaming the prefix, or
// delete the leftover name manually.
func releaseZrokShare(ctx context.Context, cfg *config.Config, slug string) error {
	if cfg == nil || !cfg.IsZrok() {
		return nil
	}
	storedName, _, storedNamespace, hasStored := proxy.ZrokShareIdentity(cfg.AbsWorktreeBase(), slug)
	stopZrokShare(cfg, slug)
	bin := proxy.ZrokBin(cfg.ZrokBinary())
	name := zrokNameFor(cfg, slug)
	prefix := cfg.ZrokNamePrefix()
	namespace := cfg.ZrokNamespace()
	if hasStored {
		// The pid file proves wrk3 created this reservation, so the
		// prefix guard is skipped: the stored name may predate a prefix
		// change that the guard would otherwise refuse. The stored
		// namespace wins too, or a namespace change orphans the old
		// reservation (delete would target the new namespace no-op).
		name, prefix = storedName, ""
		if storedNamespace != "" {
			namespace = storedNamespace
		}
	}
	ctx2, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	return proxy.ZrokDeleteName(ctx2, bin, namespace, prefix, name)
}
