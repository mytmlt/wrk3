package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTaskCompose(t *testing.T, repo string) {
	t.Helper()
	body := `services:
  app:
    image: alpine:3.19
    command: sleep infinity
    environment:
      TOKEN: ${APP_TOKEN}
    ports:
      - "8000:80"
`
	if err := os.WriteFile(filepath.Join(repo, "docker-compose.yml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func chdirTaskRepo(t *testing.T) {
	t.Helper()
	repo := initMainTestRepo(t)
	writeTestConfig(t, repo)
	writeTaskCompose(t, repo)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	oldFile := fileFlag
	oldFmt := taskFormat
	oldSwarm := taskSwarm
	fileFlag = ""
	taskFormat = "yaml"
	taskSwarm = false
	t.Cleanup(func() {
		fileFlag = oldFile
		taskFormat = oldFmt
		taskSwarm = oldSwarm
	})
}

func TestCLITaskYAML(t *testing.T) {
	chdirTaskRepo(t)
	out, _, err := executeCmd("task")
	if err != nil {
		t.Fatalf("task: %v", err)
	}
	if !strings.Contains(out, "kind: compose") || !strings.Contains(out, "name: app") {
		t.Errorf("task yaml =\n%s", out)
	}
}

func TestCLITaskHostAndPortainer(t *testing.T) {
	chdirTaskRepo(t)
	out, _, err := executeCmd("task", "--format", "host")
	if err != nil {
		t.Fatalf("task --format host: %v", err)
	}
	if !strings.Contains(out, "dockerArgs:") && !strings.Contains(out, "image: alpine:3.19") {
		t.Errorf("host plan =\n%s", out)
	}
	out, _, err = executeCmd("task", "--format", "portainer")
	if err != nil {
		t.Fatalf("task --format portainer: %v", err)
	}
	if !strings.Contains(out, `"Name"`) || !strings.Contains(out, "StackFileContent") {
		t.Errorf("portainer =\n%s", out)
	}
	if !strings.Contains(out, "APP_TOKEN") {
		t.Errorf("portainer env interpolation missing APP_TOKEN:\n%s", out)
	}
	out, _, err = executeCmd("task", "--format", "swarm")
	if err != nil {
		t.Fatalf("task --format swarm: %v", err)
	}
	if !strings.Contains(out, "3.8") {
		t.Errorf("swarm =\n%s", out)
	}
}

func TestCLITaskUnknownFormat(t *testing.T) {
	chdirTaskRepo(t)
	_, _, err := executeCmd("task", "--format", "nomad")
	if err == nil || !strings.Contains(err.Error(), "unknown task format") {
		t.Fatalf("err = %v, want unknown task format", err)
	}
}
