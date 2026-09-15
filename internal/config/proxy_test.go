package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeProxyConfig(t *testing.T, body string) *Config {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "wrk3.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

const proxyBase = `project:
  worktreeBase: .worktrees
source:
  type: git
runner:
  type: docker
  docker:
    composeFiles: [docker-compose.yml]
    projectPrefix: demo
entry:
  run: "echo run"
  stop: "echo stop"
ports:
  base: {app: 8000}
  step: 100
`

func TestProxyDefaultsWhenAbsent(t *testing.T) {
	cfg := writeProxyConfig(t, proxyBase)
	if cfg.Proxy.Enabled {
		t.Error("Enabled should default false")
	}
	if cfg.ProxyDomain() != "localhost" {
		t.Errorf("Domain = %q, want localhost", cfg.ProxyDomain())
	}
	if cfg.ProxyAddr() != "127.0.0.1:8080" {
		t.Errorf("Addr = %q", cfg.ProxyAddr())
	}
	if got := cfg.ProxyURL("pr-101"); got != "http://pr-101.localhost:8080" {
		t.Errorf("ProxyURL = %q", got)
	}
}

func TestProxyEnabledValidation(t *testing.T) {
	cfg := writeProxyConfig(t, proxyBase+"proxy:\n  enabled: true\n  domain: WRK3.Test\n  addr: 127.0.0.1:80\n")
	if cfg.ProxyDomain() != "wrk3.test" {
		t.Errorf("Domain not normalized: %q", cfg.Proxy.Domain)
	}
	if got := cfg.ProxyURL("pr-1"); got != "http://pr-1.wrk3.test" {
		t.Errorf("port 80 URL = %q", got)
	}
	for _, body := range []string{
		proxyBase + "proxy:\n  enabled: true\n  domain: \"bad domain\"\n",
		proxyBase + "proxy:\n  enabled: true\n  addr: \"noport\"\n",
		proxyBase + "proxy:\n  enabled: true\n  addr: \"127.0.0.1:99999\"\n",
	} {
		dir := t.TempDir()
		path := filepath.Join(dir, "wrk3.yaml")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil {
			t.Errorf("Load(%q) = nil, want error", body)
		}
	}
}
