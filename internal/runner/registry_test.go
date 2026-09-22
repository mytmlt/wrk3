package runner

import (
	"strings"
	"testing"
)

func TestAvailableIsDockerLocalPodman(t *testing.T) {
	got := Available()
	if len(got) != 3 || got[0] != "docker" || got[1] != "local" || got[2] != "podman" {
		t.Fatalf("Available() = %v, want [docker local podman]", got)
	}
}

func TestResolveDocker(t *testing.T) {
	f, err := Resolve("docker")
	if err != nil {
		t.Fatalf("Resolve(docker): %v", err)
	}
	if f == nil {
		t.Fatal("Resolve(docker) returned nil factory")
	}
	if r := f(Options{}); r == nil {
		t.Fatal("docker factory returned nil Runner")
	}
	if r := f(Options{ProjectPrefix: "demo", Slug: "feat-x"}); r == nil {
		t.Fatal("docker factory with options returned nil Runner")
	}
}

func TestResolvePodman(t *testing.T) {
	f, err := Resolve("podman")
	if err != nil {
		t.Fatalf("Resolve(podman): %v", err)
	}
	if f == nil {
		t.Fatal("Resolve(podman) returned nil factory")
	}
	if r := f(Options{}); r == nil {
		t.Fatal("podman factory returned nil Runner")
	}
	if r := f(Options{ProjectPrefix: "demo", Slug: "feat-x"}); r == nil {
		t.Fatal("podman factory with options returned nil Runner")
	}
}

func TestResolveLocal(t *testing.T) {
	f, err := Resolve("local")
	if err != nil {
		t.Fatalf("Resolve(local): %v", err)
	}
	if f == nil {
		t.Fatal("Resolve(local) returned nil factory")
	}
	if r := f(Options{}); r == nil {
		t.Fatal("local factory returned nil Runner")
	}
}

func TestResolveUnknownListsOptions(t *testing.T) {
	_, err := Resolve("portainer")
	if err == nil {
		t.Fatal("Resolve(portainer) should fail (ships docker, local, and podman only)")
	}
	msg := err.Error()
	if !strings.Contains(msg, `"portainer"`) {
		t.Errorf("error %q should name the unknown type", msg)
	}
	if !strings.Contains(msg, "docker") || !strings.Contains(msg, "local") || !strings.Contains(msg, "podman") {
		t.Errorf("error %q should list available runners", msg)
	}
}
