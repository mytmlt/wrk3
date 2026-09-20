package task

import (
	"encoding/json"
	"fmt"
)

// portainerTarget is the "portainer" environment: the JSON body of a
// Portainer stack-create call (POST /api/stacks).
type portainerTarget struct{}

func (portainerTarget) Name() string { return "portainer" }

func (portainerTarget) Render(d *Definition) ([]byte, error) { return d.Portainer() }

func init() { RegisterTarget(portainerTarget{}) }

// portainerPayload mirrors the fields Portainer's stack API accepts. The
// stack file itself is the compose document, so Portainer runs the same
// workload without any source changes.
type portainerPayload struct {
	Name             string         `json:"Name"`
	StackFileContent string         `json:"StackFileContent"`
	Env              []portainerEnv `json:"Env"`
}

type portainerEnv struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Portainer renders the Definition as a Portainer stack-create payload.
// The stack name defaults to the definition name, then "wrk3".
func (d *Definition) Portainer() ([]byte, error) {
	compose, err := d.Compose()
	if err != nil {
		return nil, fmt.Errorf("portainer target: %w", err)
	}
	name := ""
	if d != nil {
		name = d.Name
	}
	if name == "" {
		name = "wrk3"
	}
	payload := portainerPayload{
		Name:             name,
		StackFileContent: string(compose),
		Env:              []portainerEnv{},
	}
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("portainer target: %w", err)
	}
	return append(raw, '\n'), nil
}
