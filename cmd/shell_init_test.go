package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestShellInit_AllShellsMentionProtocol(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		src, err := shellInitScript(shell)
		if err != nil {
			t.Fatalf("shellInitScript(%q): %v", shell, err)
		}
		if !strings.Contains(src, directiveCDFileEnv) {
			t.Errorf("%s wrapper does not set %s", shell, directiveCDFileEnv)
		}
	}
}

func TestShellInit_BashZshShareScript(t *testing.T) {
	bash, err := shellInitScript("bash")
	if err != nil {
		t.Fatal(err)
	}
	zsh, err := shellInitScript("zsh")
	if err != nil {
		t.Fatal(err)
	}
	if bash != zsh {
		t.Error("bash and zsh wrappers differ; they share one implementation")
	}
	for _, want := range []string{"command wrk3", "mktemp", `cd --`} {
		if !strings.Contains(bash, want) {
			t.Errorf("bash/zsh wrapper missing %q", want)
		}
	}
}

func TestShellInit_FishWrapper(t *testing.T) {
	src, err := shellInitScript("fish")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"function wrk3", "command wrk3", `cd --`} {
		if !strings.Contains(src, want) {
			t.Errorf("fish wrapper missing %q", want)
		}
	}
}

func TestShellInit_PowershellWrapper(t *testing.T) {
	src, err := shellInitScript("powershell")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"function wrk3", "Set-Location", "-LiteralPath", "$env:" + directiveCDFileEnv} {
		if !strings.Contains(src, want) {
			t.Errorf("powershell wrapper missing %q", want)
		}
	}
	// `exit` inside a function would kill the host shell; the wrapper
	// must propagate the status via LASTEXITCODE instead.
	if strings.Contains(src, "\n    exit ") || strings.Contains(src, "\n  exit ") {
		t.Error("powershell wrapper must not call exit (kills the host shell)")
	}
}

func TestShellInit_UnknownShellErrors(t *testing.T) {
	if _, err := shellInitScript("nushell"); err == nil {
		t.Error("shellInitScript(nushell) = nil, want error")
	}
	if _, _, err := executeCmd("shell-init", "bogus"); err == nil {
		t.Error("shell-init bogus = nil, want error")
	}
}

func TestShellInit_BashSyntaxChecks(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not on PATH")
	}
	src, err := shellInitScript("bash")
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.CreateTemp(t.TempDir(), "wrk3-init-*.bash")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if _, err := f.WriteString(src); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(bash, "-n", f.Name()).CombinedOutput(); err != nil {
		t.Errorf("bash -n: %v: %s", err, out)
	}
}

func TestShellInit_CLIOutputsWrapper(t *testing.T) {
	out, _, err := executeCmd("shell-init", "bash")
	if err != nil {
		t.Fatalf("shell-init bash: %v", err)
	}
	if !strings.Contains(out, "wrk3()") {
		t.Errorf("shell-init bash output missing wrk3() function, got:\n%s", out)
	}
}

// TestShellInit_BashWrapperCds sources the wrapper with a stub wrk3 on
// PATH and asserts the calling shell actually changes directory — the
// whole point of the integration. The target contains spaces.
func TestShellInit_BashWrapperCds(t *testing.T) {
	// POSIX-shell e2e: temp paths are Windows-style (C:\...) under
	// git-bash and break sourcing/cd. Windows coverage comes from the
	// powershell wrapper plus the OS-independent directive test.
	if runtime.GOOS == "windows" {
		t.Skip("bash wrapper e2e is POSIX-only")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not on PATH")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "work tree with spaces")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	stub := "#!/bin/sh\nprintf '%s' \"$STUB_TARGET\" > \"$WRK3_DIRECTIVE_CD_FILE\"\n"
	stubPath := filepath.Join(bin, "wrk3")
	if err := os.WriteFile(stubPath, []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	src, err := shellInitScript("bash")
	if err != nil {
		t.Fatal(err)
	}
	initPath := filepath.Join(dir, "init.bash")
	if err := os.WriteFile(initPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	start := filepath.Join(dir, "start")
	if err := os.MkdirAll(start, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "source " + initPath + " && cd " + start + " && wrk3 checkout feature-foo && pwd"
	cmd := exec.Command(bash, "-c", script)
	cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "STUB_TARGET="+target)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("wrapper run: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != target {
		t.Errorf("pwd after wrapper checkout = %q, want %q", got, target)
	}
}
