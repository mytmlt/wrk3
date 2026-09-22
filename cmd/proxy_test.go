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

func TestProxyEnvForSlugPrivilegedAddr(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	cfg.Proxy.Enabled = true
	cfg.Proxy.Domain = "localhost"
	cfg.Proxy.Addr = "127.0.0.1:80"
	got := proxyEnvForSlug(cfg, "pr-1")
	if got[ports.EnvAppURL] != "http://pr-1.localhost:8080" {
		t.Errorf("APP_URL = %v, want unprivileged port", got)
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
