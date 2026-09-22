package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLocalUpDownEmptyPath(t *testing.T) {
	r := NewLocal()
	ctx := context.Background()
	if err := r.Up(ctx, "", nil); err == nil || !strings.Contains(err.Error(), "empty worktree path") {
		t.Fatalf("Up empty path = %v, want empty worktree path", err)
	}
	if err := r.Down(ctx, "  ", nil); err == nil || !strings.Contains(err.Error(), "empty worktree path") {
		t.Fatalf("Down empty path = %v, want empty worktree path", err)
	}
}

func TestLocalUpDownNoop(t *testing.T) {
	r := NewLocal()
	dir := t.TempDir()
	ctx := context.Background()
	if err := r.Up(ctx, dir, map[string]string{"APP_PORT": "8001"}); err != nil {
		t.Fatalf("Up = %v", err)
	}
	if err := r.Down(ctx, dir, nil); err != nil {
		t.Fatalf("Down = %v", err)
	}
}

func TestLocalStatusUnknown(t *testing.T) {
	r := NewLocal()
	st, err := r.Status(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("Status = %v", err)
	}
	if st.State != StateUnknown || st.Running {
		t.Fatalf("Status = %+v, want unknown and not running", st)
	}
	if _, err := r.Status(context.Background(), ""); err == nil {
		t.Fatal("Status empty path = nil, want error")
	}
}

func TestLocalLogs(t *testing.T) {
	r := NewLocal()
	_, err := r.Logs(context.Background(), t.TempDir(), false)
	if err == nil || !strings.Contains(err.Error(), "entry.logs") {
		t.Fatalf("Logs = %v, want entry.logs hint", err)
	}
	if _, err := r.Logs(context.Background(), "", false); err == nil {
		t.Fatal("Logs empty path = nil, want error")
	}
}

func TestLocalExec(t *testing.T) {
	r := NewLocal()
	dir := t.TempDir()
	marker := filepath.Join(dir, "ok")
	cmd := []string{"sh", "-c", `test "$APP_PORT" = "8001" && touch "$1"`, "sh", marker}
	if err := r.Exec(context.Background(), dir, cmd, map[string]string{"APP_PORT": "8001"}); err != nil {
		t.Fatalf("Exec = %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("marker not written: %v", err)
	}
}

func TestLocalExecEmpty(t *testing.T) {
	r := NewLocal()
	dir := t.TempDir()
	if err := r.Exec(context.Background(), "", []string{"true"}, nil); err == nil {
		t.Fatal("Exec empty path = nil, want error")
	}
	if err := r.Exec(context.Background(), dir, nil, nil); err == nil {
		t.Fatal("Exec empty cmd = nil, want error")
	}
	if err := r.Exec(context.Background(), dir, []string{"  "}, nil); err == nil {
		t.Fatal("Exec blank cmd = nil, want error")
	}
}

func TestLocalTimeoutOverride(t *testing.T) {
	r := NewLocal()
	if got := r.timeout(); got != defaultLocalTimeout {
		t.Errorf("timeout() = %v, want %v", got, defaultLocalTimeout)
	}
	r.Timeout = time.Minute
	if got := r.timeout(); got != time.Minute {
		t.Errorf("timeout() = %v, want 1m", got)
	}
}

func TestMergeEnvNoComposeProject(t *testing.T) {
	got := mergeEnv([]string{"PATH=/bin", "APP_PORT=1"}, map[string]string{"APP_PORT": "8001", "WEB_PORT": "3000"})
	env := map[string]string{}
	for _, kv := range got {
		i := strings.IndexByte(kv, '=')
		env[kv[:i]] = kv[i+1:]
	}
	if env["APP_PORT"] != "8001" || env["WEB_PORT"] != "3000" || env["PATH"] != "/bin" {
		t.Fatalf("mergeEnv = %v", env)
	}
	if _, ok := env["COMPOSE_PROJECT_NAME"]; ok {
		t.Fatalf("mergeEnv set COMPOSE_PROJECT_NAME: %v", env)
	}
}
