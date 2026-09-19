package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/mytmlt/wrk3/internal/config"
	"github.com/mytmlt/wrk3/internal/ports"
	"github.com/mytmlt/wrk3/internal/source"
)

// localWorktreesByBranch must skip the main checkout even when git reports
// the resolved path (/private/var/...) while RepoPath keeps the logical
// symlinked form (/var/...), as on macOS with TMPDIR under /var.
func TestLocalWorktreesByBranch_SkipsMainThroughSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink behavior differs on windows")
	}
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestConfig(t, repo)
	link := filepath.Join(dir, "link")
	if err := os.Symlink(repo, link); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(filepath.Join(link, "wrk3.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	r := &resolved{cfg: cfg}
	infos := []source.WorktreeInfo{
		{Path: repo, Branch: "main"}, // git reports the resolved path
		{Path: filepath.Join(dir, "wt1"), Branch: "feature-foo"},
	}
	byBranch := localWorktreesByBranch(r, infos)
	if _, ok := byBranch["main"]; ok {
		t.Errorf("main checkout via symlink was not skipped: %v", byBranch)
	}
	if got := byBranch["feature-foo"]; got == "" {
		t.Errorf("feature-foo worktree missing: %v", byBranch)
	}
}

// adoptListStub serves a fixed worktree list for addOne/addOrAdoptOne tests.
type adoptListStub struct {
	stubSource
	infos []source.WorktreeInfo
}

func (s *adoptListStub) List(repoPath string) ([]source.WorktreeInfo, error) {
	return s.infos, nil
}

// addOrAdoptOne must adopt an existing on-disk worktree instead of
// re-creating it (orphan missing from state after state-file loss).
func TestAddOrAdoptOne_AdoptsOrphan(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	wt := filepath.Join(repo, ".worktrees", "feature-foo")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	r := &resolved{cfg: cfg, src: &adoptListStub{}, base: cfg.AbsWorktreeBase(), stateP: cfg.StatePath()}
	var recs []ports.WorktreeRecord
	alloc := cfg.Allocator()
	byBranch := map[string]string{"feature-foo": wt}
	cmd, _, _ := newCreateTestCmd("")
	res, err := addAdoptOrCreateOne(cmd, r, &recs, &alloc, byBranch, "feature-foo", "origin", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.verb != "adopted" {
		t.Errorf("verb = %q, want adopted for existing worktree", res.verb)
	}
	if len(res.warns) != 0 {
		t.Errorf("warns = %v, want none", res.warns)
	}
	if len(recs) != 1 || recs[0].Branch != "feature-foo" || recs[0].AbsPath != filepath.Clean(wt) {
		t.Errorf("recs = %+v, want adopted feature-foo at %s", recs, wt)
	}
	if recs[0].Index != 1 || recs[0].Ports["app"] != 8001 {
		t.Errorf("recs[0] = %+v, want index 1 app 8001 (index 0 is the main checkout)", recs[0])
	}
}

// addOne must hint at adoption instead of surfacing a raw git failure
// when the branch is already checked out somewhere.
func TestAddOne_HintsAdoptWhenCheckedOut(t *testing.T) {
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	r := &resolved{
		cfg:    cfg,
		src:    &adoptListStub{infos: []source.WorktreeInfo{{Path: "/elsewhere/wt", Branch: "feature-foo"}}},
		base:   cfg.AbsWorktreeBase(),
		stateP: cfg.StatePath(),
	}
	var recs []ports.WorktreeRecord
	alloc := cfg.Allocator()
	if _, err := addOne(r, &recs, &alloc, "feature-foo", "origin"); err == nil {
		t.Fatal("expected checked-out error")
	} else if !strings.Contains(err.Error(), "add --local") {
		t.Errorf("error should hint at add --local, got: %v", err)
	}
}

// createTestSource records Add/AddNew calls for creation-path tests. Like
// real git it creates the worktree directory on success so .env ensure
// has a place to write.
type createTestSource struct {
	stubSource
	infos          []source.WorktreeInfo
	fetchCalls     int
	fetchErr       error
	refsAfterFetch []string
	addCalls       []string
	addNewCalls    []struct{ branch, path, base string }
}

func (s *createTestSource) Fetch(repoPath, remote string) error {
	s.fetchCalls++
	if s.fetchErr != nil {
		return s.fetchErr
	}
	if s.refsAfterFetch != nil {
		s.refs = s.refsAfterFetch
	}
	return nil
}

func (s *createTestSource) Add(repoPath, branch, worktreePath, remote string) error {
	s.addCalls = append(s.addCalls, branch)
	return os.MkdirAll(worktreePath, 0o755)
}

func (s *createTestSource) AddNew(repoPath, branch, worktreePath, base string) error {
	s.addNewCalls = append(s.addNewCalls, struct{ branch, path, base string }{branch, worktreePath, base})
	return os.MkdirAll(worktreePath, 0o755)
}

func (s *createTestSource) List(repoPath string) ([]source.WorktreeInfo, error) {
	return s.infos, nil
}

// newCreateTestCmd builds a bare command with canned stdin and captured
// output for confirm-prompt tests.
func newCreateTestCmd(input string) (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader(input))
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	return cmd, &out, &errBuf
}

func newCreateTestResolved(t *testing.T, src *createTestSource) *resolved {
	t.Helper()
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	return &resolved{cfg: cfg, src: src, base: cfg.AbsWorktreeBase(), stateP: cfg.StatePath()}
}

// withCreateFlags sets the --create/--no-create globals, restoring them
// after the test.
func withCreateFlags(t *testing.T, create, noCreate bool) {
	t.Helper()
	oldCreate, oldNoCreate := addCreate, addNoCreate
	addCreate, addNoCreate = create, noCreate
	t.Cleanup(func() { addCreate, addNoCreate = oldCreate, oldNoCreate })
}

// Unknown branch + "y" creates from the remote default after one refresh
// fetch.
func TestAddAdoptOrCreateOne_CreatesOnConfirm(t *testing.T) {
	withCreateFlags(t, false, false)
	src := &createTestSource{}
	src.base = "main"
	r := newCreateTestResolved(t, src)
	var recs []ports.WorktreeRecord
	alloc := r.cfg.Allocator()
	cmd, out, _ := newCreateTestCmd("y\n")
	res, err := addAdoptOrCreateOne(cmd, r, &recs, &alloc, map[string]string{}, "feature/new", "origin", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.verb != "added" {
		t.Errorf("verb = %q, want added", res.verb)
	}
	if res.note != " (new branch from origin/main)" {
		t.Errorf("note = %q, want base note", res.note)
	}
	if src.fetchCalls != 1 {
		t.Errorf("fetchCalls = %d, want 1 refresh before prompting", src.fetchCalls)
	}
	if len(src.addNewCalls) != 1 {
		t.Fatalf("addNewCalls = %+v, want one creation", src.addNewCalls)
	}
	if got := src.addNewCalls[0]; got.branch != "feature/new" || got.base != "origin/main" {
		t.Errorf("addNew call = %+v, want branch feature/new from origin/main", got)
	}
	if !strings.Contains(out.String(), `branch "feature/new" not found`) {
		t.Errorf("prompt missing from output: %q", out.String())
	}
	if len(recs) != 1 || recs[0].Branch != "feature/new" || recs[0].Slug != "feature-new" {
		t.Errorf("recs = %+v, want created feature/new", recs)
	}
}

// Unknown branch + "n" aborts without side effects.
func TestAddAdoptOrCreateOne_DeclinesOnNo(t *testing.T) {
	withCreateFlags(t, false, false)
	src := &createTestSource{}
	src.base = "main"
	r := newCreateTestResolved(t, src)
	var recs []ports.WorktreeRecord
	alloc := r.cfg.Allocator()
	cmd, _, _ := newCreateTestCmd("n\n")
	if _, err := addAdoptOrCreateOne(cmd, r, &recs, &alloc, map[string]string{}, "feature/new", "origin", false); err == nil {
		t.Fatal("expected cancelled error")
	} else if !strings.Contains(err.Error(), "cancelled") {
		t.Errorf("error = %v, want cancelled", err)
	}
	if len(src.addNewCalls) != 0 || len(recs) != 0 {
		t.Errorf("declined prompt must not create: calls=%+v recs=%+v", src.addNewCalls, recs)
	}
}

// EOF (pipes/CI) counts as "no" and points at --create.
func TestAddAdoptOrCreateOne_EOFMeansNo(t *testing.T) {
	withCreateFlags(t, false, false)
	src := &createTestSource{}
	src.base = "main"
	r := newCreateTestResolved(t, src)
	var recs []ports.WorktreeRecord
	alloc := r.cfg.Allocator()
	cmd, _, _ := newCreateTestCmd("")
	if _, err := addAdoptOrCreateOne(cmd, r, &recs, &alloc, map[string]string{}, "feature/new", "origin", false); err == nil {
		t.Fatal("expected cancelled error on EOF")
	} else if !strings.Contains(err.Error(), "--create") {
		t.Errorf("error = %v, want --create hint", err)
	}
}

// --create skips the prompt entirely (EOF stdin would abort otherwise).
func TestAddAdoptOrCreateOne_CreateFlagSkipsPrompt(t *testing.T) {
	withCreateFlags(t, true, false)
	src := &createTestSource{}
	src.base = "main"
	r := newCreateTestResolved(t, src)
	var recs []ports.WorktreeRecord
	alloc := r.cfg.Allocator()
	cmd, out, _ := newCreateTestCmd("")
	res, err := addAdoptOrCreateOne(cmd, r, &recs, &alloc, map[string]string{}, "feature/new", "origin", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.note != " (new branch from origin/main)" {
		t.Errorf("note = %q, want base note", res.note)
	}
	if strings.Contains(out.String(), "Create new branch") {
		t.Errorf("prompt must be skipped with --create: %q", out.String())
	}
	if len(src.addNewCalls) != 1 {
		t.Errorf("addNewCalls = %+v, want one creation", src.addNewCalls)
	}
}

// --no-create fails fast without prompting, even on "y" input.
func TestAddAdoptOrCreateOne_NoCreateFailsFast(t *testing.T) {
	withCreateFlags(t, false, true)
	src := &createTestSource{}
	src.base = "main"
	r := newCreateTestResolved(t, src)
	var recs []ports.WorktreeRecord
	alloc := r.cfg.Allocator()
	cmd, out, _ := newCreateTestCmd("y\n")
	if _, err := addAdoptOrCreateOne(cmd, r, &recs, &alloc, map[string]string{}, "feature/new", "origin", false); err == nil {
		t.Fatal("expected unknown-branch error")
	} else if !strings.Contains(err.Error(), "use --create") {
		t.Errorf("error = %v, want --create hint", err)
	}
	if strings.Contains(out.String(), "Create new branch") {
		t.Errorf("prompt must be skipped with --no-create: %q", out.String())
	}
	if len(src.addNewCalls) != 0 {
		t.Errorf("addNewCalls = %+v, want none", src.addNewCalls)
	}
}

// A remote branch missed by the stale cache is found after the refresh
// fetch and checked out normally — no prompt, no creation.
func TestAddAdoptOrCreateOne_FetchRevealsRemoteBranch(t *testing.T) {
	withCreateFlags(t, false, false)
	src := &createTestSource{refsAfterFetch: []string{"feature-foo"}}
	src.base = "main"
	r := newCreateTestResolved(t, src)
	var recs []ports.WorktreeRecord
	alloc := r.cfg.Allocator()
	cmd, out, _ := newCreateTestCmd("")
	res, err := addAdoptOrCreateOne(cmd, r, &recs, &alloc, map[string]string{}, "feature-foo", "origin", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.verb != "added" || res.note != "" {
		t.Errorf("res = %+v, want plain added", res)
	}
	if src.addCalls == nil || len(src.addCalls) != 1 {
		t.Errorf("addCalls = %+v, want one checkout", src.addCalls)
	}
	if len(src.addNewCalls) != 0 {
		t.Errorf("addNewCalls = %+v, want none", src.addNewCalls)
	}
	if strings.Contains(out.String(), "Create new branch") {
		t.Errorf("unexpected prompt: %q", out.String())
	}
}

// A known local branch checks out without any fetch or prompt.
func TestAddAdoptOrCreateOne_KnownLocalBranch(t *testing.T) {
	withCreateFlags(t, false, false)
	src := &createTestSource{}
	src.local = []string{"feature-foo"}
	src.base = "main"
	r := newCreateTestResolved(t, src)
	var recs []ports.WorktreeRecord
	alloc := r.cfg.Allocator()
	cmd, _, _ := newCreateTestCmd("")
	res, err := addAdoptOrCreateOne(cmd, r, &recs, &alloc, map[string]string{}, "feature-foo", "origin", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.verb != "added" || res.note != "" {
		t.Errorf("res = %+v, want plain added", res)
	}
	if src.fetchCalls != 0 {
		t.Errorf("fetchCalls = %d, want no fetch for known branches", src.fetchCalls)
	}
}

// Unknown remote default falls back to HEAD with a warning.
func TestAddAdoptOrCreateOne_FallsBackToHEAD(t *testing.T) {
	withCreateFlags(t, true, false)
	src := &createTestSource{}
	r := newCreateTestResolved(t, src)
	var recs []ports.WorktreeRecord
	alloc := r.cfg.Allocator()
	cmd, _, errBuf := newCreateTestCmd("")
	res, err := addAdoptOrCreateOne(cmd, r, &recs, &alloc, map[string]string{}, "feature-new", "origin", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.note != " (new branch from HEAD)" {
		t.Errorf("note = %q, want HEAD fallback note", res.note)
	}
	if len(src.addNewCalls) != 1 || src.addNewCalls[0].base != "HEAD" {
		t.Errorf("addNewCalls = %+v, want base HEAD", src.addNewCalls)
	}
	if !strings.Contains(errBuf.String(), "default branch on origin unknown") {
		t.Errorf("stderr = %q, want HEAD fallback warning", errBuf.String())
	}
}

func TestValidateCreateFlags(t *testing.T) {
	cases := []struct {
		name     string
		create   bool
		noCreate bool
		args     []string
		wantErr  string
	}{
		{name: "no flags", args: nil, wantErr: ""},
		{name: "create with names", create: true, args: []string{"a"}, wantErr: ""},
		{name: "no-create with names", noCreate: true, args: []string{"a"}, wantErr: ""},
		{name: "both flags", create: true, noCreate: true, args: []string{"a"}, wantErr: "either --create or --no-create"},
		{name: "create bare", create: true, args: nil, wantErr: "pass branch names"},
		{name: "no-create bare", noCreate: true, args: nil, wantErr: "pass branch names"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withCreateFlags(t, tc.create, tc.noCreate)
			err := validateCreateFlags(tc.args)
			if tc.wantErr == "" && err != nil {
				t.Errorf("validateCreateFlags(%v) = %v, want nil", tc.args, err)
			}
			if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
				t.Errorf("validateCreateFlags(%v) = %v, want %q", tc.args, err, tc.wantErr)
			}
		})
	}
}

func TestValidateCreateFlags_RejectsBulkModes(t *testing.T) {
	withCreateFlags(t, true, false)
	oldLocal := addLocal
	addLocal = true
	defer func() { addLocal = oldLocal }()
	if err := validateCreateFlags([]string{"a"}); err == nil {
		t.Error("expected error for --create with --local")
	} else if !strings.Contains(err.Error(), "explicit branch names") {
		t.Errorf("error = %v, want explicit-names hint", err)
	}
}
