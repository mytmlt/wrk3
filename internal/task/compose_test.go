package task

import (
	"strings"
	"testing"
)

func TestParseComposeBytes(t *testing.T) {
	yaml := []byte(`
version: "3.8"
services:
  web:
    image: nginx:latest
    ports:
      - "8080:80"
    environment:
      - FOO=bar
      - BAZ=qux
    restart: unless-stopped
  api:
    build: .
    command: ["./server", "--port=3000"]
    ports:
      - "3000:3000"
    depends_on:
      - db
  db:
    image: postgres:16
    environment:
      POSTGRES_PASSWORD: secret
    volumes:
      - pgdata:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U postgres"]
      interval: 10s
      timeout: 5s
      retries: 5
networks:
  default:
    driver: bridge
volumes:
  pgdata:
`)

	task, err := ParseComposeBytes(yaml)
	if err != nil {
		t.Fatalf("ParseComposeBytes: %v", err)
	}

	if got := len(task.Services); got != 3 {
		t.Fatalf("expected 3 services, got %d", got)
	}

	web := task.ServiceByName("web")
	if web == nil {
		t.Fatal("service web not found")
	}
	if web.Image != "nginx:latest" {
		t.Errorf("web image = %q, want nginx:latest", web.Image)
	}
	if web.Restart != "unless-stopped" {
		t.Errorf("web restart = %q, want unless-stopped", web.Restart)
	}
	if len(web.Ports) != 1 {
		t.Errorf("web ports = %d, want 1", len(web.Ports))
	} else {
		if web.Ports[0].HostPort != 8080 {
			t.Errorf("web hostPort = %d, want 8080", web.Ports[0].HostPort)
		}
		if web.Ports[0].Container != 80 {
			t.Errorf("web containerPort = %d, want 80", web.Ports[0].Container)
		}
	}
	if len(web.Environment) != 2 {
		t.Errorf("web env = %d, want 2", len(web.Environment))
	}

	api := task.ServiceByName("api")
	if api == nil {
		t.Fatal("service api not found")
	}
	if api.Build != "." {
		t.Errorf("api build = %q, want .", api.Build)
	}
	if len(api.Command) != 2 {
		t.Errorf("api command = %d, want 2", len(api.Command))
	}
	if len(api.DependsOn) != 1 || api.DependsOn[0] != "db" {
		t.Errorf("api depends_on = %v, want [db]", api.DependsOn)
	}

	db := task.ServiceByName("db")
	if db == nil {
		t.Fatal("service db not found")
	}
	if db.Health == nil {
		t.Fatal("db health check missing")
	}
	if db.Health.Retries != 5 {
		t.Errorf("db health retries = %d, want 5", db.Health.Retries)
	}
	if len(db.Volumes) != 1 {
		t.Errorf("db volumes = %d, want 1", len(db.Volumes))
	} else if db.Volumes[0].Source != "pgdata" {
		t.Errorf("db volume source = %q, want pgdata", db.Volumes[0].Source)
	}

	if len(task.Networks) != 1 {
		t.Errorf("networks = %d, want 1", len(task.Networks))
	}
	if len(task.Volumes) != 1 {
		t.Errorf("volumes = %d, want 1", len(task.Volumes))
	}
}

func TestParseComposePorts(t *testing.T) {
	tests := []struct {
		spec     string
		hostPort int
		cont     int
		proto    string
		host     string
	}{
		{"8080:80", 8080, 80, "tcp", ""},
		{"80", 0, 80, "tcp", ""},
		{"8080:80/udp", 8080, 80, "udp", ""},
		{"127.0.0.1:8080:80", 8080, 80, "tcp", "127.0.0.1"},
		{"3000", 0, 3000, "tcp", ""},
	}
	for _, tt := range tests {
		pm := parsePort(tt.spec)
		if pm.HostPort != tt.hostPort {
			t.Errorf("parsePort(%q) HostPort = %d, want %d", tt.spec, pm.HostPort, tt.hostPort)
		}
		if pm.Container != tt.cont {
			t.Errorf("parsePort(%q) Container = %d, want %d", tt.spec, pm.Container, tt.cont)
		}
		if pm.Protocol != tt.proto {
			t.Errorf("parsePort(%q) Protocol = %q, want %q", tt.spec, pm.Protocol, tt.proto)
		}
		if pm.Host != tt.host {
			t.Errorf("parsePort(%q) Host = %q, want %q", tt.spec, pm.Host, tt.host)
		}
	}
}

func TestParseComposeVolumes(t *testing.T) {
	tests := []struct {
		spec     string
		source   string
		target   string
		readOnly bool
	}{
		{"pgdata:/var/lib/postgresql/data", "pgdata", "/var/lib/postgresql/data", false},
		{"/var/lib/postgresql/data", "", "/var/lib/postgresql/data", false},
		{"./data:/app/data:ro", "./data", "/app/data", true},
	}
	for _, tt := range tests {
		vm := parseVolume(tt.spec)
		if vm.Source != tt.source {
			t.Errorf("parseVolume(%q) Source = %q, want %q", tt.spec, vm.Source, tt.source)
		}
		if vm.Target != tt.target {
			t.Errorf("parseVolume(%q) Target = %q, want %q", tt.spec, vm.Target, tt.target)
		}
		if vm.ReadOnly != tt.readOnly {
			t.Errorf("parseVolume(%q) ReadOnly = %v, want %v", tt.spec, vm.ReadOnly, tt.readOnly)
		}
	}
}

