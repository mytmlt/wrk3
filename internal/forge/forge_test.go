package forge

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestClassify(t *testing.T) {
	cases := map[string]string{
		"git@github.com:owner/repo.git":          KindGitHub,
		"git@github.com:OWNER/REPO":              KindGitHub,
		"https://github.com/owner/repo.git":      KindGitHub,
		"https://github.com/owner/repo":          KindGitHub,
		"ssh://git@github.com/owner/repo.git":    KindGitHub,
		"https://ghe.example.ghe.com/o/r.git":    KindGitHub,
		"git@gitlab.com:owner/repo.git":          KindGitLab,
		"https://gitlab.com/owner/repo.git":      KindGitLab,
		"https://git.example.com:2222/o/r.git":   KindUnknown,
		"https://selfhosted.example.com/o/r.git": KindUnknown,
		"https://gitea.example.com/owner/repo":   KindGitea,
		"git@gitea.example.com:owner/repo.git":   KindGitea,
		"":                                       KindUnknown,
		"not-a-url":                              KindUnknown,
		"/local/path/repo.git":                   KindUnknown,
	}
	for raw, want := range cases {
		if got := Classify(raw); got != want {
			t.Errorf("Classify(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestParseGitHubPRs(t *testing.T) {
	out := `[
	  {"number": 12, "title": "Fix login", "url": "https://github.com/o/r/pull/12", "headRefName": "fix-login"},
	  {"number": 7, "title": "WIP", "url": "https://github.com/o/r/pull/7", "headRefName": ""},
	  {"number": 3, "title": "Docs", "url": "https://github.com/o/r/pull/3", "headRefName": "docs/touch"}
	]`
	prs, err := parseGitHubPRs(out)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(prs) != 2 {
		t.Fatalf("got %d prs, want 2 (empty headRefName skipped)", len(prs))
	}
	if prs[0].Branch != "fix-login" || prs[0].Number != 12 {
		t.Errorf("first pr = %+v, want fix-login #12", prs[0])
	}
	if prs, err := parseGitHubPRs(""); err != nil || len(prs) != 0 {
		t.Errorf("empty output = %v, %v; want empty, nil", prs, err)
	}
	if _, err := parseGitHubPRs("not json"); err == nil {
		t.Error("invalid json: want error")
	}
}

func TestGitHubForgeMissingGH(t *testing.T) {
	g := &GitHubForge{
		LookPath: func(string) (string, error) { return "", errors.New("not found") },
	}
	_, err := g.MyPRBranches(context.Background(), t.TempDir(), "origin")
	if err == nil || !strings.Contains(err.Error(), "gh auth login") {
		t.Fatalf("missing gh err = %v, want install/auth guidance", err)
	}
}

func TestGitHubForgeAuthErr(t *testing.T) {
	g := &GitHubForge{
		LookPath: func(s string) (string, error) { return "/bin/gh", nil },
		Run: func(ctx context.Context, dir, name string, args ...string) (string, error) {
			return "", errors.New("To use GitHub CLI in a script, ... not logged into any GitHub hosts ... gh auth login")
		},
	}
	_, err := g.MyPRBranches(context.Background(), t.TempDir(), "origin")
	if err == nil || !strings.Contains(err.Error(), "gh auth login") {
		t.Fatalf("auth err = %v, want auth guidance", err)
	}
}

func TestGitHubForgeSuccess(t *testing.T) {
	var gotDir, gotSearch string
	g := &GitHubForge{
		LookPath: func(s string) (string, error) { return "/bin/gh", nil },
		Run: func(ctx context.Context, dir, name string, args ...string) (string, error) {
			gotDir = dir
			for i, a := range args {
				if a == "--search" && i+1 < len(args) {
					gotSearch = args[i+1]
				}
			}
			return `[{"number":1,"title":"T","url":"https://github.com/o/r/pull/1","headRefName":"feat/x"}]`, nil
		},
	}
	dir := t.TempDir()
	prs, err := g.MyPRBranches(context.Background(), dir, "origin")
	if err != nil {
		t.Fatalf("MyPRBranches: %v", err)
	}
	if len(prs) != 1 || prs[0].Branch != "feat/x" {
		t.Fatalf("prs = %+v, want [feat/x]", prs)
	}
	if gotDir != dir {
		t.Errorf("gh cwd = %q, want repo dir %q", gotDir, dir)
	}
	if !strings.Contains(gotSearch, "involves:@me") {
		t.Errorf("search = %q, want involves:@me query", gotSearch)
	}
}

func TestRegistry(t *testing.T) {
	if _, err := Resolve(KindGitHub); err != nil {
		t.Fatalf("resolve github: %v", err)
	}
	if _, err := Resolve("nosuchforge"); err == nil {
		t.Fatal("unknown forge: want error")
	}
	found := false
	for _, a := range Available() {
		if a == KindGitHub {
			found = true
		}
	}
	if !found {
		t.Errorf("Available() = %v, want github listed", Available())
	}
}
