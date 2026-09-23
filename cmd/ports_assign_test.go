package cmd

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mytmlt/wrk3/internal/config"
	"github.com/mytmlt/wrk3/internal/ports"
	"github.com/mytmlt/wrk3/internal/source"
)

// TestMain stubs the OS bind probe for hermetic unit tests: no network
// in cmd tests. Individual tests override osPortFree temporarily to
// simulate an occupied port.
func TestMain(m *testing.M) {
	osPortFree = func(int) bool { return true }
	os.Exit(m.Run())
}

func testAllocator() ports.Allocator {
	return ports.Allocator{
		Base:   map[string]int{"app": 8000},
		Ranges: map[string][2]int{"app": {8000, 8099}},
	}
}

func TestAssignPorts_GapReuse(t *testing.T) {
	alloc := testAllocator()
	recs := []ports.WorktreeRecord{
		{Branch: "a", Slug: "a", Index: 1, Ports: map[string]int{"app": 8001}},
		{Branch: "b", Slug: "b", Index: 2, Ports: map[string]int{"app": 8003}},
	}
	got, err := assignPorts(alloc, recs)
	if err != nil {
		t.Fatalf("assignPorts = %v", err)
	}
	// main holds 8000, a holds 8001, b holds 8003 -> lowest free is 8002.
	if got["app"] != 8002 {
		t.Errorf("app = %d, want gap reuse 8002", got["app"])
	}
}

func TestAssignPorts_SkipsOSOccupied(t *testing.T) {
	alloc := testAllocator()
	old := osPortFree
	osPortFree = func(p int) bool { return p != 8001 }
	defer func() { osPortFree = old }()
	got, err := assignPorts(alloc, nil)
	if err != nil {
		t.Fatalf("assignPorts = %v", err)
	}
	// 8000 taken by main reservation, 8001 OS-occupied -> 8002.
	if got["app"] != 8002 {
		t.Errorf("app = %d, want 8002 (main 8000, OS 8001)", got["app"])
	}
}

func TestAssignPorts_Exhaustion(t *testing.T) {
	alloc := ports.Allocator{
		Base:   map[string]int{"app": 8000},
		Ranges: map[string][2]int{"app": {8000, 8001}},
	}
	recs := []ports.WorktreeRecord{
		{Branch: "a", Slug: "a", Index: 1, Ports: map[string]int{"app": 8001}},
	}
	_, err := assignPorts(alloc, recs)
	if err == nil {
		t.Fatal("assignPorts = nil, want exhaustion (8000 main, 8001 taken)")
	}
}

func TestPreviewPorts_StateOnly(t *testing.T) {
	alloc := testAllocator()
	old := osPortFree
	osPortFree = func(p int) bool { return false }
	defer func() { osPortFree = old }()
	// preview ignores osPortFree (nil isFree): still finds 8001.
	got, err := previewPorts(alloc, nil)
	if err != nil {
		t.Fatalf("previewPorts = %v", err)
	}
	if got["app"] != 8001 {
		t.Errorf("app = %d, want 8001 state-only", got["app"])
	}
}

func mustURLSpec(varName, raw string, lo, hi int) ports.URLSpec {
	u, err := url.Parse(raw)
	if err != nil {
		panic(err)
	}
	return ports.URLSpec{Var: varName, BaseURL: u, Range: [2]int{lo, hi}}
}

func TestURLRecoveredReusable_Alias(t *testing.T) {
	specs := []ports.URLSpec{
		mustURLSpec("BASE_URL", "http://localhost:8000", 8000, 8099),
		mustURLSpec("ALLOWED_WS_ORIGINS", "http://localhost:8000", 8000, 8099),
	}
	taken := map[int]struct{}{8000: {}, 8001: {}}
	// Shared alias value is reusable.
	if !urlRecoveredReusable(map[string]int{"BASE_URL": 8002, "ALLOWED_WS_ORIGINS": 8002}, taken, specs, nil) {
		t.Error("shared alias URL value should be reusable")
	}
	// Split alias values are not.
	if urlRecoveredReusable(map[string]int{"BASE_URL": 8002, "ALLOWED_WS_ORIGINS": 8003}, taken, specs, nil) {
		t.Error("split alias URL values should not be reusable")
	}
	// Distinct base ports sharing one value collide.
	distinct := []ports.URLSpec{
		mustURLSpec("A_URL", "http://localhost:8000", 8000, 8099),
		mustURLSpec("B_URL", "http://localhost:9000", 9000, 9099),
	}
	if urlRecoveredReusable(map[string]int{"A_URL": 8002, "B_URL": 8002}, taken, distinct, nil) {
		t.Error("distinct URL bases sharing one port should not be reusable")
	}
}

