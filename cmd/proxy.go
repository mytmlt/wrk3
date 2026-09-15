package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/mytmlt/wrk3/internal/config"
	"github.com/mytmlt/wrk3/internal/ports"
	"github.com/mytmlt/wrk3/internal/proxy"
)

// proxyPid holds the gateway daemon identity on disk.
type proxyPid struct {
	PID    int    `json:"pid"`
	Addr   string `json:"addr"`
	Domain string `json:"domain"`
}

func proxyPidPath(cfg *config.Config) string {
	return proxy.PidPath(cfg.AbsWorktreeBase())
}

func proxyLogPath(cfg *config.Config) string {
	return filepath.Join(cfg.AbsWorktreeBase(), ".wrk3-proxy.log")
}

// proxyRunning dials the gateway addr; a successful dial means serving.
// A stale pid file (nothing listening) is removed best-effort.
func proxyRunning(cfg *config.Config) bool {
	addr := cfg.ProxyAddr()
	conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// proxyEnvForSlug returns {APP_URL} when the gateway is enabled.
func proxyEnvForSlug(cfg *config.Config, slug string) map[string]string {
	if cfg == nil || !cfg.Proxy.Enabled {
		return nil
	}
	port := proxy.GatewayPort(cfg.ProxyAddr())
	return map[string]string{ports.EnvAppURL: ports.AppURL(slug, cfg.ProxyDomain(), port)}
}

// ensureProxyForUp starts the gateway in the background when enabled and
// not already listening. It never fails `up`: bind/spawn errors come back
// as warnings for the caller to log.
func ensureProxyForUp(r *resolved) (msg string, warned string) {
	if r == nil || r.cfg == nil || !r.cfg.Proxy.Enabled {
		return "", ""
	}
	if proxyRunning(r.cfg) {
		return "", ""
	}
	if err := startProxyDetached(r.cfg); err != nil {
		return "", fmt.Sprintf("proxy gateway not started (%v); worktrees still reachable via localhost ports", err)
	}
	// Brief grace so the first proxied request doesn't 502.
	for i := 0; i < 10 && !proxyRunning(r.cfg); i++ {
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Sprintf("proxy gateway up on %s (<slug>.%s)", r.cfg.ProxyAddr(), r.cfg.ProxyDomain()), ""
}

// startProxyDetached spawns `wrk3 proxy run` detached with logs next to state.
func startProxyDetached(cfg *config.Config) error {
	if err := os.MkdirAll(cfg.AbsWorktreeBase(), 0o755); err != nil {
		return fmt.Errorf("create worktree base: %w", err)
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate wrk3 binary: %w", err)
	}
	logF, err := os.OpenFile(proxyLogPath(cfg), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open proxy log: %w", err)
	}
	defer func() { _ = logF.Close() }()
	cmd := exec.Command(exe, "-f", cfg.ConfigPath(), "proxy", "run")
	cmd.Dir = cfg.RepoPath()
	cmd.Stdout = logF
	cmd.Stderr = logF
	// Start + Release detaches the gateway from `up`/`proxy up`; its
	// stdout/stderr go to the proxy log next to the state file.
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("spawn proxy: %w", err)
	}
	_ = cmd.Process.Release()
	return nil
}

// stopProxy kills the gateway via its pid file (or best-effort dial check).
func stopProxy(cfg *config.Config) error {
	raw, err := os.ReadFile(proxyPidPath(cfg))
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("proxy not running (no pid file)")
		}
		return fmt.Errorf("read proxy pid: %w", err)
	}
	var p proxyPid
	if err := json.Unmarshal(raw, &p); err != nil {
		_ = os.Remove(proxyPidPath(cfg))
		return fmt.Errorf("proxy pid file corrupt (removed): %w", err)
	}
	proc, err := os.FindProcess(p.PID)
	if err != nil {
		_ = os.Remove(proxyPidPath(cfg))
		return fmt.Errorf("proxy pid %d not found (pid file removed)", p.PID)
	}
	if err := proc.Kill(); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("stop proxy pid %d: %w", p.PID, err)
	}
	_ = os.Remove(proxyPidPath(cfg))
	return nil
}

// proxyTargetsWithMain loads slug->appPort from state for the gateway handler.
// mainEntry (possibly nil) is merged in so the implicit main checkout URL
// routes too; state wins on slug collision.
func proxyTargetsWithMain(cfg *config.Config, mainEntry *ports.WorktreeRecord) map[string]int {
	recs, err := ports.Load(cfg.StatePath())
	if err != nil {
		recs = []ports.WorktreeRecord{}
	}
	out := make(map[string]int, len(recs)+1)
	for _, rec := range recs {
		if app, ok := rec.Ports[ports.PortApp]; ok {
			out[rec.Slug] = app
		}
	}
	if mainEntry != nil {
		if app, ok := mainEntry.Ports[ports.PortApp]; ok {
			if _, taken := out[mainEntry.Slug]; !taken {
				out[mainEntry.Slug] = app
			}
		}
	}
	return out
}

// proxyHostnames returns the <slug>.<domain> names for hosts-sync/status.
func proxyHostnames(cfg *config.Config, recs []ports.WorktreeRecord) []string {
	out := make([]string, 0, len(recs))
	for _, rec := range recs {
		out = append(out, rec.Slug+"."+cfg.ProxyDomain())
	}
	return out
}

var proxyCmd = &cobra.Command{
	Use:   "proxy",
	Short: "Local gateway: <slug>.localhost URLs for worktrees",
}

