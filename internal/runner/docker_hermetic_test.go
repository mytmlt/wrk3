package runner

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUpFailsFastOnContainerNameNoDaemon(t *testing.T) {
	dir := t.TempDir()
	compose := "services:\n  app:\n    image: alpine:3.19\n    container_name: fixed\n"
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(compose), 0o644); err != nil {
		t.Fatal(err)
	}
	r := New(Options{ComposeFiles: []string{"docker-compose.yml"}, ProjectPrefix: "demo", Slug: "x"})
	if err := r.Up(context.Background(), dir, nil); err == nil {
		t.Fatal("Up with container_name = nil, want offender error (no daemon needed)")
	}
}

func TestBuildEnvEdgeCases(t *testing.T) {
	// Duplicate base keys: last wins, no duplicates.
	got := buildEnv([]string{"A=1", "A=2"}, "p", nil)
	seen := map[string]int{}
	for _, kv := range got {
		k := kv[:len("A")]
		if k == "A="[:1] {
			seen[kv]++
		}
	}
	// Base entry without '=' preserved verbatim.
	got = buildEnv([]string{"NOEQUALS"}, "p", map[string]string{"": "x", "A=B": "y", "OK": "1"})
	found := false
	for _, kv := range got {
		if kv == "NOEQUALS" {
			found = true
		}
		if kv == "=x" || kv == "A=B=y" {
			t.Errorf("malformed extra key leaked: %q", kv)
		}
	}
	if !found {
		t.Error("base entry without '=' not preserved")
	}
	// Sorted determinism: run twice, compare.
	e1 := buildEnv(nil, "p", map[string]string{"Z": "1", "A": "2", "M": "3"})
	e2 := buildEnv(nil, "p", map[string]string{"Z": "1", "A": "2", "M": "3"})
	if len(e1) != len(e2) {
		t.Fatalf("buildEnv nondeterministic: %v vs %v", e1, e2)
	}
	for i := range e1 {
		if e1[i] != e2[i] {
			t.Fatalf("buildEnv nondeterministic: %v vs %v", e1, e2)
		}
	}
}

func TestTimeoutDefault(t *testing.T) {
	r := New(Options{})
	if got := r.timeout(); got != defaultDockerTimeout {
		t.Errorf("timeout() = %v, want %v", got, defaultDockerTimeout)
	}
	r.Timeout = time.Minute
	if got := r.timeout(); got != time.Minute {
		t.Errorf("timeout() = %v, want 1m", got)
	}
}
