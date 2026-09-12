package source

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func pushAs(t *testing.T, dir, branch, name, email string) {
	t.Helper()
	runGit(t, dir, "checkout", "-qb", branch)
	f, err := os.OpenFile(filepath.Join(dir, "f.txt"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("\n" + branch); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	cmd := exec.Command("git", "commit", "-m", branch)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME="+name, "GIT_AUTHOR_EMAIL="+email,
		"GIT_COMMITTER_NAME="+name, "GIT_COMMITTER_EMAIL="+email,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit in %s: %v: %s", dir, err, out)
	}
	runGit(t, dir, "push", "origin", branch)
}

func TestRefsDetailed_AuthorsAndSkips(t *testing.T) {
	origin := t.TempDir()
	runGit(t, origin, "init", "--bare")
	repo := initRepo(t)
	runGit(t, repo, "remote", "add", "origin", origin)
	runGit(t, repo, "push", "origin", "main")
	pushAs(t, repo, "alice/feat", "Alice", "alice@example.com")
	runGit(t, repo, "checkout", "-q", "main")
	pushAs(t, repo, "bob/feat", "Bob", "bob@example.com")

	src := &GitSource{}
	if err := src.Fetch(repo, "origin"); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	refs, err := src.RefsDetailed(repo, "origin")
	if err != nil {
		t.Fatalf("RefsDetailed: %v", err)
	}
	byName := map[string]BranchRef{}
	for _, r := range refs {
		byName[r.Name] = r
	}
	for _, bad := range []string{"", "origin", "HEAD"} {
		if _, ok := byName[bad]; ok {
			t.Errorf("RefsDetailed should skip %q: %+v", bad, refs)
		}
	}
	al, ok := byName["alice/feat"]
	if !ok {
		t.Fatalf("missing alice/feat in %+v", refs)
	}
	if al.AuthorName != "Alice" || al.AuthorEmail != "alice@example.com" {
		t.Errorf("alice author wrong: %+v", al)
	}
	bo, ok := byName["bob/feat"]
	if !ok {
		t.Fatalf("missing bob/feat in %+v", refs)
	}
	if bo.AuthorName != "Bob" || bo.AuthorEmail != "bob@example.com" {
		t.Errorf("bob author wrong: %+v", bo)
	}
}

func TestIdentity_ReadsConfig(t *testing.T) {
	repo := initRepo(t)
	runGit(t, repo, "config", "user.name", "Ada")
	runGit(t, repo, "config", "user.email", "ada@example.com")
	src := &GitSource{}
	name, email, err := src.Identity(repo)
	if err != nil {
		t.Fatalf("Identity: %v", err)
	}
	if name != "Ada" || email != "ada@example.com" {
		t.Errorf("Identity = %q %q", name, email)
	}
}

func TestIdentity_UnsetEmpty(t *testing.T) {
	// Isolate from the developer's real ~/.gitconfig: the empty HOME
	// means no global identity, and --unset clears the repo-local one.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "nonexistent"))
	t.Setenv("GIT_CONFIG_SYSTEM", filepath.Join(t.TempDir(), "nonexistent"))
	repo := initRepo(t)
	runGit(t, repo, "config", "--unset", "user.name")
	runGit(t, repo, "config", "--unset", "user.email")
	src := &GitSource{}
	name, email, err := src.Identity(repo)
	if err != nil {
		t.Fatalf("Identity: %v", err)
	}
	if name != "" || email != "" {
		t.Errorf("expected empty identity, got %q %q", name, email)
	}
}

func TestParseRefsDetailed(t *testing.T) {
	out := "origin\x00t\x00<t@t>\n" +
		"origin/alice/feat\x00Alice\x00<alice@example.com>\n" +
		"origin/HEAD\x00t\x00<t@t>\n" +
		"broken-line-without-separators\n"
	refs := parseRefsDetailed(out, "origin")
	if len(refs) != 1 {
		t.Fatalf("got %+v", refs)
	}
	if refs[0].Name != "alice/feat" || refs[0].AuthorName != "Alice" || refs[0].AuthorEmail != "alice@example.com" {
		t.Errorf("wrong parse: %+v", refs[0])
	}
}

func TestParseRefs_FiltersByRemote(t *testing.T) {
	out := "origin/main\n  origin/alice/feat\n  upstream/bob/feat\n  upstream/HEAD\n  origin\n  upstream\n"
	got := parseRefs(out, "upstream")
	if len(got) != 1 || got[0] != "bob/feat" {
		t.Fatalf("upstream parse = %v", got)
	}
	got = parseRefs(out, "origin")
	if len(got) != 2 || got[0] != "main" || got[1] != "alice/feat" {
		t.Fatalf("origin parse = %v", got)
	}
	// Empty remote defaults to origin.
	got = parseRefs(out, "")
	if len(got) != 2 {
		t.Fatalf("default parse = %v", got)
	}
}

func TestParseRefsDetailed_CustomRemote(t *testing.T) {
	out := "upstream\x00t\x00<t@t>\n" +
		"upstream/bob/feat\x00Bob\x00<bob@example.com>\n" +
		"upstream/HEAD\x00t\x00<t@t>\n"
	refs := parseRefsDetailed(out, "upstream")
	if len(refs) != 1 || refs[0].Name != "bob/feat" {
		t.Fatalf("got %+v", refs)
	}
	// Wrong remote yields nothing.
	if refs := parseRefsDetailed(out, "origin"); len(refs) != 0 {
		t.Fatalf("origin parse of upstream refs = %+v", refs)
	}
}

func TestFetchAndRefs_CustomRemote(t *testing.T) {
	upstream := t.TempDir()
	runGit(t, upstream, "init", "--bare")
	repo := initRepo(t)
	runGit(t, repo, "remote", "add", "upstream", upstream)
	runGit(t, repo, "branch", "local-only")
	runGit(t, repo, "push", "upstream", "main")
	runGit(t, repo, "push", "upstream", "local-only")

	src := &GitSource{}
	if err := src.Fetch(repo, "upstream"); err != nil {
		t.Fatalf("Fetch upstream: %v", err)
	}
	refs, err := src.Refs(repo, "upstream")
	if err != nil {
		t.Fatalf("Refs upstream: %v", err)
	}
	has := map[string]bool{}
	for _, r := range refs {
		has[r] = true
	}
	for _, want := range []string{"main", "local-only"} {
		if !has[want] {
			t.Errorf("Refs upstream = %v, missing %q", refs, want)
		}
	}
}
