package cmd

import (
	"net"
	"os"
	"strings"
	"syscall"
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

func permissionDeniedListenErr() error {
	return &net.OpError{
		Op:   "listen",
		Net:  "tcp",
		Addr: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 80},
		Err:  os.NewSyscallError("bind", syscall.EACCES),
	}
}

func addrInUseListenErr() error {
	return &net.OpError{
		Op:   "listen",
		Net:  "tcp",
		Addr: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 80},
		Err:  os.NewSyscallError("bind", syscall.EADDRINUSE),
	}
}

func genericListenErr() error {
	return &net.OpError{
		Op:   "listen",
		Net:  "tcp",
		Addr: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8080},
		Err:  os.NewSyscallError("bind", syscall.ENOTCONN),
	}
}

func TestWrapListenError_PermissionDenied(t *testing.T) {
	err := permissionDeniedListenErr()
	got := wrapListenError("127.0.0.1:80", err)
	msg := got.Error()
	if !strings.Contains(msg, "127.0.0.1:80") {
		t.Errorf("wrapListenError missing address: %q", msg)
	}
	if !strings.Contains(msg, "extra permission required") {
		t.Errorf("wrapListenError missing permission hint: %q", msg)
	}
	if !strings.Contains(msg, "127.0.0.1:8080") {
		t.Errorf("wrapListenError missing default port hint: %q", msg)
	}
}

func TestWrapListenError_AddrInUse(t *testing.T) {
	err := addrInUseListenErr()
	got := wrapListenError("127.0.0.1:80", err)
	msg := got.Error()
	if strings.Contains(msg, "extra permission") {
		t.Errorf("wrapListenError should not add permission hint for EADDRINUSE: %q", msg)
	}
	if !strings.Contains(msg, "proxy listen") {
		t.Errorf("wrapListenError missing 'proxy listen' prefix: %q", msg)
	}
	if !strings.Contains(msg, "address already in use") {
		t.Errorf("wrapListenError should preserve original error detail: %q", msg)
	}
}

func TestWrapListenError_Generic(t *testing.T) {
	err := genericListenErr()
	got := wrapListenError("127.0.0.1:8080", err)
	msg := got.Error()
	if strings.Contains(msg, "extra permission") {
		t.Errorf("wrapListenError should not add permission hint for generic error: %q", msg)
	}
	if !strings.Contains(msg, "proxy listen") {
		t.Errorf("wrapListenError missing 'proxy listen' prefix: %q", msg)
	}
}
