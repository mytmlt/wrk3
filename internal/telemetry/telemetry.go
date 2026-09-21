package telemetry

import (
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

func DSNConfigured() bool {
	return activeDSN() != ""
}

func activeDSN() string {
	if v := os.Getenv("WRK3_SENTRY_DSN"); v != "" {
		return v
	}
	return DSN
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

	msg := Scrub(err)
	if msg == "" {
		return
	}

	et := fmt.Sprintf("%T", err)
	et = strings.TrimPrefix(et, "*")

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

	hub := sentry.CurrentHub()
	hub.CaptureEvent(event)
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
		BeforeSend: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
			if CheckDisabled() {
				return nil
			}
			enabled, _ := LoadPrefs()
			if !enabled {
				return nil
			}
			if event.Message == "" {
				return nil
			}
			return event
		},
	}); err != nil {
		_ = err
	}
}

func Flush() {
	sentry.Flush(2 * time.Second)
}
