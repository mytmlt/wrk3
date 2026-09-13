package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestSkill_PrintsGuide(t *testing.T) {
	var buf bytes.Buffer
	skillCmd.SetOut(&buf)
	t.Cleanup(func() { skillCmd.SetOut(nil) })
	if err := skillCmd.RunE(skillCmd, nil); err != nil {
		t.Fatalf("skill RunE: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"wrk3.yaml",
		"worktreeBase",
		"composeFiles",
		"APP_PORT",
		"container_name",
		"wrk3 -f",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("skill output missing %q", want)
		}
	}
}

func TestSkill_RejectsArgs(t *testing.T) {
	if err := skillCmd.Args(skillCmd, []string{"extra"}); err == nil {
		t.Error("expected error for extra args, got nil")
	}
}
