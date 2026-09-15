package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mytmlt/wrk3/internal/ports"
	"github.com/mytmlt/wrk3/internal/source"
)

func gitWorktreeAdd(t *testing.T, repo, path, branch string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "-C", repo, "worktree", "add", path, "-b", branch)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v: %s", err, out)
	}
}

func reconcileFixture(t *testing.T) (*resolved, string) {
	t.Helper()
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	r := &resolved{cfg: cfg, src: &source.GitSource{}, base: cfg.AbsWorktreeBase(), stateP: cfg.StatePath()}
	return r, repo
}

// Empty state + on-disk worktree must adopt with a fresh deterministic
// allocation and persist the state file.
func TestReconcileAndSave_AdoptsOrphanEmptyState(t *testing.T) {
	r, repo := reconcileFixture(t)
	wt := filepath.Join(repo, ".worktrees", "feature-x")
	gitWorktreeAdd(t, repo, wt, "feature-x")

	updated, adopted, warns, err := reconcileAndSave(r, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(adopted) != 1 || adopted[0] != "feature-x" {
		t.Errorf("adopted = %v, want [feature-x]", adopted)
	}
	if len(warns) != 0 {
		t.Errorf("warns = %v, want none for fresh .env", warns)
	}
	if len(updated) != 1 {
		t.Fatalf("updated = %+v, want 1 record", updated)
	}
	rec := updated[0]
	wantPath, err := filepath.EvalSymlinks(wt)
	if err != nil {
		wantPath = filepath.Clean(wt)
	}
	if rec.Branch != "feature-x" || rec.Slug != "feature-x" || rec.AbsPath != filepath.Clean(wantPath) {
		t.Errorf("rec = %+v, want feature-x at %s", rec, wantPath)
	}
	if rec.Index != 0 || rec.Ports["app"] != 8000 {
		t.Errorf("rec = %+v, want index 0 app 8000", rec)
	}
	if rec.ComposeProject != "demo-feature-x" {
		t.Errorf("ComposeProject = %q, want demo-feature-x", rec.ComposeProject)
	}
	if rec.Status != ports.StatusStopped {
		t.Errorf("Status = %q, want stopped", rec.Status)
	}
	// Persisted: reload raw state and reconcile again must be clean.
	raw, err := ports.Load(r.stateP)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 1 {
		t.Fatalf("saved state = %+v, want 1 record", raw)
	}
	again, adopted, _, err := reconcileAndSave(r, raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(adopted) != 0 || len(again) != 1 {
		t.Errorf("second reconcile must be a no-op: adopted=%v recs=%+v", adopted, again)
	}
}

// A complete, on-grid .env allocation must be recovered, not reassigned.
func TestReconcileState_RecoversPortsFromEnv(t *testing.T) {
	r, repo := reconcileFixture(t)
	wt := filepath.Join(repo, ".worktrees", "feature-y")
	gitWorktreeAdd(t, repo, wt, "feature-y")
	env := "APP_PORT=8100\nBASE_URL=http://localhost:8100\nWEBHOOKS_BASE_URL=http://localhost:8100\nALLOWED_WS_ORIGINS=http://localhost:8100\n"
	if err := os.WriteFile(filepath.Join(wt, ".env"), []byte(env), 0o644); err != nil {
		t.Fatal(err)
	}
	updated, warns, dirty, err := reconcileState(r, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !dirty || len(updated) != 1 {
		t.Fatalf("updated = %+v, dirty=%v, want 1 adopted", updated, dirty)
	}
	if updated[0].Ports["app"] != 8100 || updated[0].Index != 1 {
		t.Errorf("rec = %+v, want recovered app 8100 index 1", updated[0])
	}
	if len(warns) != 0 {
		t.Errorf("warns = %v, want none when .env matches", warns)
	}
}

// Diverged .env ports fall back to a fresh allocation with warnings.
func TestReconcileState_DivergedEnvAllocatesFresh(t *testing.T) {
	r, repo := reconcileFixture(t)
	wt := filepath.Join(repo, ".worktrees", "feature-z")
	gitWorktreeAdd(t, repo, wt, "feature-z")
	if err := os.WriteFile(filepath.Join(wt, ".env"), []byte("APP_PORT=9999\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	updated, warns, dirty, err := reconcileState(r, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !dirty || updated[0].Ports["app"] != 8000 {
		t.Errorf("updated = %+v, want fresh app 8000", updated)
	}
	if len(warns) == 0 {
		t.Error("diverged .env must warn")
	}
}

// Slug collisions stay hard errors; state is never half-written.
func TestReconcileState_SlugCollisionErrors(t *testing.T) {
	r, repo := reconcileFixture(t)
	wt := filepath.Join(repo, ".worktrees", "feature-a")
	gitWorktreeAdd(t, repo, wt, "feature/a")
	recs := []ports.WorktreeRecord{{Branch: "other", Slug: "feature-a", AbsPath: "/x", Index: 0, Ports: map[string]int{"app": 8000}}}
	if _, _, _, err := reconcileState(r, recs); err == nil {
		t.Fatal("expected slug collision error")
	}
}

// List failures degrade to no adoption instead of failing reads.
func TestReconcileState_ListErrorSkips(t *testing.T) {
	r, _ := reconcileFixture(t)
	r.src = &mainStubSource{listErr: os.ErrClosed}
	updated, _, dirty, err := reconcileState(r, nil)
	if err != nil {
		t.Fatal(err)
	}
	if dirty || len(updated) != 0 {
		t.Errorf("list error must skip: dirty=%v updated=%+v", dirty, updated)
	}
}

func TestUnionBranches(t *testing.T) {
	got := unionBranches([]string{"b", "a"}, []string{"c", "a"})
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Errorf("got %v, want [a b c]", got)
	}
}

func TestDisplayBranches_UnfilteredUnionsLocal(t *testing.T) {
	s := &stubSource{refs: []string{"remote-only"}, local: []string{"local-only", "remote-only"}}
	got, err := displayBranches(s, ".", "origin", false, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "local-only" || got[1] != "remote-only" {
		t.Errorf("got %v, want union [local-only remote-only]", got)
	}
}

func TestDisplayBranches_FilteredStaysRemoteOnly(t *testing.T) {
	s := &stubSource{refs: []string{"a"}, local: []string{"zzz-local"}, name: "t", email: "t@t"}
	got, err := displayBranches(s, ".", "origin", true, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range got {
		if b == "zzz-local" {
			t.Errorf("filtered view must stay remote-only: %v", got)
		}
	}
}
