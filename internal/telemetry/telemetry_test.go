package telemetry

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getsentry/sentry-go"
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
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("telemetry:\n  enabled: true\n  prompted: true\n"), 0o600); err != nil {
		t.Fatalf("write prefs: %v", err)
	}
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
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte{}, 0o600); err != nil {
		t.Fatalf("write prefs: %v", err)
	}
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

func TestNewErrorEvent_ExceptionAndThread(t *testing.T) {
	event := newErrorEvent("status", errSentinel("something went wrong"))
	if event == nil {
		t.Fatal("newErrorEvent returned nil")
	}
	if !event.User.IsEmpty() {
		t.Errorf("User = %+v, want empty", event.User)
	}
	if event.ServerName != "" {
		t.Errorf("ServerName = %q, want empty", event.ServerName)
	}
	if len(event.Exception) != 1 {
		t.Fatalf("Exception len = %d, want 1", len(event.Exception))
	}
	if len(event.Threads) != 1 {
		t.Fatalf("Threads len = %d, want 1", len(event.Threads))
	}
	th := event.Threads[0]
	if th.Name != "main" {
		t.Errorf("thread Name = %q, want main", th.Name)
	}
	if th.ID != "0" {
		t.Errorf("thread ID = %q, want 0", th.ID)
	}
	if !th.Current {
		t.Error("thread Current = false, want true")
	}
	if !th.Crashed {
		t.Error("thread Crashed = false, want true")
	}
	st := event.Exception[0].Stacktrace
	if st == nil || len(st.Frames) == 0 {
		t.Fatal("exception stacktrace is empty")
	}
	if th.Stacktrace != st {
		t.Error("thread and exception must share the same stacktrace")
	}
	for i, f := range st.Frames {
		if f.Vars != nil {
			t.Errorf("frame %d Vars = %#v, want nil", i, f.Vars)
		}
		if f.ContextLine != "" {
			t.Errorf("frame %d ContextLine = %q, want empty", i, f.ContextLine)
		}
		if f.PreContext != nil {
			t.Errorf("frame %d PreContext = %#v, want nil", i, f.PreContext)
		}
		if f.PostContext != nil {
			t.Errorf("frame %d PostContext = %#v, want nil", i, f.PostContext)
		}
	}
}

func TestBeforeSend_ScrubsMessageAndFrames(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home dir")
	}
	dir := t.TempDir()
	t.Setenv("WRK3_CONFIG_HOME", dir)
	t.Setenv("WRK3_NO_TELEMETRY", "")
	if err := SavePrefs(true, true); err != nil {
		t.Fatalf("SavePrefs: %v", err)
	}

	event := sentry.NewEvent()
	event.Message = "failed at " + home + "/.config/wrk3.yaml"
	event.User = sentry.User{Username: "alice"}
	event.ServerName = "devbox"
	event.Exception = []sentry.Exception{{
		Type:  "pathError",
		Value: "open " + home + "/secret",
		Stacktrace: &sentry.Stacktrace{Frames: []sentry.Frame{{
			Filename: home + "/src/foo.go",
			AbsPath:  home + "/src/foo.go",
			Module:   "github.com/mytmlt/wrk3/internal/telemetry",
			Function: "ReportIfEnabled",
			Lineno:   49,
		}}},
	}}

	got := beforeSend(event, nil)
	if got == nil {
		t.Fatal("beforeSend dropped event")
	}
	if strings.Contains(got.Message, home) {
		t.Errorf("message still contains home dir: %q", got.Message)
	}
	if !strings.Contains(got.Message, "$HOME") {
		t.Errorf("message missing $HOME: %q", got.Message)
	}
	if !got.User.IsEmpty() {
		t.Errorf("User = %+v, want empty", got.User)
	}
	if got.ServerName != "" {
		t.Errorf("ServerName = %q, want empty", got.ServerName)
	}
	frame := got.Exception[0].Stacktrace.Frames[0]
	if strings.Contains(frame.Filename, home) || strings.Contains(frame.AbsPath, home) {
		t.Errorf("frame still contains home dir: filename=%q abs=%q", frame.Filename, frame.AbsPath)
	}
	if !strings.Contains(frame.Filename, "$HOME") {
		t.Errorf("frame filename missing $HOME: %q", frame.Filename)
	}
}

