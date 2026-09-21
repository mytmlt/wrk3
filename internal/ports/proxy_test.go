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

func TestStripManagedKeepsAppURL(t *testing.T) {
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
	if strings.Contains(string(raw), "APP_PORT") {
		t.Errorf("managed port keys survive:\n%s", raw)
	}
	if !strings.Contains(string(raw), "APP_URL=http://pr-1.localhost:8080") {
		t.Errorf("user gateway URL must survive:\n%s", raw)
	}
	if !strings.Contains(string(raw), "SECRET=x") {
		t.Errorf("secret lost:\n%s", raw)
	}
	if !strings.Contains(string(raw), "BASE_URL=http://localhost:8000") {
		t.Errorf("user-owned BASE_URL must survive:\n%s", raw)
	}
}
