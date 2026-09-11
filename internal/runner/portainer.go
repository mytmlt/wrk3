package runner

import (
	"context"
	"fmt"
)

// PortainerRunner is a v1 stub: the Source x Runner matrix compiles,
// but remote Portainer execution is out of scope and returns
// "not implemented" from every method.
type PortainerRunner struct{}

var _ Runner = PortainerRunner{}

func portainerErr(op string) error {
	return fmt.Errorf("portainer runner %s: not implemented (v1 ships docker only)", op)
}

// Up implements Runner (stub).
func (PortainerRunner) Up(_ context.Context, _ string, _ map[string]string) error {
	return portainerErr("up")
}

// Down implements Runner (stub).
func (PortainerRunner) Down(_ context.Context, _ string, _ map[string]string) error {
	return portainerErr("down")
}

// Logs implements Runner (stub).
func (PortainerRunner) Logs(_ context.Context, _ string, _ bool) (string, error) {
	return "", portainerErr("logs")
}

// Exec implements Runner (stub).
func (PortainerRunner) Exec(_ context.Context, _ string, _ []string, _ map[string]string) error {
	return portainerErr("exec")
}

// Status implements Runner (stub).
func (PortainerRunner) Status(_ context.Context, _ string) (Status, error) {
	return Status{State: StateUnknown}, portainerErr("status")
}
