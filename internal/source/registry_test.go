package source

import (
	"strings"
	"testing"
)

func TestAvailable_ContainsGit(t *testing.T) {
	avail := Available()
	found := false
	for _, n := range avail {
		if n == "git" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Available() = %v, want it to contain git", avail)
	}
}

func TestResolve_Git(t *testing.T) {
	f, err := Resolve("git")
	if err != nil {
		t.Fatalf("Resolve(git) error: %v", err)
	}
	if f == nil {
		t.Fatal("Resolve(git) returned nil factory")
	}
	if s := f(); s == nil {
		t.Fatal("git factory returned nil Source")
	}
}

func TestResolve_UnknownListsAvailable(t *testing.T) {
	_, err := Resolve("nope-unknown")
	if err == nil {
		t.Fatal("expected error for unknown source type")
	}
	msg := err.Error()
	if !strings.Contains(msg, "nope-unknown") {
		t.Errorf("error should name the type: %v", err)
	}
	for _, n := range Available() {
		if !strings.Contains(msg, n) {
			t.Errorf("error should list available option %q: %v", n, err)
		}
	}
}
