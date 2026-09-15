package runner

import (
	"context"
	"fmt"
)

// NomadRunner is a v1 stub: the Source x Runner matrix compiles,
// but remote Nomad execution is out of scope and returns
// "not implemented" from every method.
type NomadRunner struct{}

var _ Runner = NomadRunner{}

func nomadErr(op string) error {
	return fmt.Errorf("nomad runner %s: not implemented (v1 ships docker only)", op)
}

// Up implements Runner (stub).
func (NomadRunner) Up(_ context.Context, _ string, _ map[string]string) error {
	return nomadErr("up")
}

// Down implements Runner (stub).
func (NomadRunner) Down(_ context.Context, _ string, _ map[string]string) error {
	return nomadErr("down")
}

// Logs implements Runner (stub).
func (NomadRunner) Logs(_ context.Context, _ string, _ bool) (string, error) {
	return "", nomadErr("logs")
}

// Exec implements Runner (stub).
func (NomadRunner) Exec(_ context.Context, _ string, _ []string, _ map[string]string) error {
	return nomadErr("exec")
}

// Status implements Runner (stub).
func (NomadRunner) Status(_ context.Context, _ string) (Status, error) {
	return Status{State: StateUnknown}, nomadErr("status")
}
