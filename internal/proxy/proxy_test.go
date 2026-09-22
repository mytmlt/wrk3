package proxy

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestSlugFromHost(t *testing.T) {
	cases := []struct {
		host, domain string
		slug         string
		ok           bool
	}{
		{"pr-101.localhost", "localhost", "pr-101", true},
		{"pr-101.localhost:8080", "localhost", "pr-101", true},
		{"PR-101.LOCALHOST:8080", "localhost", "pr-101", true},
		{"pr-101.localhost.", "localhost", "pr-101", true},
		{"localhost", "localhost", "", false},
		{"localhost:8080", "localhost", "", false},
		{"a.b.localhost", "localhost", "", false},
		{"pr-101.example.com", "localhost", "", false},
		{"", "localhost", "", false},
	}
	for _, tc := range cases {
		if slug, ok := SlugFromHost(tc.host, tc.domain); slug != tc.slug || ok != tc.ok {
			t.Errorf("SlugFromHost(%q) = (%q,%v), want (%q,%v)", tc.host, slug, ok, tc.slug, tc.ok)
		}
	}
}

func TestValidate(t *testing.T) {
	if err := Validate("localhost", "127.0.0.1:8080"); err != nil {
		t.Errorf("valid proxy rejected: %v", err)
	}
	for _, tc := range [][2]string{
		{"", "127.0.0.1:8080"},
		{"local host", "127.0.0.1:8080"},
		{"localhost", "8080"},
		{"localhost", "127.0.0.1:99999"},
		{"localhost", "127.0.0.1:notaport"},
	} {
		if err := Validate(tc[0], tc[1]); err == nil {
			t.Errorf("Validate(%q,%q) = nil, want error", tc[0], tc[1])
		}
	}
}

func TestWrapListenErrorPrivilegedHint(t *testing.T) {
	err := WrapListenError("10.0.0.1:80", os.ErrPermission)
	if err == nil {
		t.Fatal("WrapListenError returned nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "proxy listen 10.0.0.1:80") {
		t.Errorf("missing listen prefix: %q", msg)
	}
	if !strings.Contains(msg, DefaultAddr) {
		t.Errorf("missing default-addr hint: %q", msg)
	}
	if !strings.Contains(msg, "CAP_NET_BIND_SERVICE") {
		t.Errorf("missing capability hint: %q", msg)
	}
	if !errors.Is(err, os.ErrPermission) {
		t.Error("wrapped error should unwrap to permission denied")
	}
	if !IsUnprivilegedListen(err) {
		t.Error("IsUnprivilegedListen = false, want true")
	}
}

func TestWrapListenErrorHighPort(t *testing.T) {
	err := WrapListenError("127.0.0.1:8080", os.ErrPermission)
	if err == nil {
		t.Fatal("WrapListenError returned nil")
	}
	if strings.Contains(err.Error(), "CAP_NET_BIND_SERVICE") {
		t.Errorf("high port should not get privileged hint: %q", err)
	}
	if !strings.Contains(err.Error(), "proxy listen 127.0.0.1:8080") {
		t.Errorf("missing listen prefix: %q", err)
	}
}

func TestIsUnprivilegedListen(t *testing.T) {
	if IsUnprivilegedListen(nil) {
		t.Error("nil should not match")
	}
	if IsUnprivilegedListen(os.ErrPermission) {
		t.Error("bare permission denied is not a listen failure")
	}
	listenDenied := WrapListenError("127.0.0.1:80", os.ErrPermission)
	if !IsUnprivilegedListen(listenDenied) {
		t.Errorf("wrapped privileged listen should match: %v", listenDenied)
	}
}

func TestCheckListenFreeAndBusy(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := CheckListen(addr); err == nil {
		t.Fatal("CheckListen on occupied addr = nil, want error")
	}
	_ = ln.Close()
	if err := CheckListen(addr); err != nil {
		t.Fatalf("CheckListen on free addr: %v", err)
	}
}

func TestListenPrivilegedPort(t *testing.T) {
	_, err := Listen("127.0.0.1:1")
	if err == nil {
		t.Skip("process can bind privileged ports")
	}
	if !errors.Is(err, os.ErrPermission) {
		t.Skipf("listen :1: %v", err)
	}
	if !IsUnprivilegedListen(err) {
		t.Errorf("IsUnprivilegedListen = false for %v", err)
	}
	if !strings.Contains(err.Error(), DefaultAddr) {
		t.Errorf("missing default-addr hint: %q", err)
	}
}

func TestURLForSlug(t *testing.T) {
	if got := URLForSlug("pr-101", "localhost", "127.0.0.1:8080"); got != "http://pr-101.localhost:8080" {
		t.Errorf("URL = %q", got)
	}
	if got := URLForSlug("pr-101", "localhost", "127.0.0.1:80"); got != "http://pr-101.localhost" {
		t.Errorf("port 80 URL = %q", got)
	}
}

func TestHandlerRoutesAnd404(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("from-backend host=" + r.Host))
	}))
	defer backend.Close()
	// Extract backend port from test server URL.
	port := strings.TrimPrefix(backend.URL, "http://127.0.0.1:")
	h := NewHandler("localhost", func() map[string]int {
		n := 0
		for _, r := range port {
			if r < '0' || r > '9' {
				n = -1
				break
			}
			n = n*10 + int(r-'0')
		}
		return map[string]int{"pr-101": n}
	})
	req := httptest.NewRequest("GET", "http://pr-101.localhost:8080/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "from-backend") {
		t.Fatalf("route = %d %q", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest("GET", "http://unknown.localhost:8080/", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown slug = %d, want 404", rec.Code)
	}
}

func TestSyncHostsContentIdempotent(t *testing.T) {
	out := SyncHostsContent("127.0.0.1 localhost\n", []string{"pr-101.localhost"})
	if !strings.Contains(out, "127.0.0.1 pr-101.localhost") || !strings.Contains(out, HostsMarker) {
		t.Fatalf("sync missing entry:\n%s", out)
	}
	again := SyncHostsContent(out, []string{"pr-101.localhost"})
	if again != out {
		t.Fatalf("second sync not idempotent:\n%s", again)
	}
}
