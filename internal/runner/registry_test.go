package runner

import (
	"strings"
	"testing"
)

func TestAvailableIsDockerOnly(t *testing.T) {
	got := Available()
	if len(got) != 1 || got[0] != "docker" {
		t.Fatalf("Available() = %v, want [docker]", got)
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

func TestResolveUnknownListsOptions(t *testing.T) {
	_, err := Resolve("portainer")
	if err == nil {
		t.Fatal("Resolve(portainer) should fail (v1 ships docker only)")
	}
	msg := err.Error()
	if !strings.Contains(msg, `"portainer"`) {
		t.Errorf("error %q should name the unknown type", msg)
	}
	if !strings.Contains(msg, "docker") {
		t.Errorf("error %q should list available runners", msg)
	}
}
