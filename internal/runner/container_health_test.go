package runner

import (
	"context"
	"testing"
)

func TestParseInspectHealth(t *testing.T) {
	out := "abc123 /demo-app-1 healthy\ndef456 /demo-db-1 unhealthy\nghi789 /demo-web-1 none\n"
	got := parseInspectHealth(out)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[0].Name != "demo-app-1" || got[0].Status != "healthy" {
		t.Errorf("row0 = %+v, want demo-app-1/healthy", got[0])
	}
	if !got[0].HasHealth() || got[2].HasHealth() {
		t.Errorf("HasHealth: healthy should be true, none should be false")
	}
	if got[1].Status != "unhealthy" {
		t.Errorf("row1 status = %q, want unhealthy", got[1].Status)
	}
	if got := parseInspectHealth("  \nbadline\n"); len(got) != 0 {
		t.Errorf("bad input parsed = %v, want empty", got)
	}
}

func TestContainerHealthsUnsupported(t *testing.T) {
	if _, err := ContainerHealths(context.Background(), NomadRunner{}, "/tmp"); err == nil {
		t.Error("NomadRunner ContainerHealths = nil, want error")
	}
	if _, err := ContainerHealths(context.Background(), PortainerRunner{}, "/tmp"); err == nil {
		t.Error("PortainerRunner ContainerHealths = nil, want error")
	}
}

func TestContainerHealthsEmptyPath(t *testing.T) {
	r := New(Options{Slug: "x"})
	if _, err := r.ContainerHealths(context.Background(), ""); err == nil {
		t.Error("empty path = nil, want error")
	}
	p := NewPodman(Options{Slug: "x"})
	if _, err := p.ContainerHealths(context.Background(), "  "); err == nil {
		t.Error("blank path = nil, want error")
	}
}
