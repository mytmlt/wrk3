package task

import (
	"strings"
	"testing"
)

func TestToDockerCompose(t *testing.T) {
	yaml := []byte(`
version: "3.8"
services:
  web:
    image: nginx:latest
    ports:
      - "8080:80"
    environment:
      - FOO=bar
    restart: unless-stopped
    healthcheck:
      test: ["CMD-SHELL", "curl -f http://localhost || exit 1"]
      interval: 30s
      timeout: 10s
      retries: 3
  api:
    build: .
    command: ["./server"]
    ports:
      - "3000"
    depends_on:
      - web
    deploy:
      resources:
        limits:
          cpus: "1.0"
          memory: 256M
networks:
  default:
    driver: bridge
volumes:
  appdata:
`)

	task, err := ParseComposeBytes(yaml)
	if err != nil {
		t.Fatalf("ParseComposeBytes: %v", err)
	}

	out := task.ToDockerCompose()

	if !strings.Contains(out, "nginx:latest") {
		t.Error("output missing image")
	}
	if !strings.Contains(out, "8080:80") {
		t.Error("output missing port mapping")
	}
	if !strings.Contains(out, "FOO=bar") {
		t.Error("output missing env var")
	}
	if !strings.Contains(out, "services:") {
		t.Error("output missing services key")
	}
	if !strings.Contains(out, "networks:") {
		t.Error("output missing networks key")
	}
	if !strings.Contains(out, "volumes:") {
		t.Error("output missing volumes key")
	}

	// Round-trip: parse the output back
	rt, err := ParseComposeBytes([]byte(out))
	if err != nil {
		t.Fatalf("round-trip parse: %v", err)
	}
	if len(rt.Services) != 2 {
		t.Errorf("round-trip services = %d, want 2", len(rt.Services))
	}
	web := rt.ServiceByName("web")
	if web == nil || web.Restart != "unless-stopped" {
		t.Error("round-trip: web restart lost")
	}
	api := rt.ServiceByName("api")
	if api == nil || api.ResourceLimit == nil || api.ResourceLimit.CPUs != "1.0" {
		t.Error("round-trip: api resource limit lost")
	}
}

func TestToDockerSwarm(t *testing.T) {
	yaml := []byte(`
services:
  web:
    image: nginx:latest
    deploy:
      resources:
        limits:
          cpus: "2.0"
`)

	task, err := ParseComposeBytes(yaml)
	if err != nil {
		t.Fatalf("ParseComposeBytes: %v", err)
	}

	out := task.ToDockerSwarm()
	if !strings.Contains(out, "services:") {
		t.Error("swarm output missing services")
	}
	if !strings.Contains(out, "cpus: \"2.0\"") {
		t.Error("swarm output missing resource limits")
	}
}

func TestToPortainerStackJSON(t *testing.T) {
	yaml := []byte(`
services:
  web:
    image: nginx:latest
    ports:
      - "80"
`)

	task, err := ParseComposeBytes(yaml)
	if err != nil {
		t.Fatalf("ParseComposeBytes: %v", err)
	}

	out := task.ToPortainerStackJSON()
	if !strings.Contains(out, "StackFileContent") {
		t.Error("portainer output missing StackFileContent")
	}
	if !strings.Contains(out, "nginx:latest") {
		t.Error("portainer output missing service image")
	}
}

func TestToDirectRun(t *testing.T) {
	yaml := []byte(`
services:
  web:
    image: nginx:latest
    environment:
      - PORT=8080
    ports:
      - "8080:80"
  task:
    build: .
    command: ["./worker"]
    environment:
      - QUEUE=default
`)

	task, err := ParseComposeBytes(yaml)
	if err != nil {
		t.Fatalf("ParseComposeBytes: %v", err)
	}

	out := task.ToDirectRun()
	if !strings.Contains(out, "#!/bin/sh") {
		t.Error("direct-run output missing shebang")
	}
	if !strings.Contains(out, "export PORT=8080") {
		t.Error("direct-run output missing env export")
	}
	if !strings.Contains(out, "./worker") {
		t.Error("direct-run output missing command")
	}
	if !strings.Contains(out, "cannot run natively") {
		t.Error("direct-run output missing native warning for image-only service")
	}
}

func TestToDockerComposeEmptyTask(t *testing.T) {
	task := &Task{}
	out := task.ToDockerCompose()
	if !strings.Contains(out, "services:") {
		t.Error("empty task compose missing services key")
	}
}

func TestToDockerComposePrivileged(t *testing.T) {
	yaml := []byte(`
services:
  priv:
    image: alpine
    privileged: true
`)

	task, _ := ParseComposeBytes(yaml)
	out := task.ToDockerCompose()
	if !strings.Contains(out, "privileged: true") {
		t.Error("output missing privileged")
	}
}

func TestToDockerComposeExtraHosts(t *testing.T) {
	yaml := []byte(`
services:
  app:
    image: alpine
    extra_hosts:
      - "host.docker.internal:host-gateway"
`)

	task, _ := ParseComposeBytes(yaml)
	out := task.ToDockerCompose()
	if !strings.Contains(out, "host.docker.internal") {
		t.Error("output missing extra_hosts")
	}
}

func TestShellQuote(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"hello", "hello"},
		{"hello world", "'hello world'"},
		{"it's", "'it'\\''s'"},
		{"$PATH", "'$PATH'"},
	}
	for _, tt := range tests {
		got := shellQuote(tt.in)
		if got != tt.want {
			t.Errorf("shellQuote(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFormatters(t *testing.T) {
	t.Run("formatPort", func(t *testing.T) {
		cases := []struct {
			p    PortMapping
			want string
		}{
			{PortMapping{HostPort: 8080, Container: 80}, "8080:80"},
			{PortMapping{Container: 3000}, "3000"},
			{PortMapping{Host: "127.0.0.1", HostPort: 8080, Container: 80, Protocol: "udp"}, "127.0.0.1:8080:80/udp"},
			{PortMapping{HostPort: 8080, Container: 80, Protocol: "tcp"}, "8080:80"},
		}
		for _, c := range cases {
			got := formatPort(c.p)
			if got != c.want {
				t.Errorf("formatPort(%+v) = %q, want %q", c.p, got, c.want)
			}
		}
	})

	t.Run("formatVolume", func(t *testing.T) {
		cases := []struct {
			v    VolumeMapping
			want string
		}{
			{VolumeMapping{Source: "pgdata", Target: "/var/lib/postgresql/data"}, "pgdata:/var/lib/postgresql/data"},
			{VolumeMapping{Target: "/tmp"}, "/tmp"},
			{VolumeMapping{Source: "./data", Target: "/app/data", ReadOnly: true}, "./data:/app/data:ro"},
		}
		for _, c := range cases {
			got := formatVolume(c.v)
			if got != c.want {
				t.Errorf("formatVolume(%+v) = %q, want %q", c.v, got, c.want)
			}
		}
	})
}