func TestURLRecoveredReusable_Tracked(t *testing.T) {
	specs := []ports.URLSpec{
		mustURLSpec("BASE_URL", "http://localhost:8000", 8000, 8099),
		mustURLSpec("ALLOWED_WS_ORIGINS", "http://localhost:8000", 8000, 8099),
	}
	// taken holds the worktree's own host port: tracked values must not
	// trip the collision check, but must equal the tracked port.
	tracked := map[int]int{8000: 8001}
	taken := map[int]struct{}{8000: {}, 8001: {}}
	if !urlRecoveredReusable(map[string]int{"BASE_URL": 8001, "ALLOWED_WS_ORIGINS": 8001}, taken, specs, tracked) {
		t.Error("tracked URL values equal to the host port should be reusable")
	}
	// Diverged from the tracked host port: not reusable (migrates).
	if urlRecoveredReusable(map[string]int{"BASE_URL": 8002, "ALLOWED_WS_ORIGINS": 8002}, taken, specs, tracked) {
		t.Error("URL values diverged from the tracked host port should not be reusable")
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
  base: {app: 8000, public_api: 8000}
  ranges:
    app: [8000, 8099]
    public_api: [8000, 8099]
urls:
  - {var: BASE_URL, base: http://localhost:8000, range: [8000, 8099]}
  - {var: ALLOWED_WS_ORIGINS, base: http://localhost:8000, range: [8000, 8099]}
  - {var: DOCS_URL, base: http://localhost:9000, range: [9000, 9099]}
`
	path := filepath.Join(repoRoot, "wrk3-urls.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestMigrateTrackedURLs_RewritesDiverged(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfigWithURLs(t, repo)
	r := &resolved{cfg: cfg, src: &source.GitSource{}, base: cfg.AbsWorktreeBase(), stateP: cfg.StatePath()}
	wt := t.TempDir()
	if err := os.WriteFile(filepath.Join(wt, ports.EnvFileName),
		[]byte("APP_PORT=8001\nPUBLIC_API_PORT=8001\nBASE_URL=http://localhost:8002\nALLOWED_WS_ORIGINS=http://localhost:8002\nDOCS_URL=http://localhost:9000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	recs := []ports.WorktreeRecord{
		{Branch: "feat", Slug: "feat", AbsPath: wt, Index: 1,
			Ports:  map[string]int{"app": 8001, "public_api": 8001},
			Urls:   map[string]int{"BASE_URL": 8002, "ALLOWED_WS_ORIGINS": 8002, "DOCS_URL": 9000},
			Status: ports.StatusStopped},
	}
	updated, moved, warns, err := migrateTrackedURLs(r, recs)
	if err != nil {
		t.Fatal(err)
	}
	if len(moved) != 1 || moved[0] != "feat" {
		t.Fatalf("moved = %v, want [feat]", moved)
	}
	if len(warns) == 0 {
		t.Error("warns should name the migration")
	}
	got := updated[0].Urls
	if got["BASE_URL"] != 8001 || got["ALLOWED_WS_ORIGINS"] != 8001 {
		t.Errorf("tracked URLs = %v, want BASE_URL=ALLOWED_WS_ORIGINS=8001", got)
	}
	if got["DOCS_URL"] != 9000 {
		t.Errorf("untracked DOCS_URL = %d, want untouched 9000", got["DOCS_URL"])
	}
	raw, err := os.ReadFile(filepath.Join(wt, ports.EnvFileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "BASE_URL=http://localhost:8001") {
		t.Errorf(".env was not rewritten to the tracked port:\n%s", raw)
	}
}

func TestMigrateTrackedURLs_NoOpWhenAligned(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfigWithURLs(t, repo)
	r := &resolved{cfg: cfg, src: &source.GitSource{}, base: cfg.AbsWorktreeBase(), stateP: cfg.StatePath()}
	recs := []ports.WorktreeRecord{
		{Branch: "feat", Slug: "feat", AbsPath: filepath.Join(repo, ".worktrees", "missing"), Index: 1,
			Ports:  map[string]int{"app": 8001, "public_api": 8001},
			Urls:   map[string]int{"BASE_URL": 8001, "ALLOWED_WS_ORIGINS": 8001, "DOCS_URL": 9000},
			Status: ports.StatusStopped},
		{Branch: "old", Slug: "old", AbsPath: filepath.Join(repo, ".worktrees", "missing2"), Index: 2,
			Ports:  map[string]int{"app": 8002, "public_api": 8002},
			Status: ports.StatusStopped},
	}
	updated, moved, _, err := migrateTrackedURLs(r, recs)
	if err != nil {
		t.Fatal(err)
	}
	if len(moved) != 0 {
		t.Errorf("moved = %v, want none (aligned URLs and records without URLs stay)", moved)
	}
	if len(updated) != 2 {
		t.Errorf("len(updated) = %d, want 2", len(updated))
	}
}
