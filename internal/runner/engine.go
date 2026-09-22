package runner

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"
)

const defaultEngineCheckTimeout = 15 * time.Second

// EngineUnavailableError is a local environment failure: the container
// engine binary is missing or its daemon is not reachable. It is not a
// wrk3 bug.
type EngineUnavailableError struct {
	Engine string
	Err    error
}

func (e *EngineUnavailableError) Error() string {
	if e == nil {
		return "container engine is not running; start it and retry"
	}
	engine := e.engine()
	switch {
	case e.Err != nil && strings.Contains(strings.ToLower(e.Err.Error()), "not found"):
		return engine + " binary not found in PATH; install " + engine + " and retry"
	case e.Err != nil:
		return engine + " daemon is not running: " + strings.TrimSpace(e.Err.Error()) + "; start " + engine + " and retry"
	default:
		return engine + " daemon is not running; start " + engine + " and retry"
	}
}

func (e *EngineUnavailableError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (e *EngineUnavailableError) Expected() bool { return true }

func (e *EngineUnavailableError) engine() string {
	if e == nil || strings.TrimSpace(e.Engine) == "" {
		return "container"
	}
	return e.Engine
}

// CheckEngine pings docker or podman (`<engine> info`) so `up` can fail
// fast before entry.setup. Other runner types are skipped.
func CheckEngine(ctx context.Context, engine string) error {
	engine = strings.TrimSpace(engine)
	switch engine {
	case "docker", "podman":
	default:
		return nil
	}
	if _, err := exec.LookPath(engine); err != nil {
		return &EngineUnavailableError{Engine: engine, Err: err}
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, defaultEngineCheckTimeout)
	defer cancel()
	var stdout, stderr bytes.Buffer
	if err := runDockerCmd(timeoutCtx, engine, "", os.Environ(), &stdout, &stderr, "info"); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return &EngineUnavailableError{Engine: engine, Err: errors.New(detail)}
	}
	return nil
}

// IsEngineUnavailable reports whether err is (or wraps) a missing or
// unreachable container engine, including raw docker/podman messages
// from entry commands that talk to the daemon themselves.
func IsEngineUnavailable(err error) bool {
	if err == nil {
		return false
	}
	var eu *EngineUnavailableError
	if errors.As(err, &eu) {
		return true
	}
	return looksEngineUnavailable(err.Error())
}

func wrapEngineErr(engine string, err error) error {
	if err == nil {
		return nil
	}
	var eu *EngineUnavailableError
	if errors.As(err, &eu) {
		return err
	}
	if looksEngineUnavailable(err.Error()) {
		return &EngineUnavailableError{Engine: engine, Err: err}
	}
	return err
}

func looksEngineUnavailable(msg string) bool {
	m := strings.ToLower(msg)
	for _, frag := range []string{
		"failed to connect to the docker api",
		"cannot connect to the docker daemon",
		"cannot connect to podman",
		"is the docker daemon running",
		"docker daemon is not running",
		"podman daemon is not running",
		"daemon not running",
	} {
		if strings.Contains(m, frag) {
			return true
		}
	}
	if strings.Contains(m, "docker.sock") || strings.Contains(m, "podman.sock") {
		if strings.Contains(m, "no such file or directory") ||
			strings.Contains(m, "connection refused") ||
			strings.Contains(m, "connect:") {
			return true
		}
	}
	return false
}
