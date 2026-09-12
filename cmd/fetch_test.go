package cmd

import (
	"testing"

	"github.com/mytmlt/wrk3/internal/source"
)

type stubSource struct {
	refs     []string
	detailed []source.BranchRef
	name     string
	email    string
}

func (s *stubSource) Fetch(repoPath, remote string) error { return nil }
func (s *stubSource) Refs(repoPath, remote string) ([]string, error) {
	return s.refs, nil
}
func (s *stubSource) RefsDetailed(repoPath, remote string) ([]source.BranchRef, error) {
	return s.detailed, nil
}
func (s *stubSource) Identity(repoPath string) (string, string, error) {
	return s.name, s.email, nil
}
func (s *stubSource) Add(repoPath, branch, worktreePath, remote string) error { return nil }
func (s *stubSource) Remove(repoPath, worktreePath string, force bool) error {
	return nil
}
func (s *stubSource) List(repoPath string) ([]source.WorktreeInfo, error) { return nil, nil }

func detailedFixture() []source.BranchRef {
	return []source.BranchRef{
		{Name: "alice/feat", AuthorName: "Alice", AuthorEmail: "alice@example.com"},
		{Name: "bob/feat", AuthorName: "Bob", AuthorEmail: "bob@example.com"},
		{Name: "main", AuthorName: "T", AuthorEmail: "t@t"},
	}
}

func TestFilterRefs_Unfiltered(t *testing.T) {
	s := &stubSource{refs: []string{"a", "b"}, detailed: detailedFixture()}
	got, err := filterRefs(s, ".", "origin", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "a" {
		t.Errorf("got %v", got)
	}
}

func TestFilterRefs_Mine(t *testing.T) {
	s := &stubSource{detailed: detailedFixture(), name: "Alice", email: "alice@example.com"}
	got, err := filterRefs(s, ".", "origin", true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "alice/feat" {
		t.Errorf("got %v", got)
	}
}

func TestFilterRefs_MineMatchesEitherNameOrEmail(t *testing.T) {
	s := &stubSource{detailed: detailedFixture(), name: "Nobody", email: "BOB@EXAMPLE.COM"}
	got, err := filterRefs(s, ".", "origin", true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "bob/feat" {
		t.Errorf("got %v", got)
	}
}

func TestFilterRefs_MineWithoutIdentityErrors(t *testing.T) {
	s := &stubSource{detailed: detailedFixture()}
	if _, err := filterRefs(s, ".", "origin", true, nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestFilterRefs_AuthorSubstring(t *testing.T) {
	s := &stubSource{detailed: detailedFixture()}
	got, err := filterRefs(s, ".", "origin", false, []string{"bob"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "bob/feat" {
		t.Errorf("got %v", got)
	}
	got, err = filterRefs(s, ".", "origin", false, []string{"EXAMPLE", "nomatch"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("repeatable author should OR, got %v", got)
	}
	got, err = filterRefs(s, ".", "origin", false, []string{"alice,bob"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("comma-separated should split, got %v", got)
	}
}

func TestFilterRefs_MineAndAuthorIntersect(t *testing.T) {
	s := &stubSource{detailed: detailedFixture(), name: "Alice", email: "alice@example.com"}
	got, err := filterRefs(s, ".", "origin", true, []string{"alice"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("got %v", got)
	}
	got, err = filterRefs(s, ".", "origin", true, []string{"bob"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("intersection should be empty, got %v", got)
	}
}