var proxyUpCmd = &cobra.Command{
	Use:   "up",
	Short: "Start the local gateway (background)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		r, err := resolveConfig()
		if err != nil {
			return err
		}
		if proxyRunning(r.cfg) {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "proxy already listening on %s\n", r.cfg.ProxyAddr())
			return nil
		}
		if err := startProxyDetached(r.cfg); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "proxy up on %s (<slug>.%s)\n", r.cfg.ProxyAddr(), r.cfg.ProxyDomain())
		return nil
	},
}

var proxyDownCmd = &cobra.Command{
	Use:   "down",
	Short: "Stop the local gateway",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		r, err := resolveConfig()
		if err != nil {
			return err
		}
		if err := stopProxy(r.cfg); err != nil {
			return err
		}
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "proxy down")
		return nil
	},
}

var proxyStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show gateway state + per-worktree URLs",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		r, err := resolveConfig()
		if err != nil {
			return err
		}
		state := "stopped"
		if proxyRunning(r.cfg) {
			state = "listening on " + r.cfg.ProxyAddr()
		}
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "gateway: %s (domain .%s, enabled=%v)\n",
			state, r.cfg.ProxyDomain(), r.cfg.Proxy.Enabled)
		recs, err := recordsForDisplay(r.cfg)
		if err != nil {
			return err
		}
		for _, rec := range recs {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s -> %s (localhost:%d)\n",
				r.cfg.ProxyURL(rec.Slug), rec.Slug, rec.Ports[ports.PortApp])
		}
		return nil
	},
}

var proxyOpenCmd = &cobra.Command{
	Use:               "open <worktree>",
	Short:             "Open the worktree URL in a browser",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeWorktrees,
	RunE: func(cmd *cobra.Command, args []string) error {
		r, err := resolveConfig()
		if err != nil {
			return err
		}
		recs, err := loadState(r)
		if err != nil {
			return err
		}
		rec, err := findRecordIncludingMain(r, recs, args[0])
		if err != nil {
			return err
		}
		if rec == nil {
			return fmt.Errorf("unknown worktree %q (see status)", args[0])
		}
		target := r.cfg.ProxyURL(rec.Slug)
		opener, err := browserOpener()
		if err != nil {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), target)
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		c := exec.CommandContext(ctx, opener[0], append(opener[1:], target)...)
		if err := c.Run(); err != nil {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), target)
			return fmt.Errorf("open browser: %w", err)
		}
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), target)
		return nil
	},
}

// browserOpener returns the OS browser launcher.
func browserOpener() ([]string, error) {
	switch runtime.GOOS {
	case "darwin":
		return []string{"open"}, nil
	case "windows":
		return []string{"cmd", "/c", "start"}, nil
	default:
		return []string{"xdg-open"}, nil
	}
}

var proxyHostsSyncCmd = &cobra.Command{
	Use:   "hosts-sync",
	Short: "Add 127.0.0.1 entries for worktree hostnames (needs sudo for /etc/hosts)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		r, err := resolveConfig()
		if err != nil {
			return err
		}
		hostsPath := hostsFilePath()
		if v := strings.TrimSpace(os.Getenv("WRK3_HOSTS_FILE")); v != "" {
			hostsPath = v
		}
		recs, err := recordsForDisplay(r.cfg)
		if err != nil {
			return err
		}
		added, err := proxy.SyncHosts(hostsPath, proxyHostnames(r.cfg, recs))
		if err != nil {
			return err
		}
		if len(added) == 0 {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "hosts up to date")
			return nil
		}
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "added %d host(s) to %s\n", len(added), hostsPath)
		return nil
	},
}

func hostsFilePath() string {
	if runtime.GOOS == "windows" {
		return `C:\Windows\System32\drivers\etc\hosts`
	}
	return "/etc/hosts"
}

var proxyRunCmd = &cobra.Command{
	Use:    "run",
	Short:  "Run the gateway in the foreground (used by proxy up)",
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		r, err := resolveConfig()
		if err != nil {
			return err
		}
		addr := r.cfg.ProxyAddr()
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("proxy listen %s: %w", addr, err)
		}
		pid := proxyPid{PID: os.Getpid(), Addr: addr, Domain: r.cfg.ProxyDomain()}
		raw, _ := json.Marshal(pid)
		_ = os.MkdirAll(r.cfg.AbsWorktreeBase(), 0o755)
		if err := os.WriteFile(proxyPidPath(r.cfg), append(raw, '\n'), 0o644); err != nil {
			_ = ln.Close()
			return fmt.Errorf("write proxy pid: %w", err)
		}
		defer func() { _ = os.Remove(proxyPidPath(r.cfg)) }()
		// Resolve the implicit main checkout once at startup (branch name
		// needs git; state is still read per request so up/add/remove
		// apply immediately).
		var mainEntry *ports.WorktreeRecord
		if recs, err := ports.Load(r.cfg.StatePath()); err == nil {
			mainEntry, _ = mainRecord(r, recs)
		}
		// ReadHeaderTimeout bounds slowloris-style slow headers on the
		// local-only gateway (gosec G112).
		srv := &http.Server{
			ReadHeaderTimeout: 5 * time.Second,
			Handler: proxy.NewHandler(r.cfg.ProxyDomain(), func() map[string]int {
				return proxyTargetsWithMain(r.cfg, mainEntry)
			}),
		}
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "proxy listening on %s (<slug>.%s)\n", addr, r.cfg.ProxyDomain())
		go func() {
			<-cmd.Context().Done()
			_ = srv.Close()
		}()
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("proxy serve: %w", err)
		}
		return nil
	},
}

func init() {
	proxyCmd.AddCommand(proxyUpCmd, proxyDownCmd, proxyStatusCmd, proxyOpenCmd, proxyHostsSyncCmd, proxyRunCmd)
	rootCmd.AddCommand(proxyCmd)
}
