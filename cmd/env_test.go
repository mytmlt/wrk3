package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mytmlt/wrk3/internal/ports"
)

func TestEnvPrint_WritesDotEnv(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	dir := t.TempDir()
	rec := ports.WorktreeRecord{
		Branch:  "feature-foo",
		Slug:    "feature-foo",
		AbsPath: dir,
		Index:   1,
		Ports:   map[string]int{"app": 8001},
	}
	if err := saveState(&resolved{cfg: cfg, stateP: cfg.StatePath()}, []ports.WorktreeRecord{rec}); err != nil {
		t.Fatalf("saveState = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("MY_SECRET=abc\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	oldPrint := envPrint
	envPrint = true
	defer func() { envPrint = oldPrint }()
	oldFile := fileFlag
	fileFlag = cfg.ConfigPath()
	defer func() { fileFlag = oldFile }()

	// findRecordIncludingMain reconciles against git; point src at the real
	// repo so the temp-dir record survives (git list failure degrades to
	// no adoption rather than dropping state).
	out, _, err := executeCmd("env", rec.Branch)
	if err != nil {
		t.Fatalf("env: %v", err)
	}
	if !strings.Contains(out, "MY_SECRET=abc") {
		t.Errorf("output missing user key: %q", out)
	}
	if !strings.Contains(out, "APP_PORT=8001") {
		t.Errorf("output missing managed key: %q", out)
	}
}

func TestEnvUnknownWorktree_Errors(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	oldFile := fileFlag
	fileFlag = cfg.ConfigPath()
	defer func() { fileFlag = oldFile }()

	oldPrint := envPrint
	envPrint = true
	defer func() { envPrint = oldPrint }()
	if _, _, err := executeCmd("env", "no-such-branch"); err == nil {
		t.Error("env unknown = nil, want error")
	}
}
