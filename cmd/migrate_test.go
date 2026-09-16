package cmd

import (
	"path/filepath"
	"testing"

	"github.com/mytmlt/wrk3/internal/ports"
)

// Legacy managed index 0 collides with main at ports.base: migration must
// bump it to index 1 (base+step) and recordsWithMain must then succeed
// with main at base.
func TestMigrateLegacyZeroToBasePlusStep(t *testing.T) {
	r, repo := reconcileFixture(t)
	wt := filepath.Join(repo, ".worktrees", "legacy")
	gitWorktreeAdd(t, repo, wt, "legacy")
	recs := []ports.WorktreeRecord{{
		Branch: "legacy", Slug: "legacy", AbsPath: wt,
		Index: 0, Ports: map[string]int{"app": 8000},
		Status: ports.StatusStopped, ComposeProject: "demo-legacy",
	}}
	updated, moved, _, err := migrateLegacyMainCollisions(r, recs)
	if err != nil {
		t.Fatal(err)
	}
	if len(moved) != 1 || moved[0] != "legacy" {
		t.Fatalf("moved = %v, want [legacy]", moved)
	}
	if updated[0].Index != 1 || updated[0].Ports["app"] != 8100 {
		t.Fatalf("rec = %+v, want index 1 app 8100", updated[0])
	}
	all, err := recordsWithMain(r, updated)
	if err != nil {
		t.Fatalf("recordsWithMain after migrate = %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("all = %+v, want managed + main", all)
	}
	var main *ports.WorktreeRecord
	for i := range all {
		if all[i].Index == mainWorktreeIndex {
			main = &all[i]
		}
	}
	if main == nil || main.Ports["app"] != 8000 {
		t.Errorf("main = %+v, want index 0 app 8000", main)
	}
}

// Two colliding records must land on distinct fresh slots, never renumbering
// healthy ones.
func TestMigrateLegacyZeroChainShift(t *testing.T) {
	r, repo := reconcileFixture(t)
	wt1 := filepath.Join(repo, ".worktrees", "a-legacy")
	wt2 := filepath.Join(repo, ".worktrees", "b-healthy")
	gitWorktreeAdd(t, repo, wt1, "a-legacy")
	gitWorktreeAdd(t, repo, wt2, "b-healthy")
	recs := []ports.WorktreeRecord{
		{Branch: "a-legacy", Slug: "a-legacy", AbsPath: wt1, Index: 0, Ports: map[string]int{"app": 8000}, Status: ports.StatusStopped, ComposeProject: "demo-a-legacy"},
		{Branch: "b-healthy", Slug: "b-healthy", AbsPath: wt2, Index: 1, Ports: map[string]int{"app": 8100}, Status: ports.StatusStopped, ComposeProject: "demo-b-healthy"},
	}
	updated, moved, _, err := migrateLegacyMainCollisions(r, recs)
	if err != nil {
		t.Fatal(err)
	}
	if len(moved) != 1 || moved[0] != "a-legacy" {
		t.Fatalf("moved = %v, want [a-legacy]", moved)
	}
	byBranch := map[string]ports.WorktreeRecord{}
	for _, rec := range updated {
		byBranch[rec.Branch] = rec
	}
	if byBranch["b-healthy"].Index != 1 || byBranch["b-healthy"].Ports["app"] != 8100 {
		t.Errorf("healthy renumbered: %+v", byBranch["b-healthy"])
	}
	if byBranch["a-legacy"].Index != 2 || byBranch["a-legacy"].Ports["app"] != 8200 {
		t.Errorf("legacy = %+v, want index 2 app 8200", byBranch["a-legacy"])
	}
}

// reconcileAndSave must auto-heal legacy state and persist, unblocking
// status/pull/dashboard without manual remove+re-add.
func TestReconcileAndSave_MigratesLegacyZero(t *testing.T) {
	r, _ := reconcileFixture(t)
	recs := []ports.WorktreeRecord{{
		Branch: "legacy", Slug: "legacy", AbsPath: filepath.Join(t.TempDir(), "legacy"),
		Index: 0, Ports: map[string]int{"app": 8000},
		Status: ports.StatusStopped, ComposeProject: "demo-legacy",
	}}
	updated, _, warns, err := reconcileAndSave(r, recs)
	if err != nil {
		t.Fatal(err)
	}
	if len(updated) != 1 || updated[0].Index != 1 || updated[0].Ports["app"] != 8100 {
		t.Fatalf("updated = %+v, want migrated index 1 app 8100", updated)
	}
	if len(warns) == 0 {
		t.Error("warns empty, want migration note")
	}
	if _, err := recordsWithMain(r, updated); err != nil {
		t.Fatalf("recordsWithMain after reconcile = %v", err)
	}
	raw, err := ports.Load(r.stateP)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 1 || raw[0].Index != 1 {
		t.Errorf("persisted = %+v, want migrated index 1", raw)
	}
}

// Non-colliding state must pass through untouched.
func TestMigrateLegacyNoopWhenHealthy(t *testing.T) {
	r, _ := reconcileFixture(t)
	recs := []ports.WorktreeRecord{{
		Branch: "ok", Slug: "ok", AbsPath: "/tmp/ok",
		Index: 1, Ports: map[string]int{"app": 8100},
		Status: ports.StatusStopped, ComposeProject: "demo-ok",
	}}
	updated, moved, warns, err := migrateLegacyMainCollisions(r, recs)
	if err != nil {
		t.Fatal(err)
	}
	if len(moved) != 0 || len(warns) != 0 {
		t.Errorf("moved=%v warns=%v, want none", moved, warns)
	}
	if len(updated) != 1 || updated[0].Index != 1 {
		t.Errorf("updated = %+v, want untouched", updated)
	}
}
