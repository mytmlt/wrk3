package task

import (
	"fmt"
	"strings"
)

// Render projects t onto env as YAML. Source files are never written.
func Render(t Task, env Environment, extra []Note) ([]byte, []Note, error) {
	if err := t.Validate(); err != nil {
		return nil, nil, err
	}
	notes := t.NotesFor(env, extra)
	if err := blockError(env, notes); err != nil {
		return nil, notes, err
	}
	var (
		body []byte
		err  error
	)
	switch env {
	case EnvInternal:
		body, err = marshalYAML(t)
	case EnvCompose:
		body, err = renderCompose(t, false)
	case EnvPortainer:
		body, err = renderCompose(t, true)
	case EnvSwarm:
		body, err = renderSwarm(t)
	case EnvHost:
		body, err = renderHost(t)
	default:
		return nil, notes, fmt.Errorf("unknown environment %q (available environments: %v)", env, Available())
	}
	if err != nil {
		return nil, notes, err
	}
	return []byte(header(env, t) + string(body)), notes, nil
}

func cmdValue(parts []string, shell string) any {
	if strings.TrimSpace(shell) != "" {
		return shell
	}
	if len(parts) == 0 {
		return nil
	}
	return parts
}
