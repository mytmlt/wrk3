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

func TestWrite_PreservesSecrets(t *testing.T) {
	dir := t.TempDir()
	wt := filepath.Join(dir, "feature-foo")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := "# my project\nSECRET=topsecret\nDATABASE_URL=postgres://u:p@db/x\nAPP_PORT=9999\n"
	if err := os.WriteFile(filepath.Join(wt, EnvFileName), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	a := Allocator{Base: DefaultBase(), Step: DefaultStep}
	if err := Write(wt, a.Allocate(0).Ports); err != nil {
		t.Fatalf("Write() = %v", err)
	}
	got, err := os.ReadFile(filepath.Join(wt, EnvFileName))
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	for _, want := range []string{
		"# my project\n",
		"SECRET=topsecret\n",
		"DATABASE_URL=postgres://u:p@db/x\n",
		"APP_PORT=8000\n",
		"BASE_URL=http://localhost:8000\n",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("merged .env missing %q.\n%s", want, s)
		}
	}
	if strings.Contains(s, "9999") {
		t.Errorf("stale APP_PORT value survived.\n%s", s)
	}
}

func TestWrite_AppendsMissingUnderMarker(t *testing.T) {
	dir := t.TempDir()
	wt := filepath.Join(dir, "feature-foo")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, EnvFileName), []byte("SECRET=xyz\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := Allocator{Base: DefaultBase(), Step: DefaultStep}
	if err := Write(wt, a.Allocate(0).Ports); err != nil {
		t.Fatalf("Write() = %v", err)
	}
	got, err := os.ReadFile(filepath.Join(wt, EnvFileName))
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if !strings.HasPrefix(s, "SECRET=xyz\n") {
		t.Errorf("user lines must stay first.\n%s", s)
	}
	if !strings.Contains(s, managedHeader+"\n") {
		t.Errorf("missing managed marker.\n%s", s)
	}
}

func TestWrite_Idempotent(t *testing.T) {
	dir := t.TempDir()
	wt := filepath.Join(dir, "feature-foo")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, EnvFileName), []byte("SECRET=xyz\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := Allocator{Base: DefaultBase(), Step: DefaultStep}
	if err := Write(wt, a.Allocate(0).Ports); err != nil {
		t.Fatalf("Write() #1 = %v", err)
	}
	first, err := os.ReadFile(filepath.Join(wt, EnvFileName))
	if err != nil {
		t.Fatal(err)
	}
	if err := Write(wt, a.Allocate(0).Ports); err != nil {
		t.Fatalf("Write() #2 = %v", err)
	}
	second, err := os.ReadFile(filepath.Join(wt, EnvFileName))
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Errorf("second Write changed content.\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
	if got, want := strings.Count(string(second), "APP_PORT="), 1; got != want {
		t.Errorf("APP_PORT= count = %d, want %d.\n%s", got, want, second)
	}
}

func TestWrite_ExportAndSpacingForms(t *testing.T) {
	dir := t.TempDir()
	wt := filepath.Join(dir, "feature-foo")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := "export APP_PORT=9999\nBASE_URL = http://old # keep me\n"
	if err := os.WriteFile(filepath.Join(wt, EnvFileName), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	a := Allocator{Base: DefaultBase(), Step: DefaultStep}
	if err := Write(wt, a.Allocate(0).Ports); err != nil {
		t.Fatalf("Write() = %v", err)
	}
	got, err := os.ReadFile(filepath.Join(wt, EnvFileName))
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if !strings.Contains(s, "export APP_PORT=8000\n") {
		t.Errorf("export form not updated.\n%s", s)
	}
	if !strings.Contains(s, "BASE_URL=http://localhost:8000 # keep me\n") {
		t.Errorf("spacing/comment form not preserved.\n%s", s)
	}
}

func TestStripManaged_PreservesUserKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, EnvFileName)
	content := "# Generated by wrk3 — do not edit.\nSECRET=xyz\nAPP_PORT=8000\nBASE_URL=http://localhost:8000\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	a := Allocator{Base: DefaultBase(), Step: DefaultStep}
	deleted, err := StripManaged(path, a.Allocate(0).Ports)
	if err != nil {
		t.Fatalf("StripManaged() = %v", err)
	}
	if deleted {
		t.Fatalf("StripManaged() deleted file with user keys")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "SECRET=xyz\n" {
		t.Errorf("got %q, want user keys only", got)
	}
}

func TestStripManaged_DeletesWhenOnlyManaged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, EnvFileName)
	a := Allocator{Base: DefaultBase(), Step: DefaultStep}
	content, err := Render(a.Allocate(0).Ports)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	deleted, err := StripManaged(path, a.Allocate(0).Ports)
	if err != nil {
		t.Fatalf("StripManaged() = %v", err)
	}
	if !deleted {
		t.Errorf("StripManaged() = false, want true for managed-only file")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected .env deleted, stat err = %v", err)
	}
}
