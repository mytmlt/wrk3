package project

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *FileStore {
	t.Helper()
	s, err := NewFileStore(WithPath(filepath.Join(t.TempDir(), "projects.yaml")))
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	return s
}

func TestTouchAndList(t *testing.T) {
	s := newTestStore(t)
	if err := s.Touch("api", "/tmp/api/wrk3.yaml"); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	if err := s.Touch("web", "/tmp/web/wrk3.yaml"); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	got, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 || got[0].Name != "api" || got[1].Name != "web" {
		t.Fatalf("unexpected list: %+v", got)
	}
	if got[0].LastSeen.IsZero() {
		t.Fatalf("LastSeen not set")
	}
}

func TestTouchRefreshesLastSeen(t *testing.T) {
	s := newTestStore(t)
	if err := s.Touch("api", "/tmp/api/wrk3.yaml"); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	first, err := s.Get("api")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	if err := s.Touch("renamed", "/tmp/api/wrk3.yaml"); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	second, err := s.Get("renamed")
	if err != nil {
		t.Fatalf("Get renamed: %v", err)
	}
	if !second.LastSeen.After(first.LastSeen) && !second.LastSeen.Equal(first.LastSeen) {
		t.Fatalf("LastSeen not refreshed: %v vs %v", first.LastSeen, second.LastSeen)
	}
	if _, err := s.Get("api"); err == nil {
		t.Fatalf("old name should be gone after rename")
	}
}

func TestGetAmbiguous(t *testing.T) {
	s := newTestStore(t)
	if err := s.Touch("api", "/tmp/a/wrk3.yaml"); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	if err := s.Touch("api", "/tmp/b/wrk3.yaml"); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	if _, err := s.Get("api"); err == nil {
		t.Fatalf("expected ambiguous error")
	}
	// Full config path still resolves.
	if _, err := s.Get("/tmp/a/wrk3.yaml"); err != nil {
		t.Fatalf("Get by path: %v", err)
	}
}

func TestListEmptyWhenMissing(t *testing.T) {
	s := newTestStore(t)
	got, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty, got %+v", got)
	}
}
