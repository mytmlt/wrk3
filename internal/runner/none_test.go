package runner

import (
	"context"
	"strings"
	"sync"
	"testing"
)

// buildEnvNoProject must drop COMPOSE_PROJECT_NAME from both the ambient
// base environment and explicit extras so none-runner entry commands
// invoking `docker compose` without -p never inherit a stale project.
func TestBuildEnvNoProjectStripsComposeProject(t *testing.T) {
	base := []string{"PATH=/bin", "COMPOSE_PROJECT_NAME=stale", "FOO=1"}
	extra := map[string]string{"BAR": "2", "COMPOSE_PROJECT_NAME": "explicit"}
	got := buildEnvNoProject(base, extra)
	for _, kv := range got {
		if k := kv; k == "COMPOSE_PROJECT_NAME" || strings.HasPrefix(kv, "COMPOSE_PROJECT_NAME=") {
			t.Fatalf("buildEnvNoProject kept %q in %v", kv, got)
		}
	}
	joined := strings.Join(got, "\n")
	for _, want := range []string{"PATH=/bin", "FOO=1", "BAR=2"} {
		if !strings.Contains(joined, want) {
			t.Errorf("buildEnvNoProject dropped %q from %v", want, got)
		}
	}
}

func TestNoneExecRejectsEmpty(t *testing.T) {
	r := NewNoneRunner()
	if err := r.Exec(context.Background(), "   ", []string{"go", "version"}, nil); err == nil {
		t.Error("Exec with blank worktree path = nil, want error")
	}
	if err := r.Exec(context.Background(), t.TempDir(), nil, nil); err == nil {
		t.Error("Exec with empty command = nil, want error")
	}
}

// None Exec must tee command output to the ctx sink like the compose
// runners do, so dashboard/CLI live output stays visible.
func TestNoneExecTeesSink(t *testing.T) {
	var mu sync.Mutex
	var lines []string
	ctx := WithOutput(context.Background(), func(line string) {
		mu.Lock()
		lines = append(lines, line)
		mu.Unlock()
	})
	if err := NewNoneRunner().Exec(ctx, t.TempDir(), []string{"go", "version"}, nil); err != nil {
		t.Fatalf("Exec(go version) = %v, want nil", err)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, l := range lines {
		if strings.Contains(l, "go version") {
			return
		}
	}
	t.Fatalf("sink got %v, want a line containing %q", lines, "go version")
}