func TestBeforeSend_DropsWhenDisabledOrEmpty(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WRK3_CONFIG_HOME", dir)
	t.Setenv("WRK3_NO_TELEMETRY", "")
	if err := SavePrefs(false, true); err != nil {
		t.Fatalf("SavePrefs: %v", err)
	}
	event := sentry.NewEvent()
	event.Message = "something went wrong"
	if got := beforeSend(event, nil); got != nil {
		t.Error("beforeSend should drop when prefs are disabled")
	}

	if err := SavePrefs(true, true); err != nil {
		t.Fatalf("SavePrefs: %v", err)
	}
	event = sentry.NewEvent()
	event.Message = ""
	if got := beforeSend(event, nil); got != nil {
		t.Error("beforeSend should drop when message is empty")
	}

	t.Setenv("WRK3_NO_TELEMETRY", "1")
	event = sentry.NewEvent()
	event.Message = "something went wrong"
	if got := beforeSend(event, nil); got != nil {
		t.Error("beforeSend should drop when kill-switch is on")
	}
}

func TestErrorTypeName(t *testing.T) {
	base := errSentinel("something went wrong")
	once := fmt.Errorf("wrapped: %w", base)
	twice := fmt.Errorf("outer: %w", once)
	execErr := &exec.ExitError{}
	execWrapped := fmt.Errorf("command failed: %w", execErr)

	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "bare sentinel",
			err:  base,
			want: "telemetry.errSentinel",
		},
		{
			name: "wrapped once",
			err:  once,
			want: "telemetry.errSentinel",
		},
		{
			name: "wrapped multiple levels",
			err:  twice,
			want: "telemetry.errSentinel",
		},
		{
			name: "exec exit wrapped in message",
			err:  execWrapped,
			want: "*exec.ExitError",
		},
		{
			name: "nil error",
			err:  nil,
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := errorTypeName(tt.err); got != tt.want {
				t.Errorf("errorTypeName(%v) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}

func TestNewErrorEvent_DropsBindPermissionDenied(t *testing.T) {
	err := fmt.Errorf("proxy listen 127.0.0.1:80: listen tcp 127.0.0.1:80: bind: permission denied")
	if event := newErrorEvent("run", err); event != nil {
		t.Fatalf("newErrorEvent returned event for bind permission denied: %+v", event)
	}
	if event := newErrorEvent("run", fmt.Errorf("listen tcp :80: bind: access is denied")); event != nil {
		t.Fatalf("newErrorEvent returned event for bind access is denied: %+v", event)
	}
	if event := newErrorEvent("run", errSentinel("something went wrong")); event == nil {
		t.Fatal("newErrorEvent dropped a real error")
	}
}

func TestNewErrorEvent_UnwrappedType(t *testing.T) {
	err := fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", errSentinel("something went wrong")))
	event := newErrorEvent("status", err)
	if event == nil {
		t.Fatal("newErrorEvent returned nil")
	}
	if len(event.Exception) != 1 {
		t.Fatalf("Exception len = %d, want 1", len(event.Exception))
	}
	if got := event.Exception[0].Type; got != "telemetry.errSentinel" {
		t.Errorf("Exception[0].Type = %q, want telemetry.errSentinel", got)
	}
	et, ok := event.Extra["error_type"].(string)
	if !ok {
		t.Fatalf("Extra[error_type] = %T, want string", event.Extra["error_type"])
	}
	if et != "telemetry.errSentinel" {
		t.Errorf("Extra[error_type] = %q, want telemetry.errSentinel", et)
	}
	if event.Message != "outer: inner: something went wrong" {
		t.Errorf("Message = %q, want full wrapped text", event.Message)
	}
}
