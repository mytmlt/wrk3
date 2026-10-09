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
  ranges:
    app: [8000, 8099]
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
	cfg := writeProxyConfig(t, proxyBase+"proxy:\n  enabled: true\n  domain: WRK3.Test\n  addr: 127.0.0.1:8080\n")
	if cfg.ProxyDomain() != "wrk3.test" {
		t.Errorf("Domain not normalized: %q", cfg.Proxy.Domain)
	}
	if got := cfg.ProxyURL("pr-1"); got != "http://pr-1.wrk3.test:8080" {
		t.Errorf("proxy URL = %q", got)
	}
	for _, body := range []string{
		proxyBase + "proxy:\n  enabled: true\n  domain: \"bad domain\"\n",
		proxyBase + "proxy:\n  enabled: true\n  addr: \"noport\"\n",
		proxyBase + "proxy:\n  enabled: true\n  addr: \"127.0.0.1:99999\"\n",
		proxyBase + "proxy:\n  enabled: true\n  addr: \"127.0.0.1:80\"\n",
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

func TestProxyProviderDefaultsLocal(t *testing.T) {
	cfg := writeProxyConfig(t, proxyBase)
	if cfg.ProxyProvider() != "local" {
		t.Errorf("Provider = %q, want local", cfg.ProxyProvider())
	}
	if cfg.IsZrok() {
		t.Error("IsZrok should be false by default")
	}
}

func TestProxyProviderValidation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wrk3.yaml")
	if err := os.WriteFile(path, []byte(proxyBase+"proxy:\n  enabled: true\n  provider: bogus\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Error("provider bogus should fail")
	}
}

func TestProxyZrokValidation(t *testing.T) {
	cfg := writeProxyConfig(t, proxyBase+`proxy:
  enabled: true
  provider: zrok
  domain: tunnel.mytmlt.site
  zrok:
    namespace: public
    namePrefix: myt-home-
`)
	if !cfg.IsZrok() {
		t.Error("IsZrok should be true")
	}
	if cfg.ZrokNamespace() != "public" {
		t.Errorf("Namespace = %q", cfg.ZrokNamespace())
	}
	if got := cfg.ZrokName("demo"); got != "myt-home-demo" {
		t.Errorf("ZrokName = %q", got)
	}
	if got := cfg.ProxyURL("demo"); got != "https://myt-home-demo.tunnel.mytmlt.site" {
		t.Errorf("ProxyURL = %q", got)
	}
	for _, body := range []string{
		// localhost is not a valid zrok frontend domain
		proxyBase + "proxy:\n  enabled: true\n  provider: zrok\n  domain: localhost\n",
		// bad backend mode
		proxyBase + "proxy:\n  enabled: true\n  provider: zrok\n  domain: tunnel.mytmlt.site\n  zrok:\n    backendMode: web\n",
		// bad prefix chars
		proxyBase + "proxy:\n  enabled: true\n  provider: zrok\n  domain: tunnel.mytmlt.site\n  zrok:\n    namePrefix: \"bad prefix!\"\n",
	} {
		dir := t.TempDir()
		p := filepath.Join(dir, "wrk3.yaml")
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(p); err == nil {
			t.Errorf("Load(%q) = nil, want error", body)
		}
	}
}

func TestProxyZrokPrefixDerivesFromProjectPrefix(t *testing.T) {
	cfg := writeProxyConfig(t, proxyBase+`proxy:
  enabled: true
  provider: zrok
  domain: tunnel.mytmlt.site
`)
	// proxyBase sets projectPrefix: demo
	if got := cfg.ZrokNamePrefix(); got != "demo-" {
		t.Errorf("derived prefix = %q, want demo-", got)
	}
	if got := cfg.ProxyURL("pr-1"); got != "https://demo-pr-1.tunnel.mytmlt.site" {
		t.Errorf("ProxyURL = %q", got)
	}
}
