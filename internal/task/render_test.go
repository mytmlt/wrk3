package task

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestToCompose_DropsContainerName(t *testing.T) {
	def := &Definition{
		Name: "demo",
		Services: []Service{{
			Name:          "app",
			Runtime:       RuntimeContainer,
			Image:         "alpine:3.19",
			ContainerName: "fixed",
			Command:       Cmd{Argv: []string{"sleep", "infinity"}},
			Ports:         []Port{{Published: "8000", Target: "80"}},
		}},
	}
	raw, err := ToCompose(def)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "container_name") {
		t.Fatalf("compose output still has container_name:\n%s", raw)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	svcs := asMap(doc["services"])
	app := asMap(svcs["app"])
	if asString(app["image"]) != "alpine:3.19" {
		t.Errorf("image = %v", app["image"])
	}
}

func TestToSwarm_OverlayAndDeploy(t *testing.T) {
	def := &Definition{
		Name: "demo",
		Services: []Service{{
			Name:        "app",
			Image:       "alpine:3.19",
			Restart:     "unless-stopped",
			Build:       &Build{Context: "."},
			DependsOn:   []string{"db"},
			NetworkMode: "host",
			Mounts:      []Mount{{Type: "bind", Source: "./data", Target: "/data"}},
		}, {
			Name:  "db",
			Image: "postgres:16-alpine",
		}},
		Networks: []Network{{Name: "frontend"}},
	}
	raw, err := ToSwarm(def)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, "version: \"3.8\"") && !strings.Contains(s, "version: '3.8'") && !strings.Contains(s, "3.8") {
		t.Errorf("missing version 3.8:\n%s", s)
	}
	if !strings.Contains(s, "overlay") {
		t.Errorf("missing overlay network:\n%s", s)
	}
	if strings.Contains(s, "network_mode") {
		t.Errorf("swarm output kept network_mode:\n%s", s)
	}
	if strings.Contains(s, "depends_on") {
		t.Errorf("swarm output kept depends_on:\n%s", s)
	}
	if !strings.Contains(s, "restart_policy") || !strings.Contains(s, "any") {
		t.Errorf("missing deploy.restart_policy:\n%s", s)
	}
	if strings.Contains(s, "build:") {
		t.Errorf("swarm output kept build when image is set:\n%s", s)
	}
	noted := false
	for _, n := range def.Notes {
		if strings.Contains(n, "network_mode") || strings.Contains(n, "depends_on") || strings.Contains(n, "bind mount") {
			noted = true
			break
		}
	}
	if !noted {
		t.Errorf("expected swarm conversion notes, got %v", def.Notes)
	}
}

func TestToSwarm_BuildOnlyGetsImageName(t *testing.T) {
	def := &Definition{
		Name: "demo",
		Services: []Service{{
			Name:  "app",
			Build: &Build{Context: "."},
		}},
	}
	raw, err := ToSwarm(def)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "demo_app:latest") {
		t.Errorf("expected derived image, got:\n%s", raw)
	}
}

func TestToPortainer_StackPayload(t *testing.T) {
	def := &Definition{
		Name: "demo",
		Services: []Service{{
			Name:  "app",
			Image: "alpine:3.19",
			Env:   map[string]string{"TOKEN": "${SECRET_TOKEN}"},
		}},
	}
	stack, err := ToPortainer(def, false)
	if err != nil {
		t.Fatal(err)
	}
	if stack.Name != "demo" || stack.Swarm {
		t.Errorf("stack = %+v", stack)
	}
	if !strings.Contains(stack.StackFileContent, "alpine:3.19") {
		t.Errorf("content = %s", stack.StackFileContent)
	}
	if len(stack.Env) != 1 || stack.Env[0].Name != "SECRET_TOKEN" {
		t.Errorf("Env = %+v", stack.Env)
	}
	swarm, err := ToPortainer(def, true)
	if err != nil {
		t.Fatal(err)
	}
	if !swarm.Swarm || !strings.Contains(swarm.StackFileContent, "3.8") {
		t.Errorf("swarm stack = %+v", swarm)
	}
}

func TestEncode_UnknownFormat(t *testing.T) {
	d := &Definition{Name: "x", Services: []Service{{Name: "a", Image: "alpine:3.19"}}}
	_, err := d.Encode(Format("nope"), false)
	if err == nil || !strings.Contains(err.Error(), "unknown task format") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseFormat(t *testing.T) {
	f, err := ParseFormat("Swarm")
	if err != nil || f != FormatSwarm {
		t.Fatalf("ParseFormat(Swarm) = %q, %v", f, err)
	}
	if _, err := ParseFormat("nomad"); err == nil {
		t.Fatal("expected error")
	}
}

func TestRoundTripCompose(t *testing.T) {
	dir := t.TempDir()
	writeCompose(t, dir, "docker-compose.yml", `
services:
  app:
    image: alpine:3.19
    command: sleep infinity
    ports:
      - "8000:80"
`)
	def, err := LoadCompose(dir, []string{"docker-compose.yml"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := ToCompose(def)
	if err != nil {
		t.Fatal(err)
	}
	writeCompose(t, dir, "generated.yml", string(raw))
	again, err := LoadCompose(dir, []string{"generated.yml"})
	if err != nil {
		t.Fatal(err)
	}
	app := again.ServiceByName("app")
	if app == nil || app.Image != "alpine:3.19" {
		t.Fatalf("roundtrip app = %+v", app)
	}
	if app.Command.Shell != "sleep infinity" {
		t.Errorf("command = %+v", app.Command)
	}
	if len(app.Ports) != 1 || app.Ports[0].Published != "8000" {
		t.Errorf("ports = %+v", app.Ports)
	}
}
