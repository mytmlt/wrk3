package ports

import (
	"errors"
	"testing"
)

var errTest = errors.New("probe failed")

func TestShouldPersistLive(t *testing.T) {
	cases := []struct {
		stored, live string
		want         bool
	}{
		// Running always wins, even over transitional/terminal states.
		{StatusStopped, StatusRunning, true},
		{StatusFailed, StatusRunning, true},
		{StatusSettingUp, StatusRunning, true},
		{StatusStopping, StatusRunning, true},
		{StatusRunning, StatusRunning, false},
		{"", StatusRunning, true},
		// Stopped clears running/failed but never clobbers in-progress work.
		{StatusRunning, StatusStopped, true},
		{StatusFailed, StatusStopped, true},
		{StatusSettingUp, StatusStopped, false},
		{StatusStopping, StatusStopped, false},
		{StatusStopped, StatusStopped, false},
		// Unknown never persists (daemon unreachable must not wipe state).
		{StatusRunning, "unknown", false},
		{StatusStopped, "unknown", false},
		{StatusRunning, "", false},
	}
	for _, c := range cases {
		if got := ShouldPersistLive(c.stored, c.live); got != c.want {
			t.Errorf("ShouldPersistLive(%q, %q) = %v, want %v", c.stored, c.live, got, c.want)
		}
	}
}

func TestResolveDisplayStatus(t *testing.T) {
	if got := ResolveDisplayStatus(StatusFailed, StatusRunning, nil); got != StatusRunning {
		t.Errorf("live running must win over failed, got %q", got)
	}
	if got := ResolveDisplayStatus(StatusSettingUp, StatusRunning, nil); got != StatusRunning {
		t.Errorf("live running must win over setting up, got %q", got)
	}
	if got := ResolveDisplayStatus(StatusSettingUp, StatusStopped, nil); got != StatusSettingUp {
		t.Errorf("setting up must win over stopped probe, got %q", got)
	}
	if got := ResolveDisplayStatus(StatusStopped, StatusStopped, nil); got != StatusStopped {
		t.Errorf("stopped probe, got %q", got)
	}
	if got := ResolveDisplayStatus(StatusStopped, "", errTest); got != StatusStopped {
		t.Errorf("probe error must fall back to stored, got %q", got)
	}
	if got := ResolveDisplayStatus("", "", errTest); got != "unknown" {
		t.Errorf("probe error without stored must be unknown, got %q", got)
	}
	if got := ResolveDisplayStatus(StatusRunning, "unknown", nil); got != StatusRunning {
		t.Errorf("unknown probe must keep stored running, got %q", got)
	}
	if got := ResolveDisplayStatus(StatusStopped, "unknown", nil); got != StatusStopped {
		t.Errorf("unknown probe must keep stored stopped, got %q", got)
	}
	if got := ResolveDisplayStatus("", "unknown", nil); got != StatusStopped {
		t.Errorf("unknown probe without stored must be stopped, got %q", got)
	}
}
