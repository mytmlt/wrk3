package telemetry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrefsPath_Default(t *testing.T) {
	path, err := prefsPath()
	if err != nil {
		t.Fatalf("prefsPath() error: %v", err)
	}
	if path == "" {
		t.Fatal("prefsPath() returned empty")
	}
	if !strings.HasSuffix(path, filepath.Join("wrk3", "preferences.yaml")) {
		t.Errorf("prefsPath() = %q, should end with wrk3/preferences.yaml", path)
	}
}

func TestPrefsPath_WRK3ConfigHome(t *testing.T) {
	t.Setenv("WRK3_CONFIG_HOME", "/custom/config")
	path, err := prefsPath()
	if err != nil {
		t.Fatalf("prefsPath() error: %v", err)
	}
	want := filepath.Join("/custom/config", "wrk3", "preferences.yaml")
	if path != want {
		t.Errorf("prefsPath() = %q, want %q", path, want)
	}
}

func TestPrefsPath_XDGConfigHome(t *testing.T) {
	t.Setenv("WRK3_CONFIG_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "/xdg/config")
	path, err := prefsPath()
	if err != nil {
		t.Fatalf("prefsPath() error: %v", err)
	}
	want := filepath.Join("/xdg/config", "wrk3", "preferences.yaml")
	if path != want {
		t.Errorf("prefsPath() = %q, want %q", path, want)
	}
}

func TestLoadPrefs_MissingFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WRK3_CONFIG_HOME", dir)
	enabled, prompted := LoadPrefs()
	if enabled {
		t.Error("LoadPrefs() enabled = true for missing file")
	}
	if prompted {
		t.Error("LoadPrefs() prompted = true for missing file")
	}
}

func TestLoadPrefs_ParsesFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WRK3_CONFIG_HOME", dir)
	path := filepath.Join(dir, "wrk3", "preferences.yaml")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte("telemetry:\n  enabled: true\n  prompted: true\n"), 0o600)
	enabled, prompted := LoadPrefs()
	if !enabled {
		t.Error("LoadPrefs() enabled = false, want true")
	}
	if !prompted {
		t.Error("LoadPrefs() prompted = false, want true")
	}
}

func TestLoadPrefs_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WRK3_CONFIG_HOME", dir)
	path := filepath.Join(dir, "wrk3", "preferences.yaml")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte{}, 0o600)
	enabled, prompted := LoadPrefs()
	if enabled {
		t.Error("LoadPrefs() enabled = true for empty file")
	}
	if prompted {
		t.Error("LoadPrefs() prompted = true for empty file")
	}
}

func TestSaveAndLoadPrefs(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WRK3_CONFIG_HOME", dir)
	if err := SavePrefs(true, true); err != nil {
		t.Fatalf("SavePrefs(true, true) error: %v", err)
	}
	enabled, prompted := LoadPrefs()
	if !enabled {
		t.Error("LoadPrefs() enabled = false after saving enabled=true")
	}
	if !prompted {
		t.Error("LoadPrefs() prompted = false after saving prompted=true")
	}
}

func TestSaveAndLoadPrefs_Disabled(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WRK3_CONFIG_HOME", dir)
	if err := SavePrefs(false, true); err != nil {
		t.Fatalf("SavePrefs(false, true) error: %v", err)
	}
	enabled, prompted := LoadPrefs()
	if enabled {
		t.Error("LoadPrefs() enabled = true after saving enabled=false")
	}
	if !prompted {
		t.Error("LoadPrefs() prompted = false after saving prompted=true")
	}
}

func TestCheckDisabled_Default(t *testing.T) {
	t.Setenv("WRK3_NO_TELEMETRY", "")
	if CheckDisabled() {
		t.Error("CheckDisabled() = true when env unset")
	}
}

func TestCheckDisabled_Enabled(t *testing.T) {
	t.Setenv("WRK3_NO_TELEMETRY", "1")
	if !CheckDisabled() {
		t.Error("CheckDisabled() = false when WRK3_NO_TELEMETRY=1")
	}
}

func TestCheckDisabled_Falsy(t *testing.T) {
	for _, v := range []string{"0", "false", "no", "off"} {
		t.Setenv("WRK3_NO_TELEMETRY", v)
		if CheckDisabled() {
			t.Errorf("CheckDisabled() = true when WRK3_NO_TELEMETRY=%q", v)
		}
	}
}

func TestDSNConfigured_Default(t *testing.T) {
	DSN = ""
	t.Setenv("WRK3_SENTRY_DSN", "")
	if DSNConfigured() {
		t.Error("DSNConfigured() = true when both vars are empty")
	}
}

func TestDSNConfigured_Var(t *testing.T) {
	DSN = "https://key@o0.ingest.sentry.io/project"
	if !DSNConfigured() {
		t.Error("DSNConfigured() = false when DSN var is set")
	}
}

func TestDSNConfigured_EnvOverride(t *testing.T) {
	DSN = ""
	t.Setenv("WRK3_SENTRY_DSN", "https://key@o0.ingest.sentry.io/project")
	if !DSNConfigured() {
		t.Error("DSNConfigured() = false when env var is set")
	}
}

func TestReportIfEnabled_DisabledByEnv(t *testing.T) {
	t.Setenv("WRK3_NO_TELEMETRY", "1")
	DSN = "https://key@o0.ingest.sentry.io/project"
	err := errSentinel("test error")
	ReportIfEnabled("test", err)
}

func TestReportIfEnabled_NoDSN(t *testing.T) {
	t.Setenv("WRK3_NO_TELEMETRY", "")
	DSN = ""
	err := errSentinel("test error")
	ReportIfEnabled("test", err)
}

func TestReportIfEnabled_NilError(t *testing.T) {
	ReportIfEnabled("test", nil)
}

func TestFlush_Noop(t *testing.T) {
	Flush()
}

func TestInit_NoDSN(t *testing.T) {
	DSN = ""
	Init()
}

func TestInit_WithDSN(t *testing.T) {
	DSN = "https://key@o0.ingest.sentry.io/project"
	Init()
}