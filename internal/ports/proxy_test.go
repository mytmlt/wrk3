package ports

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppURL(t *testing.T) {
	if got := AppURL("pr-101", "localhost", 8080); got != "http://pr-101.localhost:8080" {
		t.Errorf("AppURL = %q", got)
	}
	if got := AppURL("pr-101", "", 8080); got != "http://pr-101.localhost:8080" {
		t.Errorf("empty domain = %q", got)
	}
	if got := AppURL("pr-101", "localhost", 80); got != "http://pr-101.localhost" {
		t.Errorf("port 80 = %q", got)
	}
}

func TestEnsureKeysAppendOnly(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("SECRET=x\nAPP_URL=http://old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	added, err := EnsureKeys(dir, map[string]string{EnvAppURL: "http://pr-1.localhost:8080", "EXTRA": "1"})
	if err != nil {
		t.Fatal(err)
	}
	// APP_URL exists (kept old), EXTRA added.
	if len(added) != 1 || added[0] != "EXTRA" {
		t.Errorf("added = %v", added)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, ".env"))
	if !strings.Contains(string(raw), "APP_URL=http://old") || !strings.Contains(string(raw), "SECRET=x") {
		t.Errorf("existing lines modified:\n%s", raw)
	}
}

func TestStripManagedRemovesAppURL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := "SECRET=x\nAPP_PORT=8000\nAPP_URL=http://pr-1.localhost:8080\nBASE_URL=http://localhost:8000\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	deleted, err := StripManaged(path, map[string]int{"app": 8000})
	if err != nil {
		t.Fatal(err)
	}
	if deleted {
		t.Fatal("file with SECRET should survive")
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "APP_URL") || strings.Contains(string(raw), "APP_PORT") {
		t.Errorf("managed keys survive:\n%s", raw)
	}
	if !strings.Contains(string(raw), "SECRET=x") {
		t.Errorf("secret lost:\n%s", raw)
	}
}
