package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mytmlt/wrk3/internal/project"
)

type resolveStubStore struct {
	projects []project.Project
	listErr  error
}

func (s *resolveStubStore) Touch(string, string) error { return nil }

func (s *resolveStubStore) Get(name string) (*project.Project, error) {
	for i := range s.projects {
		if s.projects[i].Name == name {
			p := s.projects[i]
			return &p, nil
		}
	}
	return nil, fmt.Errorf("get project %q: %w", name, project.ErrNotFound)
}

func (s *resolveStubStore) List() ([]project.Project, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.projects, nil
}

func withDashboardResolveState(t *testing.T, file, projectArg string, store func() (project.Store, error)) {
	t.Helper()
	oldFile, oldArg, oldStore := fileFlag, dashboardProjectArg, newProjectStore
	fileFlag = file
	dashboardProjectArg = projectArg
	if store != nil {
		newProjectStore = store
	}
	t.Cleanup(func() {
		fileFlag = oldFile
		dashboardProjectArg = oldArg
		newProjectStore = oldStore
	})
}

func TestLatestRegistryProject_PicksMostRecentlySeen(t *testing.T) {
	older := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	got := latestRegistryProject([]project.Project{
		{Name: "old", ConfigPath: "/old/wrk3.yaml", LastSeen: older},
		{Name: "new", ConfigPath: "/new/wrk3.yaml", LastSeen: newer},
		{Name: "mid", ConfigPath: "/mid/wrk3.yaml", LastSeen: older.Add(time.Hour)},
	})
	if got.ConfigPath != "/new/wrk3.yaml" {
		t.Errorf("got %q, want /new/wrk3.yaml", got.ConfigPath)
	}
}

func TestResolveDashboardInitialPath_FallsBackToLatestRegistry(t *testing.T) {
	dir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	older := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	withDashboardResolveState(t, "", "", func() (project.Store, error) {
		return &resolveStubStore{projects: []project.Project{
			{Name: "old", ConfigPath: "/old/wrk3.yaml", LastSeen: older},
			{Name: "new", ConfigPath: "/new/wrk3.yaml", LastSeen: newer},
		}}, nil
	})

	got, err := resolveDashboardInitialPath(dashboardCmd)
	if err != nil {
		t.Fatalf("resolveDashboardInitialPath: %v", err)
	}
	if got != "/new/wrk3.yaml" {
		t.Errorf("got %q, want /new/wrk3.yaml", got)
	}
}

func TestResolveDashboardInitialPath_EmptyRegistryIsUsageError(t *testing.T) {
	dir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	withDashboardResolveState(t, "", "", func() (project.Store, error) {
		return &resolveStubStore{}, nil
	})

	_, err = resolveDashboardInitialPath(dashboardCmd)
	if !IsUsageError(err) {
		t.Fatalf("err = %v, want usage error", err)
	}
}

func TestResolveDashboardInitialPath_LocalYamlWinsOverRegistry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wrk3.yaml")
	if err := os.WriteFile(path, []byte("project: {worktreeBase: .w}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	withDashboardResolveState(t, "", "", func() (project.Store, error) {
		return &resolveStubStore{projects: []project.Project{
			{Name: "reg", ConfigPath: "/reg/wrk3.yaml", LastSeen: time.Now()},
		}}, nil
	})

	got, err := resolveDashboardInitialPath(dashboardCmd)
	if err != nil {
		t.Fatalf("resolveDashboardInitialPath: %v", err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Clean(abs) {
		t.Errorf("got %q, want local %q", got, abs)
	}
}

func TestResolveDashboardInitialPath_ProjectFlag(t *testing.T) {
	withDashboardResolveState(t, "", "old", func() (project.Store, error) {
		return &resolveStubStore{projects: []project.Project{
			{Name: "old", ConfigPath: "/old/wrk3.yaml"},
			{Name: "new", ConfigPath: "/new/wrk3.yaml", LastSeen: time.Now()},
		}}, nil
	})
	got, err := resolveDashboardInitialPath(dashboardCmd)
	if err != nil {
		t.Fatalf("resolveDashboardInitialPath: %v", err)
	}
	if got != "/old/wrk3.yaml" {
		t.Errorf("got %q, want /old/wrk3.yaml", got)
	}
}

func TestResolveDashboardInitialPath_ProjectAndFileConflict(t *testing.T) {
	withDashboardResolveState(t, "/x/wrk3.yaml", "old", nil)
	_, err := resolveDashboardInitialPath(dashboardCmd)
	if err == nil {
		t.Fatal("expected conflict error")
	}
}

func TestResolveDashboardInitialPath_ListError(t *testing.T) {
	dir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	withDashboardResolveState(t, "", "", func() (project.Store, error) {
		return &resolveStubStore{listErr: errors.New("list failed")}, nil
	})
	_, err = resolveDashboardInitialPath(dashboardCmd)
	if err == nil {
		t.Fatal("expected list error")
	}
	if IsUsageError(err) {
		t.Fatalf("list error treated as usage error: %v", err)
	}
}

func TestResolveDashboardInitialPath_StoreOpenFailureIsUsageError(t *testing.T) {
	dir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	withDashboardResolveState(t, "", "", func() (project.Store, error) {
		return nil, errors.New("boom")
	})
	_, err = resolveDashboardInitialPath(dashboardCmd)
	if !IsUsageError(err) {
		t.Fatalf("err = %v, want usage error", err)
	}
}
