//go:build e2e

package e2e

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// TestMain builds the wrk3 binary once for the whole e2e package.
// The binary lands at <repoRoot>/bin/wrk3 (absolute path resolved from
// runtime.Caller so the suite is cwd-independent).
func TestMain(m *testing.M) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		log.Fatal("e2e: runtime.Caller failed")
	}
	root, err := filepath.Abs(filepath.Join(filepath.Dir(file), "..", ".."))
	if err != nil {
		log.Fatalf("e2e: resolve repo root: %v", err)
	}
	out := filepath.Join(root, "bin", "wrk3")
	cmd := exec.Command("go", "build", "-o", out, ".")
	cmd.Dir = root
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Fatalf("e2e: build wrk3 binary: %v", err)
	}
	binWrk3 = out
	os.Exit(m.Run())
}
