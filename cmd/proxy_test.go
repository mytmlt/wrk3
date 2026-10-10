package cmd

import (
	"testing"

	"github.com/mytmlt/wrk3/internal/ports"
)

func TestProxyEnvForSlugDisabled(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	if got := proxyEnvForSlug(cfg, "pr-1"); len(got) != 0 {
		t.Errorf("disabled proxy env = %v, want empty", got)
	}
}

func TestProxyEnvForSlugEnabled(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	cfg.Proxy.Enabled = true
	cfg.Proxy.Domain = "localhost"
	cfg.Proxy.Addr = "127.0.0.1:8080"
	got := proxyEnvForSlug(cfg, "pr-1")
	if got[ports.EnvAppURL] != "http://pr-1.localhost:8080" {
		t.Errorf("APP_URL = %v", got)
	}
	merged := envForWorktree(cfg, makeRecord("b", "b"))
	if merged[ports.EnvAppURL] == "" || merged[ports.EnvApp] == "" {
		t.Errorf("merged env missing keys: %v", merged)
	}
}

func makeRecord(branch, slug string) ports.WorktreeRecord {
	return ports.WorktreeRecord{Branch: branch, Slug: slug, Ports: map[string]int{"app": 8000}}
}

func TestEnsureProxyForUpDisabledNoop(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	r := &resolved{cfg: cfg}
	if msg, warn := ensureProxyForUp(r); msg != "" || warn != "" {
		t.Errorf("disabled ensure = (%q,%q), want empty", msg, warn)
	}
}

func TestProxyEnvForSlugZrok(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	cfg.Proxy.Enabled = true
	cfg.Proxy.Provider = "zrok"
	cfg.Proxy.Domain = "tunnel.mytmlt.site"
	cfg.Proxy.Zrok.Namespace = "public"
	cfg.Proxy.Zrok.NamePrefix = "myt-home-"
	got := proxyEnvForSlug(cfg, "demo")
	if got[ports.EnvAppURL] != "https://myt-home-demo.tunnel.mytmlt.site" {
		t.Errorf("APP_URL = %v", got)
	}
}

func TestEnsureProxyForUpZrokNoop(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	cfg.Proxy.Enabled = true
	cfg.Proxy.Provider = "zrok"
	cfg.Proxy.Domain = "tunnel.mytmlt.site"
	r := &resolved{cfg: cfg}
	if msg, warn := ensureProxyForUp(r); msg != "" || warn != "" {
		t.Errorf("zrok ensure = (%q,%q), want empty (per-worktree shares)", msg, warn)
	}
}

func TestZrokNameFor(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	cfg.Proxy.Zrok.NamePrefix = "myt-home-"
	if got := zrokNameFor(cfg, "demo"); got != "myt-home-demo" {
		t.Errorf("zrokNameFor = %q", got)
	}
}

func TestZrokTargetForMissingApp(t *testing.T) {
	if _, err := zrokTargetFor(ports.WorktreeRecord{Branch: "b", Slug: "b"}); err == nil {
		t.Error("missing app port should fail")
	}
}

func TestEnsureProxyForCfgDisabledNoop(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	if warn := ensureProxyForCfg(cfg); warn != "" {
		t.Errorf("disabled ensure = %q, want empty", warn)
	}
	if warn := ensureProxyForCfg(nil); warn != "" {
		t.Errorf("nil ensure = %q, want empty", warn)
	}
}
