package cmd

import (
	"testing"

	"github.com/mytmlt/wrk3/internal/source"
)

type stubSource struct {
	refs      []string
	local     []string
	detailed  []source.BranchRef
	history   map[string][]source.BranchRef
	base      string
	name      string
	email     string
	gitStatus map[string]source.WorktreeStatus
	gitErr    map[string]error
}

func (s *stubSource) Fetch(repoPath, remote string) error { return nil }
func (s *stubSource) Refs(repoPath, remote string) ([]string, error) {
	return s.refs, nil
}
func (s *stubSource) LocalBranches(repoPath string) ([]string, error) {
	return s.local, nil
}
func (s *stubSource) RefsDetailed(repoPath, remote string) ([]source.BranchRef, error) {
	return s.detailed, nil
}
func (s *stubSource) DefaultBranch(repoPath, remote string) (string, error) {
	return s.base, nil
}
func (s *stubSource) BranchHistory(repoPath, remote, branch, base string, limit int) ([]source.BranchRef, error) {
	if h, ok := s.history[branch]; ok {
		return h, nil
	}
	return nil, nil
}
func (s *stubSource) Identity(repoPath string) (string, string, error) {
	return s.name, s.email, nil
}
func (s *stubSource) Add(repoPath, branch, worktreePath, remote string) error { return nil }
func (s *stubSource) AddNew(repoPath, branch, worktreePath, base string) error {
	return nil
}
func (s *stubSource) Remove(repoPath, worktreePath string, force bool) error {
	return nil
}
func (s *stubSource) Pull(worktreePath string, opts source.PullOptions) error {
	return nil
}
func (s *stubSource) GitStatus(worktreePath string) (source.WorktreeStatus, error) {
	if s.gitErr != nil {
		if err, ok := s.gitErr[worktreePath]; ok {
			return source.WorktreeStatus{}, err
		}
	}
	if s.gitStatus != nil {
		if st, ok := s.gitStatus[worktreePath]; ok {
			return st, nil
		}
	}
	return source.WorktreeStatus{Clean: true}, nil
}
func (s *stubSource) List(repoPath string) ([]source.WorktreeInfo, error) { return nil, nil }

