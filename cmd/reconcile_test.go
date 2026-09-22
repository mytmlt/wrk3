package cmd

import (
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mytmlt/wrk3/internal/config"
	"github.com/mytmlt/wrk3/internal/ports"
	"github.com/mytmlt/wrk3/internal/source"
)

// errNoPRsForTest stubs the `gh` PR lookup in tests that exercise the
// unfiltered displayBranches path without PR data.
var errNoPRsForTest = errors.New("no PRs in test")

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
	if rec.Index != 1 || rec.Ports["app"] != 8001 {
		t.Errorf("rec = %+v, want index 1 app 8001 (index 0 is the main checkout)", rec)
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
	env := "APP_PORT=8001\n"
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
	if updated[0].Ports["app"] != 8001 || updated[0].Index != 1 {
		t.Errorf("rec = %+v, want recovered app 8001 index 1", updated[0])
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
	if !dirty || updated[0].Ports["app"] != 8001 || updated[0].Index != 1 {
		t.Errorf("updated = %+v dirty=%v, want fresh app 8001 index 1", updated, dirty)
	}
	if len(warns) == 0 {
		t.Error("diverged .env must warn")
	}
	raw, err := os.ReadFile(filepath.Join(wt, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "APP_PORT=8001\n") {
		t.Errorf("fresh allocation not written to .env:\n%s", raw)
	}
	if strings.Contains(string(raw), "APP_PORT=9999") {
		t.Errorf("stale APP_PORT left intact:\n%s", raw)
	}
}

// A .env holding the base ports must not be adopted at the reserved
// main index: the orphan gets a fresh managed index with warnings, and
// ensure overwrites managed port keys to the new allocation.
func TestReconcileState_BaseEnvDoesNotAdoptMainSlot(t *testing.T) {
	r, repo := reconcileFixture(t)
	wt := filepath.Join(repo, ".worktrees", "feature-base")
	gitWorktreeAdd(t, repo, wt, "feature-base")
	if err := os.WriteFile(filepath.Join(wt, ".env"), []byte("APP_PORT=8000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	updated, warns, dirty, err := reconcileState(r, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !dirty || len(updated) != 1 {
		t.Fatalf("updated = %+v, dirty=%v, want 1 adopted", updated, dirty)
	}
	if updated[0].Index != 1 || updated[0].Ports["app"] != 8001 {
		t.Errorf("rec = %+v, want fresh index 1 app 8001, never the main slot", updated[0])
	}
	if len(warns) == 0 {
		t.Error("adopting over base-port .env values must warn")
	}
	raw, err := os.ReadFile(filepath.Join(wt, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "APP_PORT=8001\n") {
		t.Errorf("fresh allocation not written to .env:\n%s", raw)
	}
	if strings.Contains(string(raw), "APP_PORT=8000") {
		t.Errorf("main ports left in worktree .env:\n%s", raw)
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

func TestRecoveredReusable_RejectsSelfCollision(t *testing.T) {
	alloc := ports.Allocator{
		Base:   map[string]int{"app": 8000, "web": 3000},
		Ranges: map[string][2]int{"app": {8000, 8099}, "web": {3000, 3099}},
	}
	base := map[string]int{"app": 8000, "web": 3000}
	mainPorts := alloc.BaseAllocation()
	// Distinct in-range values reuse.
	if !recoveredReusable(alloc, base, map[string]int{"app": 8001, "web": 3001}, map[int]struct{}{8000: {}, 3000: {}}, mainPorts) {
		t.Error("distinct in-range recovered ports should be reusable")
	}
	// Same host port across services must not reuse.
	if recoveredReusable(alloc, base, map[string]int{"app": 8001, "web": 8001}, map[int]struct{}{8000: {}, 3000: {}}, mainPorts) {
		t.Error("self-colliding recovered ports (app==web) should not be reusable")
	}
}

func TestRecoveredReusable_AliasLockstep(t *testing.T) {
	alloc := ports.Allocator{
		Base:   map[string]int{"app": 8000, "public_api": 8000},
		Ranges: map[string][2]int{"app": {8000, 8099}, "public_api": {8000, 8099}},
	}
	base := map[string]int{"app": 8000, "public_api": 8000}
	mainPorts := alloc.BaseAllocation()
	taken := map[int]struct{}{8000: {}}
	// Aliases sharing one recovered port reuse.
	if !recoveredReusable(alloc, base, map[string]int{"app": 8001, "public_api": 8001}, taken, mainPorts) {
		t.Error("alias recovered ports sharing one value should be reusable")
	}
	// Split aliases must not reuse.
	if recoveredReusable(alloc, base, map[string]int{"app": 8001, "public_api": 8002}, taken, mainPorts) {
		t.Error("split alias recovered ports should not be reusable")
	}
}

func urlSpecForTest(t *testing.T, v, raw string, r [2]int) ports.URLSpec {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return ports.URLSpec{Var: v, BaseURL: parsed, Range: r}
}

func TestUrlRecoveredReusable_AliasLockstep(t *testing.T) {
	specs := []ports.URLSpec{
		urlSpecForTest(t, "BASE_URL", "http://localhost:8000", [2]int{8000, 8099}),
		urlSpecForTest(t, "ALLOWED_WS_ORIGINS", "http://localhost:8000", [2]int{8000, 8099}),
	}
	taken := map[int]struct{}{8000: {}}
	// Aliases sharing one recovered port reuse.
	if !urlRecoveredReusable(map[string]int{"BASE_URL": 8001, "ALLOWED_WS_ORIGINS": 8001}, specs, taken) {
		t.Error("alias URL ports sharing one value should be reusable")
	}
	// Split aliases must not reuse.
	if urlRecoveredReusable(map[string]int{"BASE_URL": 8001, "ALLOWED_WS_ORIGINS": 8002}, specs, taken) {
		t.Error("split alias URL ports should not be reusable")
	}
	// Distinct bases sharing one value must not reuse.
	distinct := []ports.URLSpec{
		urlSpecForTest(t, "APP_URL", "http://localhost:8000", [2]int{8000, 8099}),
		urlSpecForTest(t, "API_URL", "http://localhost:9000", [2]int{9000, 9099}),
	}
	if urlRecoveredReusable(map[string]int{"APP_URL": 8001, "API_URL": 8001}, distinct, taken) {
		t.Error("distinct-base URL ports sharing one value should not be reusable")
	}
}

func writeTestConfigWithURLs(t *testing.T, repoRoot string) *config.Config {
	t.Helper()
	content := `project:
  worktreeBase: .worktrees
source:
  type: git
  git: {remote: origin, fetchPrune: true}
runner:
  type: docker
  docker:
    composeFiles: [docker-compose.yml]
    projectPrefix: demo
entry:
  setup: ["echo setup"]
  run: "echo run"
  stop: "echo stop"
  logs: "echo logs"
ports:
  base: {app: 8000}
  ranges:
    app: [8000, 8099]
urls:
  - var: BASE_URL
    base: http://localhost:8000
    range: [8000, 8099]
  - var: ALLOWED_WS_ORIGINS
    base: http://localhost:8000
    range: [8000, 8099]
`
	path := filepath.Join(repoRoot, "wrk3.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestAssignAllocation_URLTracksHost(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfigWithURLs(t, repo)
	r := &resolved{cfg: cfg, src: &source.GitSource{}}
	got, err := assignAllocation(r, nil)
	if err != nil {
		t.Fatalf("assignAllocation = %v", err)
	}
	// main holds 8000; host app takes 8001 and both URL aliases track it.
	if got.Ports["app"] != 8001 {
		t.Errorf("app = %d, want 8001", got.Ports["app"])
	}
	if got.URLs["BASE_URL"] != 8001 || got.URLs["ALLOWED_WS_ORIGINS"] != 8001 {
		t.Errorf("urls = %v, want BASE_URL=ALLOWED_WS_ORIGINS=8001 (tracked)", got.URLs)
	}
}

func TestMigrateURLTracking_HealsScannedPast(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfigWithURLs(t, repo)
	r := &resolved{cfg: cfg, src: &source.GitSource{}}
	recs := []ports.WorktreeRecord{
		{
			Branch: "b", Slug: "b", Index: 1,
			AbsPath: filepath.Join(t.TempDir(), "missing-b"),
			Ports:   map[string]int{"app": 8001},
			Urls:    map[string]int{"BASE_URL": 8002, "ALLOWED_WS_ORIGINS": 8002},
		},
	}
	updated, moved, _, err := migrateURLTracking(r, recs)
	if err != nil {
		t.Fatalf("migrateURLTracking = %v", err)
	}
	if len(moved) != 1 || moved[0] != "b" {
		t.Fatalf("moved = %v, want [b]", moved)
	}
	if updated[0].Urls["BASE_URL"] != 8001 || updated[0].Urls["ALLOWED_WS_ORIGINS"] != 8001 {
		t.Errorf("urls = %v, want both 8001 (tracked host port)", updated[0].Urls)
	}
}

func TestMigrateURLTracking_HealsSplitAliases(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfigWithURLs(t, repo)
	r := &resolved{cfg: cfg, src: &source.GitSource{}}
	recs := []ports.WorktreeRecord{
		{
			Branch: "b", Slug: "b", Index: 1,
			AbsPath: filepath.Join(t.TempDir(), "missing-b"),
			Ports:   map[string]int{"app": 8001},
			Urls:    map[string]int{"BASE_URL": 8002, "ALLOWED_WS_ORIGINS": 8003},
		},
	}
	updated, moved, _, err := migrateURLTracking(r, recs)
	if err != nil {
		t.Fatalf("migrateURLTracking = %v", err)
	}
	if len(moved) != 1 {
		t.Fatalf("moved = %v, want [b]", moved)
	}
	if updated[0].Urls["BASE_URL"] != 8001 || updated[0].Urls["ALLOWED_WS_ORIGINS"] != 8001 {
		t.Errorf("urls = %v, want both 8001", updated[0].Urls)
	}
}

func TestMigrateURLTracking_LeavesTrackedAlone(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfigWithURLs(t, repo)
	r := &resolved{cfg: cfg, src: &source.GitSource{}}
	recs := []ports.WorktreeRecord{
		{
			Branch: "b", Slug: "b", Index: 1,
			AbsPath: filepath.Join(t.TempDir(), "missing-b"),
			Ports:   map[string]int{"app": 8001},
			Urls:    map[string]int{"BASE_URL": 8001, "ALLOWED_WS_ORIGINS": 8001},
		},
	}
	_, moved, _, err := migrateURLTracking(r, recs)
	if err != nil {
		t.Fatalf("migrateURLTracking = %v", err)
	}
	if len(moved) != 0 {
		t.Errorf("moved = %v, want none (already tracked)", moved)
	}
}

func TestUnionBranches(t *testing.T) {
	got := unionBranches([]string{"b", "a"}, []string{"c", "a"})
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Errorf("got %v, want [a b c]", got)
	}
}

func TestDisplayBranches_UnfilteredUnionsLocal(t *testing.T) {
	stubPRs(t, nil, errNoPRsForTest)
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
