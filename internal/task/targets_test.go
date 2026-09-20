package task

import (
	"encoding/json"
	"strings"
	"testing"
)

const imageCompose = `name: demo
services:
  db:
    image: postgres:16-alpine
    restart: unless-stopped
  app:
    image: ghcr.io/example/app:1.2.3
    depends_on: [db]
    ports:
      - "8000:8080"
`

func TestSwarmRejectsBuildOnly(t *testing.T) {
	def, err := ParseCompose([]byte(sampleCompose))
	if err != nil {
		t.Fatal(err)
	}
	_, err = def.Swarm()
	if err == nil || !strings.Contains(err.Error(), "no image") {
		t.Fatalf("Swarm = %v, want no-image error", err)
	}
}

func TestSwarmStack(t *testing.T) {
	def, err := ParseCompose([]byte(imageCompose))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := def.Swarm()
	if err != nil {
		t.Fatalf("Swarm: %v", err)
	}
	out := string(raw)
	for _, want := range []string{"name: demo", "replicas: 1", "mode: replicated", "image: ghcr.io/example/app:1.2.3"} {
		if !strings.Contains(out, want) {
			t.Errorf("swarm output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "restart:") {
		t.Errorf("swarm output should drop restart (ignored by stacks):\n%s", out)
	}
}

func TestSwarmGlobalHasNoReplicas(t *testing.T) {
	def := &Definition{Services: map[string]*Service{
		"agent": {Image: "agent", Deploy: &Deploy{Mode: DeployModeGlobal}},
	}}
	raw, err := def.Swarm()
	if err != nil {
		t.Fatal(err)
	}
	out := string(raw)
	if !strings.Contains(out, "mode: global") {
		t.Errorf("missing global mode:\n%s", out)
	}
	if strings.Contains(out, "replicas:") {
		t.Errorf("global service must not set replicas:\n%s", out)
	}
}

func TestPortainerPayload(t *testing.T) {
	def, err := ParseCompose([]byte(imageCompose))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := def.Portainer()
	if err != nil {
		t.Fatalf("Portainer: %v", err)
	}
	var payload struct {
		Name             string `json:"Name"`
		StackFileContent string `json:"StackFileContent"`
		Env              []any  `json:"Env"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v\n%s", err, raw)
	}
	if payload.Name != "demo" {
		t.Errorf("Name = %q, want demo", payload.Name)
	}
	if !strings.Contains(payload.StackFileContent, "services:") {
		t.Errorf("StackFileContent missing services:\n%s", payload.StackFileContent)
	}
	if payload.Env == nil {
		t.Error("Env should be an empty array, not null")
	}
}

func TestPortainerDefaultName(t *testing.T) {
	def := &Definition{Services: map[string]*Service{"app": {Image: "app"}}}
	raw, err := def.Portainer()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"Name": "wrk3"`) {
		t.Errorf("default name missing:\n%s", raw)
	}
}

func TestRegistryResolve(t *testing.T) {
	for _, name := range []string{"compose", "swarm", "portainer", "machine"} {
		if _, err := ResolveTarget(name); err != nil {
			t.Errorf("ResolveTarget(%q): %v", name, err)
		}
	}
	_, err := ResolveTarget("nomad")
	if err == nil || !strings.Contains(err.Error(), "available targets") {
		t.Fatalf("ResolveTarget(nomad) = %v, want available-targets error", err)
	}
	if got := AvailableTargets(); len(got) != 4 {
		t.Errorf("AvailableTargets = %v, want 4", got)
	}
}
