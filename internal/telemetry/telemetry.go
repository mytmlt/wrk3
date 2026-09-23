package telemetry

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
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

// NonReportable marks an error as an expected, user-actionable failure
// (e.g. refusing to remove a dirty worktree without --force). It is
// still returned to the caller and shown to the user, but ReportIfEnabled
// never captures it as a Sentry exception.
type NonReportable interface {
	// NonReportable reports whether this error opts out of telemetry.
	NonReportable() bool
}

// nonReportable reports whether err (or anything it wraps, via the whole
// unwrap chain) opts out of telemetry reporting.
func nonReportable(err error) bool {
	var nr NonReportable
	return errors.As(err, &nr) && nr.NonReportable()
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

// ReportIfEnabled reports err to Sentry when telemetry is enabled, the
// kill-switch is off, and a DSN is configured. Errors that implement
// NonReportable are skipped: they are expected, user-actionable failures
// (still surfaced to the user) rather than bugs.
func ReportIfEnabled(command string, err error) {
	if err == nil {
		return
	}
	if nonReportable(err) {
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
	msg := Scrub(err)
	if msg == "" {
		return nil
	}

	et := errorTypeName(err)
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

func errorTypeName(err error) string {
	if err == nil {
		return ""
	}
	for cur := err; ; {
		name := fmt.Sprintf("%T", cur)
		if !isGenericWrap(name) {
			return name
		}
		next := errors.Unwrap(cur)
		if next != nil {
			cur = next
			continue
		}
		if uw, ok := cur.(interface{ Unwrap() []error }); ok {
			if errs := uw.Unwrap(); len(errs) > 0 {
				cur = errs[0]
				continue
			}
		}
		return name
	}
}

func isGenericWrap(name string) bool {
	switch name {
	case "*fmt.wrapError", "*fmt.wrapErrors", "*errors.joinError":
		return true
	}
	return false
}
