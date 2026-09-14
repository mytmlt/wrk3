package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTask_AnalyzeCompose(t *testing.T) {
	dir := t.TempDir()
	yml := `services:
  web:
    image: nginx:alpine
    ports:
      - "8080:80"
`
	if err := os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errBuf bytes.Buffer
	taskFrom, taskTo, taskDir = "", "internal", dir
	t.Cleanup(func() {
		taskFrom, taskTo, taskDir = "", "internal", "."
		taskCmd.SetOut(nil)
		taskCmd.SetErr(nil)
	})
	taskCmd.SetOut(&out)
	taskCmd.SetErr(&errBuf)
	if err := taskCmd.RunE(taskCmd, nil); err != nil {
		t.Fatalf("task: %v", err)
	}
	s := out.String()
	for _, want := range []string{"name:", "web", "nginx:alpine", "source files were not modified"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in %s", want, s)
		}
	}
}

func TestTask_UnknownTo(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte("services:\n  web:\n    image: nginx\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	taskFrom, taskTo, taskDir = "", "k8s", dir
	t.Cleanup(func() {
		taskFrom, taskTo, taskDir = "", "internal", "."
	})
	if err := taskCmd.RunE(taskCmd, nil); err == nil {
		t.Fatal("expected unknown environment error")
	}
}

func TestTask_ToHost(t *testing.T) {
	dir := t.TempDir()
	yml := `services:
  redis:
    image: redis:7
    ports: ["6379:6379"]
`
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	taskFrom, taskTo, taskDir = "compose", "host", dir
	t.Cleanup(func() {
		taskFrom, taskTo, taskDir = "", "internal", "."
		taskCmd.SetOut(nil)
	})
	taskCmd.SetOut(&out)
	if err := taskCmd.RunE(taskCmd, nil); err != nil {
		t.Fatalf("task --to host: %v", err)
	}
	if !strings.Contains(out.String(), "docker run") {
		t.Fatalf("expected docker run plan, got %s", out.String())
	}
}
