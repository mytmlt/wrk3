package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLocalRunner_UpDownNoop(t *testing.T) {
	r := NewLocal()
	dir := t.TempDir()
	if err := r.Up(context.Background(), dir, nil); err != nil {
		t.Fatalf("Up: %v", err)
	}
	if err := r.Down(context.Background(), dir, nil); err != nil {
		t.Fatalf("Down: %v", err)
	}
}

func TestLocalRunner_EmptyPathErrors(t *testing.T) {
	r := NewLocal()
	ctx := context.Background()
	if err := r.Up(ctx, "", nil); err == nil {
		t.Fatal("Up empty path = nil, want error")
	}
	if err := r.Down(ctx, "  ", nil); err == nil {
		t.Fatal("Down blank path = nil, want error")
	}
	if _, err := r.Logs(ctx, "", false); err == nil {
		t.Fatal("Logs empty path = nil, want error")
	}
	if err := r.Exec(ctx, "", []string{"true"}, nil); err == nil {
		t.Fatal("Exec empty path = nil, want error")
	}
	if _, err := r.Status(ctx, ""); err == nil {
		t.Fatal("Status empty path = nil, want error")
	}
}

func TestLocalRunner_LogsRequiresEntry(t *testing.T) {
	r := NewLocal()
	_, err := r.Logs(context.Background(), t.TempDir(), false)
	if err == nil {
		t.Fatal("Logs = nil, want error")
	}
	if !strings.Contains(err.Error(), "entry.logs") {
		t.Errorf("error %q should mention entry.logs", err.Error())
	}
}

func TestLocalRunner_StatusUnknown(t *testing.T) {
	r := NewLocal()
	st, err := r.Status(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.State != StateUnknown || st.Running {
		t.Errorf("Status = %+v, want unknown and not running", st)
	}
}

func TestLocalRunner_Exec(t *testing.T) {
	r := NewLocal()
	dir := t.TempDir()
	marker := filepath.Join(dir, "ok")
	env := map[string]string{"WRK3_TEST_MARK": "yes"}
	if err := r.Exec(context.Background(), dir, []string{"sh", "-c", "test \"$WRK3_TEST_MARK\" = yes && touch ok"}, env); err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("marker missing: %v", err)
	}
}

func TestLocalRunner_ExecEmptyCommand(t *testing.T) {
	r := NewLocal()
	if err := r.Exec(context.Background(), t.TempDir(), nil, nil); err == nil {
		t.Fatal("Exec empty cmd = nil, want error")
	}
}

func TestLocalRunner_TimeoutDefault(t *testing.T) {
	r := NewLocal()
	if got := r.timeout(); got != defaultLocalTimeout {
		t.Errorf("timeout() = %v, want %v", got, defaultLocalTimeout)
	}
	r.Timeout = time.Minute
	if got := r.timeout(); got != time.Minute {
		t.Errorf("timeout() = %v, want 1m", got)
	}
}

func TestMergeEnv_NoComposeProject(t *testing.T) {
	got := mergeEnv([]string{"A=1"}, map[string]string{"B": "2"})
	for _, kv := range got {
		if strings.HasPrefix(kv, "COMPOSE_PROJECT_NAME=") {
			t.Fatalf("mergeEnv forced COMPOSE_PROJECT_NAME: %v", got)
		}
	}
}