func TestParseComposeEnv(t *testing.T) {
	yaml := []byte(`
services:
  app:
    image: alpine
    environment:
      - KEY=value
      - PLAINKEY
`)

	task, err := ParseComposeBytes(yaml)
	if err != nil {
		t.Fatalf("ParseComposeBytes: %v", err)
	}
	app := task.ServiceByName("app")
	if app == nil {
		t.Fatal("service app not found")
	}
	if len(app.Environment) != 2 {
		t.Fatalf("expected 2 env entries, got %d", len(app.Environment))
	}
	if app.Environment[0].Key != "KEY" || app.Environment[0].Value != "value" {
		t.Errorf("env[0] = %s=%s, want KEY=value", app.Environment[0].Key, app.Environment[0].Value)
	}
	if app.Environment[1].Key != "PLAINKEY" || app.Environment[1].Value != "" {
		t.Errorf("env[1] = %s=%s, want PLAINKEY=", app.Environment[1].Key, app.Environment[1].Value)
	}
}

func TestParseComposeDeployResources(t *testing.T) {
	yaml := []byte(`
services:
  app:
    image: alpine
    deploy:
      resources:
        limits:
          cpus: "0.5"
          memory: 512M
`)

	task, err := ParseComposeBytes(yaml)
	if err != nil {
		t.Fatalf("ParseComposeBytes: %v", err)
	}
	app := task.ServiceByName("app")
	if app == nil {
		t.Fatal("service app not found")
	}
	if app.ResourceLimit == nil {
		t.Fatal("resource limit missing")
	}
	if app.ResourceLimit.CPUs != "0.5" {
		t.Errorf("cpus = %q, want 0.5", app.ResourceLimit.CPUs)
	}
	if app.ResourceLimit.Memory != "512M" {
		t.Errorf("memory = %q, want 512M", app.ResourceLimit.Memory)
	}
}

func TestParseComposeNetworks(t *testing.T) {
	yaml := []byte(`
services:
  app:
    image: alpine
    networks:
      - backend
      - frontend
networks:
  backend:
    driver: bridge
  frontend:
    internal: true
`)

	task, err := ParseComposeBytes(yaml)
	if err != nil {
		t.Fatalf("ParseComposeBytes: %v", err)
	}
	app := task.ServiceByName("app")
	if app == nil {
		t.Fatal("service app not found")
	}
	if len(app.Networks) != 2 {
		t.Fatalf("expected 2 networks, got %d", len(app.Networks))
	}
	if app.Networks[0].Name != "backend" {
		t.Errorf("network[0] = %q, want backend", app.Networks[0].Name)
	}

	if len(task.Networks) != 2 {
		t.Errorf("task networks = %d, want 2", len(task.Networks))
	}
}

func TestMerge(t *testing.T) {
	base := []byte(`
services:
  web:
    image: nginx:latest
    ports:
      - "80"
  api:
    image: node:18
`)
	overlay := []byte(`
services:
  web:
    ports:
      - "8080:80"
  db:
    image: postgres:16
`)

	baseTask, err := ParseComposeBytes(base)
	if err != nil {
		t.Fatalf("ParseComposeBytes base: %v", err)
	}
	overlayTask, err := ParseComposeBytes(overlay)
	if err != nil {
		t.Fatalf("ParseComposeBytes overlay: %v", err)
	}

	baseTask.Merge(overlayTask)

	if len(baseTask.Services) != 3 {
		t.Fatalf("merged services = %d, want 3", len(baseTask.Services))
	}

	web := baseTask.ServiceByName("web")
	if web == nil {
		t.Fatal("service web not found after merge")
	}
	if web.Image != "nginx:latest" {
		t.Errorf("web image after merge = %q, want nginx:latest", web.Image)
	}
	if len(web.Ports) != 1 {
		t.Errorf("web ports after merge = %d, want 1 (overlay wins)", len(web.Ports))
	} else if web.Ports[0].HostPort != 8080 {
		t.Errorf("web hostPort after merge = %d, want 8080", web.Ports[0].HostPort)
	}

	api := baseTask.ServiceByName("api")
	if api == nil {
		t.Fatal("service api not found after merge")
	}

	db := baseTask.ServiceByName("db")
	if db == nil {
		t.Fatal("service db not found after merge")
	}
}

func TestMergeNil(t *testing.T) {
	base := []byte(`
services:
  web:
    image: nginx:latest
`)
	task, _ := ParseComposeBytes(base)
	task.Merge(nil)
	if len(task.Services) != 1 {
		t.Errorf("Merge(nil) changed services count: %d", len(task.Services))
	}
}

func TestServiceByNameNotFound(t *testing.T) {
	task := &Task{}
	if svc := task.ServiceByName("missing"); svc != nil {
		t.Error("expected nil for unknown service")
	}
}

func TestParseComposeBytesInvalidYAML(t *testing.T) {
	_, err := ParseComposeBytes([]byte(`services: ["bad`))
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestParseComposeDirNoFiles(t *testing.T) {
	dir := t.TempDir()
	_, err := ParseComposeDir(dir)
	if err == nil {
		t.Error("expected error for empty dir")
	}
	if !strings.Contains(err.Error(), "no compose files found") {
		t.Errorf("unexpected error: %v", err)
	}
}