func detailedFixture() []source.BranchRef {
	return []source.BranchRef{
		{Name: "alice/feat", AuthorName: "Alice", AuthorEmail: "alice@example.com", CommitterName: "Alice", CommitterEmail: "alice@example.com"},
		{Name: "bob/feat", AuthorName: "Bob", AuthorEmail: "bob@example.com", CommitterName: "Bob", CommitterEmail: "bob@example.com"},
		{Name: "cursor/bot-feat", AuthorName: "Cursor Bot", AuthorEmail: "bot@cursor.com", CommitterName: "Alice", CommitterEmail: "alice@example.com"},
		{Name: "main", AuthorName: "T", AuthorEmail: "t@t", CommitterName: "T", CommitterEmail: "t@t"},
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
	if len(got) != 2 || got[0] != "alice/feat" || got[1] != "cursor/bot-feat" {
		t.Errorf("mine should include committer match, got %v", got)
	}
}

func TestFilterRefs_MineMatchesCommitterOnly(t *testing.T) {
	detailed := []source.BranchRef{
		{Name: "cursor/bot-feat", AuthorName: "Cursor Bot", AuthorEmail: "bot@cursor.com", CommitterName: "Ada", CommitterEmail: "ada@example.com"},
	}
	s := &stubSource{detailed: detailed, name: "Ada", email: "ada@example.com"}
	got, err := filterRefs(s, ".", "origin", true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "cursor/bot-feat" {
		t.Errorf("mine should match tip committer, got %v", got)
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
	if len(got) != 3 {
		t.Errorf("repeatable author should OR, got %v", got)
	}
	got, err = filterRefs(s, ".", "origin", false, []string{"alice,bob"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Errorf("comma-separated should split, got %v", got)
	}
}

func TestFilterRefs_AuthorMatchesCommitter(t *testing.T) {
	s := &stubSource{detailed: detailedFixture()}
	got, err := filterRefs(s, ".", "origin", false, []string{"cursor"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "cursor/bot-feat" {
		t.Errorf("author filter should match tip author, got %v", got)
	}
}

func TestFilterRefs_MineAndAuthorIntersect(t *testing.T) {
	s := &stubSource{detailed: detailedFixture(), name: "Alice", email: "alice@example.com"}
	got, err := filterRefs(s, ".", "origin", true, []string{"alice"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("mine+alice should include author and committer matches, got %v", got)
	}
	got, err = filterRefs(s, ".", "origin", true, []string{"bob"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("intersection should be empty, got %v", got)
	}
}

func botTipFixture() []source.BranchRef {
	return []source.BranchRef{
		// Tip is fully bot-owned (author + committer are the bot) —
		// the superplane cursor case from the issue.
		{Name: "cursor/bot-tip", AuthorName: "Cursor Agent", AuthorEmail: "cursoragent@cursor.com", CommitterName: "Cursor Agent", CommitterEmail: "cursoragent@cursor.com"},
		{Name: "other/bot-tip", AuthorName: "Cursor Agent", AuthorEmail: "cursoragent@cursor.com", CommitterName: "Cursor Agent", CommitterEmail: "cursoragent@cursor.com"},
	}
}

func TestFilterRefs_MineMatchesHistory(t *testing.T) {
	history := map[string][]source.BranchRef{
		"cursor/bot-tip": {
			{Name: "cursor/bot-tip", AuthorName: "Cursor Agent", AuthorEmail: "cursoragent@cursor.com", CommitterName: "Cursor Agent", CommitterEmail: "cursoragent@cursor.com"},
			{Name: "cursor/bot-tip", AuthorName: "Cursor Agent", AuthorEmail: "cursoragent@cursor.com", CommitterName: "Ada", CommitterEmail: "ada@example.com"},
		},
		"other/bot-tip": {
			{Name: "other/bot-tip", AuthorName: "Cursor Agent", AuthorEmail: "cursoragent@cursor.com", CommitterName: "Cursor Agent", CommitterEmail: "cursoragent@cursor.com"},
		},
	}
	s := &stubSource{detailed: botTipFixture(), history: history, base: "main", name: "Ada", email: "ada@example.com"}
	got, err := filterRefs(s, ".", "origin", true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "cursor/bot-tip" {
		t.Errorf("mine should match branch-exclusive history, got %v", got)
	}
}

func TestFilterRefs_MineHistoryDisabledWithoutBase(t *testing.T) {
	history := map[string][]source.BranchRef{
		"cursor/bot-tip": {
			{Name: "cursor/bot-tip", AuthorName: "Cursor Agent", AuthorEmail: "cursoragent@cursor.com", CommitterName: "Ada", CommitterEmail: "ada@example.com"},
		},
	}
	// No base: history must not be consulted (otherwise mainline commits
	// would flag every branch). Tip-only => empty.
	s := &stubSource{detailed: botTipFixture(), history: history, base: "", name: "Ada", email: "ada@example.com"}
	got, err := filterRefs(s, ".", "origin", true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("without base, mine must stay tip-only, got %v", got)
	}
}

func TestFilterRefs_AuthorMatchesHistory(t *testing.T) {
	history := map[string][]source.BranchRef{
		"cursor/bot-tip": {
			{Name: "cursor/bot-tip", AuthorName: "Someone", AuthorEmail: "s@s", CommitterName: "Someone", CommitterEmail: "s@s"},
			{Name: "cursor/bot-tip", AuthorName: "Ada Lovelace", AuthorEmail: "ada@example.com", CommitterName: "Ada Lovelace", CommitterEmail: "ada@example.com"},
		},
	}
	s := &stubSource{detailed: botTipFixture(), history: history, base: "main"}
	got, err := filterRefs(s, ".", "origin", false, []string{"ada"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "cursor/bot-tip" {
		t.Errorf("author should match branch-exclusive history, got %v", got)
	}
}
