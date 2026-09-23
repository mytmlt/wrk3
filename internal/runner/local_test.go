package runner

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLocalRunnerExecHostCommand(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skipf("sh not found: %v", err)
	}
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r := NewLocal(Options{Slug: "test"})
	env := map[string]string{"WRK3_TEST_PORT": "8123"}
	if err := r.Exec(ctx, dir, []string{"sh", "-c", "test \"$WRK3_TEST_PORT\" = 8123"}, env); err != nil {
		t.Fatalf("Exec sh: %v", err)
	}
}

func TestLocalRunnerExecWaitsForScript(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skipf("sh not found: %v", err)
	}
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r := NewLocal(Options{Slug: "exec-wait"})
	marker := filepath.Join(dir, "done")
	start := time.Now()
	if err := r.Exec(ctx, dir, []string{"sh", "-c", "sleep 0.3 && touch \"$WRK3_TEST_MARKER\""},
		map[string]string{"WRK3_TEST_MARKER": marker}); err != nil {
		t.Fatalf("Exec: %v", err)
	}
	elapsed := time.Since(start)
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("script-written marker must exist when Exec returns: %v", err)
	}
	if elapsed < 200*time.Millisecond {
		t.Errorf("Exec returned after %v; the script was not awaited to completion", elapsed)
	}
}

func TestLocalRunnerExecEmptyPath(t *testing.T) {
	ctx := context.Background()
	r := NewLocal(Options{Slug: "test"})
	if err := r.Exec(ctx, "", []string{"true"}, nil); err == nil {
		t.Error("Exec with empty path should fail")
	}
}

func TestLocalRunnerExecEmptyCmd(t *testing.T) {
	ctx := context.Background()
	r := NewLocal(Options{Slug: "test"})
	if err := r.Exec(ctx, t.TempDir(), nil, nil); err == nil {
		t.Error("Exec with nil cmd should fail")
	}
	if err := r.Exec(ctx, t.TempDir(), []string{""}, nil); err == nil {
		t.Error("Exec with empty cmd should fail")
	}
}

func TestLocalRunnerUpDownEmptyPath(t *testing.T) {
	ctx := context.Background()
	r := NewLocal(Options{Slug: "test"})
	if err := r.Up(ctx, "", nil); err == nil {
		t.Error("Up with empty path should fail")
	}
	if err := r.Down(ctx, "", nil); err == nil {
		t.Error("Down with empty path should fail")
	}
}

func TestLocalRunnerLogsEmptyPath(t *testing.T) {
	ctx := context.Background()
	r := NewLocal(Options{Slug: "test"})
	if _, err := r.Logs(ctx, "", false); err == nil {
		t.Error("Logs with empty path should fail")
	}
}

func TestLocalRunnerLogsEmpty(t *testing.T) {
	ctx := context.Background()
	r := NewLocal(Options{Slug: "test"})
	out, err := r.Logs(ctx, t.TempDir(), false)
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	if out != "" {
		t.Errorf("Logs = %q, want empty", out)
	}
}

func TestLocalRunnerStatusEmptyPath(t *testing.T) {
	ctx := context.Background()
	r := NewLocal(Options{Slug: "test"})
	if _, err := r.Status(ctx, ""); err == nil {
		t.Error("Status with empty path should fail")
	}
}

