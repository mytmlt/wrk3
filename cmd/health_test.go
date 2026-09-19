package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/mytmlt/wrk3/internal/config"
	"github.com/mytmlt/wrk3/internal/health"
	"github.com/mytmlt/wrk3/internal/ports"
	"github.com/mytmlt/wrk3/internal/runner"
)

func testHealthConfig(checks ...config.HealthCheck) *config.Config {
	return &config.Config{
		Project: config.ProjectConfig{WorktreeBase: ".worktrees"},
		Source:  config.SourceConfig{Type: "git"},
		Runner: config.RunnerConfig{
			Type:   "docker",
			Docker: config.DockerConfig{ComposeFiles: []string{"docker-compose.yml"}},
		},
		Entry:  config.EntryConfig{Run: "echo run", Stop: "echo stop"},
		Ports:  config.PortsConfig{Base: map[string]int{"app": 8000}, Ranges: map[string][2]int{"app": {8000, 8099}}},
		Health: config.HealthConfig{Checks: checks},
	}
}

func TestContainerResults(t *testing.T) {
	hs := []runner.ContainerHealth{
		{ID: "a", Name: "web-1", Status: "healthy"},
		{ID: "b", Name: "db-1", Status: "unhealthy"},
		{ID: "c", Name: "cache-1", Status: "starting"},
		{ID: "d", Name: "plain-1", Status: "none"},
	}
	got := containerResults(hs)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3 (none skipped)", len(got))
	}
	byName := map[string]bool{}
	for _, r := range got {
		byName[r.Name] = r.Healthy
	}
	if !byName["container:web-1"] {
		t.Error("container:web-1 should pass")
	}
	if byName["container:db-1"] {
		t.Error("container:db-1 should fail")
	}
	if byName["container:cache-1"] {
		t.Error("container:cache-1 (starting) should fail")
	}
}

func TestWithHealthSuffix(t *testing.T) {
	dir := t.TempDir()
	mkRec := func() ports.WorktreeRecord {
		return ports.WorktreeRecord{
			Branch: "feature-a", Slug: "test-health-suffix-zzz",
			AbsPath: dir, Index: 1, Ports: map[string]int{"app": 8001},
			ComposeProject: "demo-test-health-suffix-zzz", Status: "running",
		}
	}
	cases := []struct {
		name      string
		lifecycle string
		cfg       *config.Config
		want      string
	}{
		{"stopped passthrough", "stopped", testHealthConfig(config.HealthCheck{Name: "a", Run: "false"}), "stopped"},
		{"failed passthrough", "failed", testHealthConfig(config.HealthCheck{Name: "a", Run: "true"}), "failed"},
		{"setting up passthrough", "setting up", testHealthConfig(config.HealthCheck{Name: "a", Run: "true"}), "setting up"},
		{"no checks bare", "running", testHealthConfig(), "running"},
		{"single pass", "running", testHealthConfig(config.HealthCheck{Name: "api", Run: "true"}), "running (healthy)"},
		{"single fail", "running", testHealthConfig(config.HealthCheck{Name: "api", Run: "false"}), "running (unhealthy)"},
		{
			"mixed degraded", "running",
			testHealthConfig(
				config.HealthCheck{Name: "api", Run: "true"},
				config.HealthCheck{Name: "db", Run: "false"},
			),
			"running (degraded 1/2)",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := withHealthSuffix(c.cfg, mkRec(), c.lifecycle); got != c.want {
				t.Errorf("withHealthSuffix = %q, want %q", got, c.want)
			}
		})
	}
}

func TestHealthReportSummaryInDashboard(t *testing.T) {
	dir := t.TempDir()
	cfg := testHealthConfig(
		config.HealthCheck{Name: "api", Run: "true"},
		config.HealthCheck{Name: "db", Run: "false"},
	)
	recs := []ports.WorktreeRecord{{
		Branch: "feature-a", Slug: "test-health-summary-zzz",
		AbsPath: dir, Index: 1, Ports: map[string]int{"app": 8001},
		ComposeProject: "demo-test-health-summary-zzz", Status: "stopped",
	}}
	// Stored stopped + no containers running: probeDashboardRows resolves
	// stopped (no health probe, no suffix). This pins the non-running path
	// without depending on daemon state beyond "no such project running".
	rows := probeDashboardRows(&resolved{cfg: cfg}, recs, "")
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if rows[0].Status != "stopped" {
		t.Errorf("Status = %q, want stopped", rows[0].Status)
	}
	if rows[0].Health != "" {
		t.Errorf("Health = %q, want empty for non-running", rows[0].Health)
	}
}

func TestDashboardWorkColumnsFor_HealthSuffix(t *testing.T) {
	cols := dashboardWorkColumnsFor(200, []dashboardWorkCells{{
		worktree: "feature-a", branch: "feature-a",
		status:  "running (degraded 1/2)",
		ports:   "app=8001",
		url:     "http://localhost:8001",
		project: "demo-feature-a",
	}})
	byTitle := map[string]int{}
	sum := 0
	for _, c := range cols {
		byTitle[c.Title] = c.Width
		sum += c.Width
	}
	if sum > 200 {
		t.Errorf("columns sum %d overflows width 200", sum)
	}
	if byTitle["STATUS"] < len("running (degraded 1/2)") {
		t.Errorf("STATUS width %d truncates health suffix", byTitle["STATUS"])
	}
}

func TestProbeStatusCells(t *testing.T) {
	dir := t.TempDir()
	cfg := testHealthConfig()
	recs := []ports.WorktreeRecord{
		{Branch: "b", Slug: "b", AbsPath: dir, Index: 1, Ports: map[string]int{"app": 8001}, ComposeProject: "p-b", Status: "stopped"},
		{Branch: "a", Slug: "a", AbsPath: "/nonexistent-wrk3-health-test", Index: 2, Ports: map[string]int{"app": 8002}, ComposeProject: "p-a", Status: "running"},
	}
	cells := probeStatusCells(cfg, recs)
	if len(cells) != 2 {
		t.Fatalf("len = %d, want 2", len(cells))
	}
	// Order preserved.
	if cells[0].branch != "b" || cells[1].branch != "a" {
		t.Fatalf("order not preserved: %+v", cells)
	}
	if cells[1].status != "stale" || cells[1].ports != "?" {
		t.Errorf("missing dir = %q/%q, want stale/?", cells[1].status, cells[1].ports)
	}
}

func TestShellPhaseBudget(t *testing.T) {
	if got := shellPhaseBudget(nil); got != 15*time.Second {
		t.Errorf("empty = %v, want 15s (10s default + 5s headroom)", got)
	}
	checks := []health.Check{{Name: "slow", Run: "true", Timeout: 90 * time.Second}}
	if got := shellPhaseBudget(checks); got != 95*time.Second {
		t.Errorf("90s check = %v, want 95s", got)
	}
}

func TestDetailPaneShowsHealth(t *testing.T) {
	m := testDashboardModel()
	m.rows[0].Status = "running (healthy)"
	m.rows[0].Health = "api: pass"
	out := m.detailPane(60, 20)
	for _, want := range []string{"health:", "api: pass"} {
		if !strings.Contains(out, want) {
			t.Errorf("detail pane missing %q:\n%s", want, out)
		}
	}
	// Empty health falls back to "no checks".
	m.rows[0].Health = ""
	if out := m.detailPane(60, 20); !strings.Contains(out, "no checks") {
		t.Errorf("detail pane should show no checks:\n%s", out)
	}
}
