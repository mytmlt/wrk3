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

func TestEnsure_FreshMatchesGolden(t *testing.T) {
	a := Allocator{Base: DefaultBase(), Step: DefaultStep}
	dir := t.TempDir()
	wt := filepath.Join(dir, "feature-foo")
	added, diverged, err := Ensure(wt, a.Allocate(0).Ports)
	if err != nil {
		t.Fatalf("Ensure() = %v", err)
	}
	if len(diverged) != 0 {
		t.Errorf("Ensure() diverged = %v, want none for fresh file", diverged)
	}
	if len(added) == 0 {
		t.Errorf("Ensure() added nothing for fresh file")
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
		"app":     "APP_PORT",
		"web":     "WEB_PORT",
		"api-v2":  "API_V2_PORT",
		"dbAdmin": "DBADMIN_PORT",
		"":        "PORT",
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

func TestEnsure_EmptyPath(t *testing.T) {
	if _, _, err := Ensure("", DefaultBase()); err == nil {
		t.Errorf("Ensure() with empty path = nil, want error")
	}
}

func TestEnsure_NeverOverridesExisting(t *testing.T) {
	dir := t.TempDir()
	wt := filepath.Join(dir, "feature-foo")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	// Stale managed values plus user secrets: nothing may be modified,
	// only missing managed keys are appended; divergences are reported.
	existing := "# my project\nSECRET=topsecret\nDATABASE_URL=postgres://u:p@db/x\nAPP_PORT=9999\n"
	if err := os.WriteFile(filepath.Join(wt, EnvFileName), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	a := Allocator{Base: DefaultBase(), Step: DefaultStep}
	added, diverged, err := Ensure(wt, a.Allocate(0).Ports)
	if err != nil {
		t.Fatalf("Ensure() = %v", err)
	}
	if got, ok := diverged["APP_PORT"]; !ok || got != "9999" {
		t.Errorf("Ensure() diverged = %v, want APP_PORT=9999", diverged)
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
		"APP_PORT=9999\n",                  // left intact, never overwritten
		"BASE_URL=http://localhost:8000\n", // missing: appended
	} {
		if !strings.Contains(s, want) {
			t.Errorf("merged .env missing %q.\n%s", want, s)
		}
	}
	if strings.Contains(s, "APP_PORT=8000") {
		t.Errorf("existing APP_PORT was overwritten.\n%s", s)
	}
	_ = added
}

func TestEnsure_AppendsMissingUnderMarker(t *testing.T) {
	dir := t.TempDir()
	wt := filepath.Join(dir, "feature-foo")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, EnvFileName), []byte("SECRET=xyz\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := Allocator{Base: DefaultBase(), Step: DefaultStep}
	added, diverged, err := Ensure(wt, a.Allocate(0).Ports)
	if err != nil {
		t.Fatalf("Ensure() = %v", err)
	}
	if len(diverged) != 0 {
		t.Errorf("Ensure() diverged = %v, want none", diverged)
	}
	if len(added) == 0 {
		t.Errorf("Ensure() added nothing, want missing managed keys")
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
	if !strings.Contains(s, "APP_PORT=8000\n") {
		t.Errorf("missing managed key not appended.\n%s", s)
	}
}

func TestEnsure_Idempotent(t *testing.T) {
	dir := t.TempDir()
	wt := filepath.Join(dir, "feature-foo")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, EnvFileName), []byte("SECRET=xyz\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := Allocator{Base: DefaultBase(), Step: DefaultStep}
	if _, _, err := Ensure(wt, a.Allocate(0).Ports); err != nil {
		t.Fatalf("Ensure() #1 = %v", err)
	}
	first, err := os.ReadFile(filepath.Join(wt, EnvFileName))
	if err != nil {
		t.Fatal(err)
	}
	added, _, err := Ensure(wt, a.Allocate(0).Ports)
	if err != nil {
		t.Fatalf("Ensure() #2 = %v", err)
	}
	if len(added) != 0 {
		t.Errorf("second Ensure() added %v, want none", added)
	}
	second, err := os.ReadFile(filepath.Join(wt, EnvFileName))
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Errorf("second Ensure changed content.\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
	if got, want := strings.Count(string(second), "APP_PORT="), 1; got != want {
		t.Errorf("APP_PORT= count = %d, want %d.\n%s", got, want, second)
	}
}

func TestEnsure_MatchingFormsAreNotDiverged(t *testing.T) {
	dir := t.TempDir()
	wt := filepath.Join(dir, "feature-foo")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	// Same values in export/quoted/spaced forms: present, not diverged,
	// left byte-for-byte intact.
	existing := "export APP_PORT=8000\nBASE_URL = \"http://localhost:8000\" # ours\n"
	if err := os.WriteFile(filepath.Join(wt, EnvFileName), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	a := Allocator{Base: DefaultBase(), Step: DefaultStep}
	added, diverged, err := Ensure(wt, a.Allocate(0).Ports)
	if err != nil {
		t.Fatalf("Ensure() = %v", err)
	}
	if _, ok := diverged["APP_PORT"]; ok {
		t.Errorf("export form falsely diverged: %v", diverged)
	}
	if _, ok := diverged["BASE_URL"]; ok {
		t.Errorf("quoted/spaced form falsely diverged: %v", diverged)
	}
	got, err := os.ReadFile(filepath.Join(wt, EnvFileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), existing) {
		t.Errorf("existing lines were modified.\n%s", got)
	}
	_ = added
}

func TestEnsureInherited_SeedsSecretsNotPorts(t *testing.T) {
	dir := t.TempDir()
	seedDir := filepath.Join(dir, "main")
	wt := filepath.Join(dir, "feature-foo")
	if err := os.MkdirAll(seedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Seed (main .env) holds secrets plus main's own managed values.
	seedContent := "SECRET=topsecret\nAPP_PORT=7900\nBASE_URL=http://localhost:7900\n"
	seedPath := filepath.Join(seedDir, EnvFileName)
	if err := os.WriteFile(seedPath, []byte(seedContent), 0o644); err != nil {
		t.Fatal(err)
	}
	a := Allocator{Base: DefaultBase(), Step: DefaultStep}
	added, diverged, err := EnsureInherited(wt, seedPath, a.Allocate(0).Ports)
	if err != nil {
		t.Fatalf("EnsureInherited() = %v", err)
	}
	if len(diverged) != 0 {
		t.Errorf("EnsureInherited() diverged = %v, want none", diverged)
	}
	if len(added) == 0 {
		t.Errorf("EnsureInherited() added nothing")
	}
	got, err := os.ReadFile(filepath.Join(wt, EnvFileName))
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if !strings.Contains(s, "SECRET=topsecret\n") {
		t.Errorf("secret not inherited.\n%s", s)
	}
	if !strings.Contains(s, "APP_PORT=8000\n") {
		t.Errorf("own allocation missing.\n%s", s)
	}
	if strings.Contains(s, "7900") {
		t.Errorf("seed's managed values leaked into worktree.\n%s", s)
	}
}

func TestEnsureInherited_MissingSeedBehavesLikeEnsure(t *testing.T) {
	dir := t.TempDir()
	wt := filepath.Join(dir, "feature-foo")
	a := Allocator{Base: DefaultBase(), Step: DefaultStep}
	if _, _, err := EnsureInherited(wt, filepath.Join(dir, "nope", EnvFileName), a.Allocate(0).Ports); err != nil {
		t.Fatalf("EnsureInherited() = %v", err)
	}
	got, err := os.ReadFile(filepath.Join(wt, EnvFileName))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", ".env.golden"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("written .env mismatch.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestEnsureInherited_ExistingFileNotReseeded(t *testing.T) {
	dir := t.TempDir()
	seedPath := filepath.Join(dir, "seed.env")
	wt := filepath.Join(dir, "feature-foo")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(seedPath, []byte("SEED_SECRET=from-main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, EnvFileName), []byte("OWN_SECRET=mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := Allocator{Base: DefaultBase(), Step: DefaultStep}
	if _, _, err := EnsureInherited(wt, seedPath, a.Allocate(0).Ports); err != nil {
		t.Fatalf("EnsureInherited() = %v", err)
	}
	got, err := os.ReadFile(filepath.Join(wt, EnvFileName))
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if !strings.Contains(s, "OWN_SECRET=mine\n") {
		t.Errorf("own secret lost.\n%s", s)
	}
	if strings.Contains(s, "SEED_SECRET=") {
		t.Errorf("existing file was reseeded.\n%s", s)
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

func TestReadPorts_RecoversAllocation(t *testing.T) {
	dir := t.TempDir()
	base := map[string]int{"app": 8000, "web": 3000}
	if _, _, err := Ensure(dir, map[string]int{"app": 8100, "web": 3100}); err != nil {
		t.Fatal(err)
	}
	got, ok := ReadPorts(dir, base)
	if !ok {
		t.Fatal("expected recovery, got not-ok")
	}
	if got["app"] != 8100 || got["web"] != 3100 {
		t.Errorf("got %v, want app=8100 web=3100", got)
	}
}

func TestReadPorts_MissingOrInvalid(t *testing.T) {
	base := map[string]int{"app": 8000}
	empty := t.TempDir()
	if _, ok := ReadPorts(empty, base); ok {
		t.Error("missing .env must not recover")
	}
	partial := t.TempDir()
	if err := os.WriteFile(partial+"/.env", []byte("WEB_PORT=3000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := ReadPorts(partial, base); ok {
		t.Error("partial .env must not recover")
	}
	bad := t.TempDir()
	if err := os.WriteFile(bad+"/.env", []byte("APP_PORT=abc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := ReadPorts(bad, base); ok {
		t.Error("invalid port must not recover")
	}
}