func TestLocalRunnerStatusStoppedDefault(t *testing.T) {
	ctx := context.Background()
	r := NewLocal(Options{Slug: "test"})
	st, err := r.Status(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.State != StateStopped {
		t.Errorf("Status = %v, want stopped", st.State)
	}
	if st.Running {
		t.Error("Running should be false by default")
	}
}

func TestLocalRunnerStatusReadsStateFile(t *testing.T) {
	dir := t.TempDir()
	wrkDir := filepath.Join(dir, "test-slug")
	if err := os.MkdirAll(wrkDir, 0o755); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(dir, ".wrk3-state.json")
	recs := []struct {
		Slug   string `json:"slug"`
		Status string `json:"status"`
	}{
		{Slug: "test-slug", Status: "running"},
	}
	raw, _ := json.Marshal(recs)
	if err := os.WriteFile(statePath, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	r := NewLocal(Options{Slug: "test-slug"})
	r.StatePath = statePath
	st, err := r.Status(ctx, wrkDir)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.State != StateRunning {
		t.Errorf("Status = %v, want running", st.State)
	}
	if !st.Running {
		t.Error("Running should be true when state file says running")
	}
}

func TestLocalRunnerStatusMissingStateFile(t *testing.T) {
	dir := t.TempDir()
	wrkDir := filepath.Join(dir, "test-slug")
	if err := os.MkdirAll(wrkDir, 0o755); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	r := NewLocal(Options{Slug: "test-slug"})
	r.StatePath = filepath.Join(dir, "nonexistent.json")
	st, err := r.Status(ctx, wrkDir)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.State != StateStopped {
		t.Errorf("Status = %v, want stopped (missing state file)", st.State)
	}
}

func TestLocalRunnerStatusNoStatePathFallback(t *testing.T) {
	ctx := context.Background()
	r := NewLocal(Options{Slug: "test"})
	st, err := r.Status(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.State != StateStopped {
		t.Errorf("Status = %v, want stopped", st.State)
	}
}

func TestLocalRunnerStatusStoppedRecord(t *testing.T) {
	dir := t.TempDir()
	wrkDir := filepath.Join(dir, "test-slug")
	if err := os.MkdirAll(wrkDir, 0o755); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(dir, ".wrk3-state.json")
	recs := []struct {
		Slug   string `json:"slug"`
		Status string `json:"status"`
	}{
		{Slug: "test-slug", Status: "stopped"},
	}
	raw, _ := json.Marshal(recs)
	if err := os.WriteFile(statePath, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	r := NewLocal(Options{Slug: "test-slug"})
	r.StatePath = statePath
	st, err := r.Status(ctx, wrkDir)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.State != StateStopped {
		t.Errorf("Status = %v, want stopped", st.State)
	}
	if st.Running {
		t.Error("Running should be false when state file says stopped")
	}
}

func TestLocalRunnerRegistry(t *testing.T) {
	r := NewLocal(Options{Slug: "test"})
	if r == nil {
		t.Fatal("NewLocal returned nil")
	}
	if _, ok := registry["local"]; !ok {
		t.Fatal("local runner not registered")
	}
	fn, err := Resolve("local")
	if err != nil {
		t.Fatalf("Resolve local: %v", err)
	}
	rn := fn(Options{Slug: "test"})
	if rn == nil {
		t.Fatal("factory returned nil")
	}
	if _, ok := rn.(*LocalRunner); !ok {
		t.Fatalf("factory returned %T, want *LocalRunner", rn)
	}
}

func TestLocalRunnerUpAndDownNoOp(t *testing.T) {
	ctx := context.Background()
	r := NewLocal(Options{Slug: "test"})
	if err := r.Up(ctx, t.TempDir(), nil); err != nil {
		t.Fatalf("Up: %v", err)
	}
	if err := r.Down(ctx, t.TempDir(), nil); err != nil {
		t.Fatalf("Down: %v", err)
	}
}

func TestLocalRunnerExecShowsErrorText(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skipf("sh not found: %v", err)
	}
	dir := t.TempDir()
	ctx := context.Background()
	r := NewLocal(Options{Slug: "test"})
	r.Timeout = 10 * time.Second
	err := r.Exec(ctx, dir, []string{"sh", "-c", "echo custom-message >&2; exit 2"}, nil)
	if err == nil {
		t.Fatal("Exec should fail on exit 2")
	}
	if !strings.Contains(err.Error(), "custom-message") {
		t.Errorf("error %q should contain custom-message", err)
	}
	if !strings.Contains(err.Error(), "exit status 2") {
		t.Errorf("error %q should contain exit status 2", err)
	}
}
