package cmd

import (
	"testing"

	"github.com/mytmlt/wrk3/internal/ports"
	"github.com/mytmlt/wrk3/internal/runner"
)

func TestApplyLiveStatuses_RunningWins(t *testing.T) {
	recs := []ports.WorktreeRecord{
		{Branch: "a", Slug: "a", AbsPath: "/tmp/a", Status: ports.StatusStopped},
		{Branch: "b", Slug: "b", AbsPath: "/tmp/b", Status: ports.StatusFailed},
		{Branch: "c", Slug: "c", AbsPath: "/tmp/c", Status: ports.StatusSettingUp},
		{Branch: "d", Slug: "d", AbsPath: "/tmp/d", Status: ports.StatusRunning},
	}
	live := map[string]runner.Status{
		"a": {State: runner.StateRunning, Running: true},
		"b": {State: runner.StateRunning, Running: true},
		"c": {State: runner.StateStopped},
		"d": {State: runner.StateStopped},
	}
	updated, changed := applyLiveStatuses(recs, live)
	byBranch := map[string]string{}
	for _, rec := range updated {
		byBranch[rec.Branch] = rec.Status
	}
	if byBranch["a"] != ports.StatusRunning {
		t.Errorf("stopped + live running must persist running, got %q", byBranch["a"])
	}
	if byBranch["b"] != ports.StatusRunning {
		t.Errorf("failed + live running must persist running, got %q", byBranch["b"])
	}
	if byBranch["c"] != ports.StatusSettingUp {
		t.Errorf("setting up + live stopped must stay (in-progress), got %q", byBranch["c"])
	}
	if byBranch["d"] != ports.StatusStopped {
		t.Errorf("running + live stopped must persist stopped, got %q", byBranch["d"])
	}
	if len(changed) != 3 {
		t.Errorf("changed = %v, want [a b d]", changed)
	}
}

func TestApplyLiveStatuses_UnknownNeverPersists(t *testing.T) {
	recs := []ports.WorktreeRecord{
		{Branch: "a", Slug: "a", AbsPath: "/tmp/a", Status: ports.StatusRunning},
	}
	live := map[string]runner.Status{
		"a": {State: runner.StateUnknown},
	}
	updated, changed := applyLiveStatuses(recs, live)
	if updated[0].Status != ports.StatusRunning || len(changed) != 0 {
		t.Errorf("unknown probe must not change state: %+v %v", updated, changed)
	}
	// Missing probe entries leave state alone too.
	updated, changed = applyLiveStatuses(recs, nil)
	if updated[0].Status != ports.StatusRunning || len(changed) != 0 {
		t.Errorf("missing probe must not change state: %+v %v", updated, changed)
	}
}
