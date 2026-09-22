package task

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeCompose(t *testing.T, dir, name, body string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadCompose_ParsesServices(t *testing.T) {
	dir := t.TempDir()
	writeCompose(t, dir, "docker-compose.yml", `
name: demo
services:
  db:
    image: postgres:16-alpine
    environment:
      POSTGRES_PASSWORD: secret
    volumes:
      - pgdata:/var/lib/postgresql/data
    ports:
      - "5432:5432"
  app:
    build:
      context: .
      dockerfile: Dockerfile
    command: ["npm", "start"]
    environment:
      - NODE_ENV=development
      - PORT=8000
    env_file: .env
    ports:
      - target: 8000
        published: 8000
        protocol: tcp
    volumes:
      - .:/app
      - type: bind
        source: ./certs
        target: /certs
        read_only: true
    depends_on:
      db:
        condition: service_healthy
    working_dir: /app
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8000/health"]
      interval: 10s
      retries: 3
    networks: [frontend]
volumes:
  pgdata:
networks:
  frontend:
`)
	def, err := LoadCompose(dir, []string{"docker-compose.yml"})
	if err != nil {
		t.Fatalf("LoadCompose: %v", err)
	}
	if def.Name != "demo" {
		t.Errorf("Name = %q, want demo", def.Name)
	}
	if def.Source.Kind != SourceCompose || len(def.Source.Files) != 1 {
		t.Errorf("Source = %+v", def.Source)
	}
	if len(def.Services) != 2 {
		t.Fatalf("services = %d, want 2", len(def.Services))
	}
	app := def.ServiceByName("app")
	if app == nil {
		t.Fatal("missing app")
	}
	if app.Build == nil || app.Build.Context != "." || app.Build.Dockerfile != "Dockerfile" {
		t.Errorf("app.Build = %+v", app.Build)
	}
	if len(app.Command.Argv) != 2 || app.Command.Argv[0] != "npm" {
		t.Errorf("app.Command = %+v", app.Command)
	}
	if app.Env["NODE_ENV"] != "development" || app.Env["PORT"] != "8000" {
		t.Errorf("app.Env = %v", app.Env)
	}
	if len(app.Ports) != 1 || app.Ports[0].Published != "8000" || app.Ports[0].Target != "8000" {
		t.Errorf("app.Ports = %+v", app.Ports)
	}
	if len(app.Mounts) != 2 {
		t.Errorf("app.Mounts = %+v", app.Mounts)
	}
	if len(app.DependsOn) != 1 || app.DependsOn[0] != "db" {
		t.Errorf("app.DependsOn = %v", app.DependsOn)
	}
	if app.Healthcheck == nil || app.Healthcheck.Retries != 3 {
		t.Errorf("app.Healthcheck = %+v", app.Healthcheck)
	}
	db := def.ServiceByName("db")
	if db == nil || db.Image != "postgres:16-alpine" {
		t.Errorf("db = %+v", db)
	}
	if len(def.Volumes) != 1 || def.Volumes[0].Name != "pgdata" {
		t.Errorf("Volumes = %+v", def.Volumes)
	}
	if len(def.Networks) != 1 || def.Networks[0].Name != "frontend" {
		t.Errorf("Networks = %+v", def.Networks)
	}
}

func TestLoadCompose_MergesOverlayFiles(t *testing.T) {
	dir := t.TempDir()
	writeCompose(t, dir, "docker-compose.yml", `
services:
  app:
    image: alpine:3.19
    environment:
      FOO: one
      KEEP: yes
    ports:
      - "8000:80"
`)
	writeCompose(t, dir, "docker-compose.override.yml", `
services:
  app:
    environment:
      FOO: two
    command: sleep infinity
  worker:
    image: alpine:3.19
    command: ["echo", "hi"]
`)
	def, err := LoadCompose(dir, []string{"docker-compose.yml", "docker-compose.override.yml"})
	if err != nil {
		t.Fatalf("LoadCompose: %v", err)
	}
	app := def.ServiceByName("app")
	if app == nil {
		t.Fatal("missing app")
	}
	if app.Env["FOO"] != "two" || app.Env["KEEP"] != "yes" {
		t.Errorf("merged env = %v", app.Env)
	}
	if app.Command.Shell != "sleep infinity" {
		t.Errorf("command = %+v", app.Command)
	}
	if def.ServiceByName("worker") == nil {
		t.Fatal("missing worker from overlay")
	}
}

func TestLoadCompose_DiscoversDefaultFile(t *testing.T) {
	dir := t.TempDir()
	writeCompose(t, dir, "compose.yaml", `
services:
  web:
    image: nginx:alpine
`)
	def, err := LoadCompose(dir, nil)
	if err != nil {
		t.Fatalf("LoadCompose: %v", err)
	}
	if def.ServiceByName("web") == nil {
		t.Fatal("missing web")
	}
	if len(def.Source.Files) != 1 || def.Source.Files[0] != "compose.yaml" {
		t.Errorf("files = %v", def.Source.Files)
	}
}

func TestLoadCompose_MissingFile(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadCompose(dir, []string{"docker-compose.yml"})
	if err == nil || !strings.Contains(err.Error(), "read compose file") {
		t.Fatalf("err = %v, want read compose file", err)
	}
}

func TestLoadCompose_NoServices(t *testing.T) {
	dir := t.TempDir()
	writeCompose(t, dir, "docker-compose.yml", "networks:\n  n:\n")
	_, err := LoadCompose(dir, []string{"docker-compose.yml"})
	if err == nil || !strings.Contains(err.Error(), "no services") {
		t.Fatalf("err = %v, want no services", err)
	}
}

func TestLoadCompose_EmptyDir(t *testing.T) {
	_, err := LoadCompose(t.TempDir(), nil)
	if err == nil || !strings.Contains(err.Error(), "no compose file found") {
		t.Fatalf("err = %v, want no compose file found", err)
	}
}

func TestParsePortString(t *testing.T) {
	cases := []struct {
		in   string
		want Port
	}{
		{"80", Port{Target: "80"}},
		{"8000:80", Port{Published: "8000", Target: "80"}},
		{"127.0.0.1:8000:80", Port{HostIP: "127.0.0.1", Published: "8000", Target: "80"}},
		{"8000:80/udp", Port{Published: "8000", Target: "80", Protocol: "udp"}},
	}
	for _, tc := range cases {
		got := parsePortString(tc.in)
		if got != tc.want {
			t.Errorf("parsePortString(%q) = %+v, want %+v", tc.in, got, tc.want)
		}
	}
}
