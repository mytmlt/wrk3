package ports

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRender_Golden(t *testing.T) {
	a := Allocator{Base: DefaultBase(), Step: DefaultStep}
	got, err := Render(a.Allocate(0).Ports)
	if err != nil {
		t.Fatalf("Render() = %v", err)
	}
	raw, err := os.ReadFile(filepath.Join("testdata", ".env.golden"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if got != string(raw) {
		t.Errorf("Render() mismatch with golden.\n--- got ---\n%s\n--- want ---\n%s", got, raw)
	}
}

func TestWrite_RoundTripGolden(t *testing.T) {
	a := Allocator{Base: DefaultBase(), Step: DefaultStep}
	dir := t.TempDir()
	wt := filepath.Join(dir, "feature-foo")
	if err := Write(wt, a.Allocate(0).Ports); err != nil {
		t.Fatalf("Write() = %v", err)
	}
	got, err := os.ReadFile(filepath.Join(wt, EnvFileName))
	if err != nil {
		t.Fatalf("read written .env: %v", err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", ".env.golden"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("written .env mismatch.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestRender_DerivedURLs(t *testing.T) {
	a := Allocator{Base: DefaultBase(), Step: DefaultStep}
	got, err := Render(a.Allocate(2).Ports)
	if err != nil {
		t.Fatalf("Render() = %v", err)
	}
	// index 2: app = 8000 + 200 = 8200.
	for _, want := range []string{
		"APP_PORT=8200",
		"BASE_URL=http://localhost:8200",
		"WEBHOOKS_BASE_URL=http://localhost:8200",
		"ALLOWED_WS_ORIGINS=http://localhost:8200",
	} {
		if !strings.Contains(got, want+"\n") {
			t.Errorf("Render() missing %q.\n%s", want, got)
		}
	}
}

func TestRender_CustomPorts(t *testing.T) {
	got, err := Render(map[string]int{"app": 8000, "web": 3000})
	if err != nil {
		t.Fatalf("Render() = %v", err)
	}
	for _, want := range []string{"APP_PORT=8000\n", "WEB_PORT=3000\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("Render() missing %q.\n%s", want, got)
		}
	}
}

func TestEnvVarForPort(t *testing.T) {
	cases := map[string]string{
		"app":    "APP_PORT",
		"web":    "WEB_PORT",
		"api-v2": "API_V2_PORT",
		"dbAdmin": "DBADMIN_PORT",
		"":       "PORT",
	}
	for in, want := range cases {
		if got := EnvVarForPort(in); got != want {
			t.Errorf("EnvVarForPort(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRender_MissingPort(t *testing.T) {
	ports := DefaultBase()
	delete(ports, PortApp)
	if _, err := Render(ports); err == nil {
		t.Errorf("Render() with missing port = nil, want error")
	}
}

func TestWrite_EmptyPath(t *testing.T) {
	if err := Write("", DefaultBase()); err == nil {
		t.Errorf("Write() with empty path = nil, want error")
	}
}
