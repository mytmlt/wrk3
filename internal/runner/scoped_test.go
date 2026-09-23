package runner

import (
	"reflect"
	"testing"
)

func TestUpArgs(t *testing.T) {
	if got := upArgs(Options{}); !reflect.DeepEqual(got, []string{"up", "-d", "--build"}) {
		t.Errorf("default upArgs = %v", got)
	}
	got := upArgs(Options{NoDeps: true, Services: []string{"db", "app"}})
	want := []string{"up", "-d", "--build", "--no-deps", "db", "app"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("scoped upArgs = %v, want %v", got, want)
	}
	got = upArgs(Options{Wait: true, Services: []string{"db"}})
	want = []string{"up", "-d", "--build", "--wait", "db"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("wait upArgs = %v, want %v", got, want)
	}
}

func TestAllComposeFilesCopies(t *testing.T) {
	base := []string{"a.yml"}
	extra := []string{"b.yml"}
	got := allComposeFiles(Options{ComposeFiles: base, ExtraFiles: extra})
	if !reflect.DeepEqual(got, []string{"a.yml", "b.yml"}) {
		t.Fatalf("allComposeFiles = %v", got)
	}
	got[0] = "mutated"
	if base[0] != "a.yml" {
		t.Error("allComposeFiles must not mutate caller slices")
	}
}

func TestComposeArgsIncludeExtraFiles(t *testing.T) {
	r := New(Options{
		ComposeFiles:  []string{"docker-compose.yml"},
		ExtraFiles:    []string{"/abs/overlay.yml"},
		ProjectPrefix: "demo",
		Slug:          "x",
	})
	want := []string{"compose", "-p", "demo-x", "-f", "docker-compose.yml", "-f", "/abs/overlay.yml", "ps", "-q"}
	if got := r.composeArgs("ps", "-q"); !reflect.DeepEqual(got, want) {
		t.Errorf("composeArgs = %v, want %v", got, want)
	}
	p := NewPodman(Options{
		ComposeFiles: []string{"docker-compose.yml"},
		Services:     []string{"app"},
		NoDeps:       true,
	})
	args := p.composeArgs(upArgs(p.opts)...)
	want = []string{"compose", "-p", "wrk3", "-f", "docker-compose.yml", "up", "-d", "--build", "--no-deps", "app"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("podman scoped up args = %v, want %v", args, want)
	}
}
