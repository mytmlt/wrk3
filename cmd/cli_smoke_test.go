package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func executeCmd(args ...string) (string, string, error) {
	rootCmd.SetOut(new(bytes.Buffer))
	rootCmd.SetErr(new(bytes.Buffer))
	rootCmd.SetArgs(args)
	outBuf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	rootCmd.SetOut(outBuf)
	rootCmd.SetErr(errBuf)
	// Silence update notice during tests.
	old := Version
	Version = "dev"
	defer func() { Version = old }()
	err := rootCmd.Execute()
	return outBuf.String(), errBuf.String(), err
}

func TestCLIVersionSmoke(t *testing.T) {
	out, _, err := executeCmd("version")
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if out == "" || len(out) < 4 || out[:4] != "wrk3" {
		t.Errorf("version output = %q, want wrk3 prefix", out)
	}
}

func TestCLIMissingConfigErrors(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	oldFile := fileFlag
	fileFlag = ""
	defer func() { fileFlag = oldFile }()
	_, err := ResolveConfigPath()
	if err == nil {
		t.Error("ResolveConfigPath in empty dir = nil, want error")
	}
	if !IsUsageError(err) {
		t.Errorf("ResolveConfigPath err = %v, want usage error", err)
	}
}

func TestIsUsageError(t *testing.T) {
	if IsUsageError(nil) {
		t.Error("nil is not a usage error")
	}
	if !IsUsageError(errConfigNotFound) {
		t.Error("errConfigNotFound should be a usage error")
	}
	wrapped := fmt.Errorf("%w; or --project <name>", errConfigNotFound)
	if !IsUsageError(wrapped) {
		t.Error("wrapped errConfigNotFound should be a usage error")
	}
	if IsUsageError(fmt.Errorf("docker compose failed")) {
		t.Error("unrelated error should not be a usage error")
	}
}

func TestCLIStatusSmokeTempRepo(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	outBuf := new(bytes.Buffer)
	// status must not fail hard on a fresh repo with no worktrees.
	recs, err := recordsForDisplay(cfg)
	if err != nil {
		t.Fatalf("recordsForDisplay = %v", err)
	}
	_ = outBuf
	if len(recs) == 0 {
		t.Log("no records (main branch lookup offline-safe)")
	}
	_ = filepath.Join(repo, "wrk3.yaml")
}
