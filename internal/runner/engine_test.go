package runner

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestEngineUnavailableErrorMessage(t *testing.T) {
	err := &EngineUnavailableError{
		Engine: "docker",
		Err:    errors.New("failed to connect to the docker API at unix:///tmp/docker.sock"),
	}
	got := err.Error()
	if !strings.Contains(got, "docker daemon is not running") {
		t.Errorf("Error() = %q, want daemon is not running", got)
	}
	if !strings.Contains(got, "start docker and retry") {
		t.Errorf("Error() = %q, want start docker and retry", got)
	}
	if !err.Expected() {
		t.Error("Expected() = false, want true")
	}
}

func TestEngineUnavailableErrorBinaryMissing(t *testing.T) {
	err := &EngineUnavailableError{
		Engine: "podman",
		Err:    errors.New(`exec: "podman": executable file not found in $PATH`),
	}
	got := err.Error()
	if !strings.Contains(got, "podman binary not found in PATH") {
		t.Errorf("Error() = %q, want binary not found", got)
	}
}

func TestIsEngineUnavailable(t *testing.T) {
	sentryMsg := `up "cursor/sentry-event-ingest-d0f5" setup "make dev.up": exec "sh -c make dev.up" (dir=$HOME/projects/superplane/superplane/.worktrees/cursor-sentry-event-ingest-d0f5 project=sp-cursor-sentry-event-ingest-d0f5): exit status 2: failed to connect to the docker API at unix://$HOME/.docker/run/docker.sock; check if the path is correct and if the daemon is running: dial unix $HOME/.docker/run/docker.sock: connect: no such file or directory
make: *** [dev.up] Error 1`
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "typed", err: &EngineUnavailableError{Engine: "docker"}, want: true},
		{name: "wrapped typed", err: errors.Join(&EngineUnavailableError{Engine: "docker"}), want: true},
		{name: "sentry docker api", err: errors.New(sentryMsg), want: true},
		{name: "classic daemon message", err: errors.New("Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?"), want: true},
		{name: "podman sock", err: errors.New("dial unix /run/podman/podman.sock: connect: no such file or directory"), want: true},
		{name: "compose conflict", err: errors.New("Conflict. The container name is already in use"), want: false},
		{name: "generic missing file", err: errors.New("open wrk3.yaml: no such file or directory"), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsEngineUnavailable(tt.err); got != tt.want {
				t.Errorf("IsEngineUnavailable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWrapEngineErr(t *testing.T) {
	inner := errors.New("failed to connect to the docker API at unix:///x.sock")
	got := wrapEngineErr("docker", inner)
	var eu *EngineUnavailableError
	if !errors.As(got, &eu) {
		t.Fatalf("wrapEngineErr type = %T, want *EngineUnavailableError", got)
	}
	if eu.Engine != "docker" {
		t.Errorf("Engine = %q, want docker", eu.Engine)
	}
	already := &EngineUnavailableError{Engine: "docker", Err: inner}
	if wrapEngineErr("docker", already) != already {
		t.Error("wrapEngineErr should keep an existing EngineUnavailableError")
	}
	other := errors.New("container_name conflict")
	if wrapEngineErr("docker", other) != other {
		t.Error("wrapEngineErr should leave unrelated errors unchanged")
	}
	if wrapEngineErr("docker", nil) != nil {
		t.Error("wrapEngineErr(nil) should be nil")
	}
}

func TestCheckEngineSkipsUnknown(t *testing.T) {
	if err := CheckEngine(context.Background(), "nomad"); err != nil {
		t.Errorf("CheckEngine(nomad) = %v, want nil", err)
	}
	if err := CheckEngine(context.Background(), ""); err != nil {
		t.Errorf("CheckEngine(empty) = %v, want nil", err)
	}
}

func TestCheckEngineMissingBinary(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	err := CheckEngine(context.Background(), "docker")
	if err == nil {
		t.Fatal("CheckEngine with empty PATH = nil, want error")
	}
	var eu *EngineUnavailableError
	if !errors.As(err, &eu) {
		t.Fatalf("CheckEngine error type = %T, want *EngineUnavailableError", err)
	}
	if !strings.Contains(err.Error(), "binary not found") {
		t.Errorf("CheckEngine error = %q, want binary not found", err)
	}
}

func TestCheckEngineWhenPresent(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker binary not found")
	}
	err := CheckEngine(context.Background(), "docker")
	if err == nil {
		return
	}
	if !IsEngineUnavailable(err) {
		t.Fatalf("CheckEngine docker present but failed with unexpected error: %v", err)
	}
}
