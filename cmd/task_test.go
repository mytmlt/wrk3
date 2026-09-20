package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTaskCompose(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "docker-compose.yml")
	body := `name: cli-demo
services:
  db:
    image: postgres:16-alpine
  app:
    image: ghcr.io/example/app:1.0
    depends_on: [db]
    ports:
      - "8000:8080"
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// resetTaskFlags clears the sticky task flags between command executions.
func resetTaskFlags() {
	taskRenderTo = "compose"
	taskRenderFrom = "compose"
	taskRenderCompose = nil
	taskRenderName = ""
}

func TestTaskRenderTargets(t *testing.T) {
	path := writeTaskCompose(t)
	cases := []struct {
		to   string
		want []string
	}{
		{"compose", []string{"services:", "postgres:16-alpine"}},
		{"swarm", []string{"deploy:", "mode: replicated"}},
		{"portainer", []string{`"Name": "cli-demo"`, "StackFileContent"}},
		{"machine", []string{"docker run", "docker rm -f"}},
	}
	for _, c := range cases {
		t.Run(c.to, func(t *testing.T) {
			resetTaskFlags()
			out, _, err := executeCmd("task", "render", "--compose", path, "--to", c.to)
			if err != nil {
				t.Fatalf("task render --to %s: %v", c.to, err)
			}
			for _, want := range c.want {
				if !strings.Contains(out, want) {
					t.Errorf("output missing %q:\n%s", want, out)
				}
			}
		})
	}
}

func TestTaskRenderNameOverride(t *testing.T) {
	resetTaskFlags()
	path := writeTaskCompose(t)
	out, _, err := executeCmd("task", "render", "--compose", path, "--name", "renamed")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "name: renamed") {
		t.Errorf("name override missing:\n%s", out)
	}
}

func TestTaskRenderFromMachine(t *testing.T) {
	resetTaskFlags()
	dir := t.TempDir()
	script := filepath.Join(dir, "run.sh")
	body := `#!/bin/sh
docker network create backend
docker run -d --name db -e POSTGRES_PASSWORD=secret -v db-data:/var/lib/postgresql/data postgres:16-alpine
docker run -d --name app --network backend -p 8000:8080 myapp:1.0
`
	if err := os.WriteFile(script, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _, err := executeCmd("task", "render", "--from", "machine", "--compose", script, "--to", "compose")
	if err != nil {
		t.Fatalf("task render --from machine: %v", err)
	}
	for _, want := range []string{"services:", "db-data:", "backend:", "myapp:1.0"} {
		if !strings.Contains(out, want) {
			t.Errorf("compose output missing %q:\n%s", want, out)
		}
	}
}

func TestTaskRenderFromMachineNeedsCompose(t *testing.T) {
	resetTaskFlags()
	_, _, err := executeCmd("task", "render", "--from", "machine")
	if err == nil || !strings.Contains(err.Error(), "requires --compose") {
		t.Fatalf("err = %v, want requires-compose error", err)
	}
}

func TestTaskRenderUnknownTarget(t *testing.T) {
	resetTaskFlags()
	path := writeTaskCompose(t)
	_, _, err := executeCmd("task", "render", "--compose", path, "--to", "nomad")
	if err == nil || !strings.Contains(err.Error(), "available targets") {
		t.Fatalf("err = %v, want available-targets error", err)
	}
}
