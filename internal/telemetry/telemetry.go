package telemetry

import (
	"errors"
	"fmt"
	"net"
	"os"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/getsentry/sentry-go"
)

// DefaultDSN is baked into every build (including plain `go build`).
// Overridden at release time via `-X .../telemetry.DSN=$SENTRY_DSN`
// and at runtime via WRK3_SENTRY_DSN. Forks can strip it by building
// with `-X .../telemetry.DSN=` (empty).
var DSN = "https://5453eb392d183be1161ff1da5450c98e@o4512095348916224.ingest.de.sentry.io/4512124896608336"

var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

func CheckDisabled() bool {
	v := strings.TrimSpace(os.Getenv("WRK3_NO_TELEMETRY"))
	if v == "" {
		return false
	}
	switch strings.ToLower(v) {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

func DSNConfigured() bool {
	return activeDSN() != ""
}

func activeDSN() string {
	if v := os.Getenv("WRK3_SENTRY_DSN"); v != "" {
		return v
	}
	return DSN
}

// IsPermissionDeniedListen reports whether err is a permission-denied
// listen error (e.g. binding a restricted port without CAP_NET_BIND_SERVICE
// or equivalent on Windows). It walks the error chain via errors.As and
// falls back to a case-insensitive substring check for wrapped net errors
// on platforms where the syscall constant differs.
func IsPermissionDeniedListen(err error) bool {
	if err == nil {
		return false
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Op == "listen" {
		var syscallErr os.SyscallError
		if errors.As(opErr, &syscallErr) {
			var errno syscall.Errno
			if errors.As(&syscallErr, &errno) {
				if errno == syscall.EACCES || errno == syscall.EPERM {
					return true
				}
			}
		}
	}
	// Fallback: case-insensitive substring match for Windows and other
	// platforms where the syscall error wrapping may differ.
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "permission denied")
}

func ReportIfEnabled(command string, err error) {
	if err == nil {
		return
	}
	if CheckDisabled() {
		return
	}
	enabled, _ := LoadPrefs()
	if !enabled {
		return
	}
	dsn := activeDSN()
	if dsn == "" {
		return
	}

	event := newErrorEvent(command, err)
	if event == nil {
		return
	}

	hub := sentry.CurrentHub()
	hub.CaptureEvent(event)
}

func newErrorEvent(command string, err error) *sentry.Event {
	if err == nil {
		return nil
	}

	if IsPermissionDeniedListen(err) {
		return nil
	}

	msg := Scrub(err)
	if msg == "" {
		return nil
	}

	et := fmt.Sprintf("%T", err)
	et = strings.TrimPrefix(et, "*")
	et = scrubString(et)

	st := sentry.ExtractStacktrace(err)
	if st == nil {
		st = sentry.NewStacktrace()
	}
	scrubStacktrace(st)

	event := sentry.NewEvent()
	event.Level = sentry.LevelError
	event.Message = msg
	event.Extra = map[string]interface{}{
		"error_type":   et,
		"message":      msg,
		"command":      command,
		"wrk3_version": Version,
		"goos":         runtime.GOOS,
		"goarch":       runtime.GOARCH,
	}
	event.Tags = map[string]string{
		"command":      command,
		"wrk3_version": Version,
		"goos":         runtime.GOOS,
		"goarch":       runtime.GOARCH,
	}
	event.User = sentry.User{}
	event.ServerName = ""
	event.Exception = []sentry.Exception{{
		Type:       et,
		Value:      msg,
		Stacktrace: st,
	}}
	event.Threads = []sentry.Thread{{
		ID:         "0",
		Name:       "main",
		Current:    true,
		Crashed:    true,
		Stacktrace: st,
	}}
	return event
}

func beforeSend(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
	if CheckDisabled() {
		return nil
	}
	enabled, _ := LoadPrefs()
	if !enabled {
		return nil
	}
	if event == nil {
		return nil
	}
	event.Message = scrubString(event.Message)
	if event.Message == "" {
		return nil
	}
	for i := range event.Exception {
		event.Exception[i].Type = scrubString(event.Exception[i].Type)
		event.Exception[i].Value = scrubString(event.Exception[i].Value)
		scrubStacktrace(event.Exception[i].Stacktrace)
	}
	if event.Extra != nil {
		if m, ok := event.Extra["message"].(string); ok {
			event.Extra["message"] = scrubString(m)
		}
	}
	for i := range event.Threads {
		scrubStacktrace(event.Threads[i].Stacktrace)
	}
	event.User = sentry.User{}
	event.ServerName = ""
	return event
}

func Init() {
	dsn := activeDSN()
	if dsn == "" {
		return
	}
	if err := sentry.Init(sentry.ClientOptions{
		Dsn:              dsn,
		TracesSampleRate: 0,
		SendDefaultPII:   false,
		ServerName:       "",
		BeforeSend:       beforeSend,
	}); err != nil {
		_ = err
	}
}

func Flush() {
	sentry.Flush(2 * time.Second)
}
