package source

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseGitStatusPorcelain_Clean(t *testing.T) {
	st := parseGitStatusPorcelain("## main...origin/main\n")
	if st.Branch != "main" {
		t.Errorf("Branch = %q, want main", st.Branch)
	}
	if !st.Clean {
		t.Errorf("Clean = false, want true: %+v", st)
	}
	if st.Ahead != 0 || st.Behind != 0 {
		t.Errorf("ahead/behind = %d/%d, want 0/0", st.Ahead, st.Behind)
	}
}

func TestParseGitStatusPorcelain_AheadBehind(t *testing.T) {
	st := parseGitStatusPorcelain("## main...origin/main [ahead 2, behind 1]\n")
	if st.Branch != "main" || st.Ahead != 2 || st.Behind != 1 {
		t.Errorf("got %+v, want main ahead 2 behind 1", st)
	}
	st = parseGitStatusPorcelain("## feat...origin/feat [ahead 3]\n")
	if st.Ahead != 3 || st.Behind != 0 {
		t.Errorf("got %+v, want ahead 3", st)
	}
	st = parseGitStatusPorcelain("## feat...origin/feat [gone]\n")
	if st.Branch != "feat" || st.Ahead != 0 || st.Behind != 0 {
		t.Errorf("gone should keep branch with 0/0, got %+v", st)
	}
}

func TestParseGitStatusPorcelain_Detached(t *testing.T) {
	st := parseGitStatusPorcelain("## HEAD (no branch)\n?? new.txt\n")
	if st.Branch != "" {
		t.Errorf("Branch = %q, want empty for detached", st.Branch)
	}
	if st.Clean || st.Untracked != 1 {
		t.Errorf("got %+v, want untracked 1", st)
	}
}

func TestParseGitStatusPorcelain_Counts(t *testing.T) {
	out := "## main\nM  staged.txt\n M unstaged.txt\nMM both.txt\nA  added.txt\n?? untracked.txt\n"
	st := parseGitStatusPorcelain(out)
	if st.Clean {
		t.Error("Clean = true, want false")
	}
	// staged: M_, MM, A_ = 3; unstaged: _M, MM = 2; untracked 1.
	if st.Staged != 3 {
		t.Errorf("Staged = %d, want 3: %+v", st.Staged, st)
	}
	if st.Unstaged != 2 {
		t.Errorf("Unstaged = %d, want 2: %+v", st.Unstaged, st)
	}
	if st.Untracked != 1 {
		t.Errorf("Untracked = %d, want 1: %+v", st.Untracked, st)
	}
	if len(st.Files) != 5 {
		t.Errorf("Files = %d, want 5: %v", len(st.Files), st.Files)
	}
}

func TestGitStatus_RealRepo(t *testing.T) {
	repo := initRepo(t)
	src := &GitSource{}
	st, err := src.GitStatus(repo)
	if err != nil {
		t.Fatalf("GitStatus: %v", err)
	}
	if st.Branch != "main" || !st.Clean {
		t.Errorf("fresh repo: got %+v, want main clean", st)
	}
	// Untracked file.
	if err := os.WriteFile(filepath.Join(repo, "new.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err = src.GitStatus(repo)
	if err != nil {
		t.Fatalf("GitStatus: %v", err)
	}
	if st.Clean || st.Untracked != 1 {
		t.Errorf("untracked: got %+v", st)
	}
	// Modification (unstaged) plus staged file.
	if err := os.WriteFile(filepath.Join(repo, "f.txt"), []byte("dirty"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "new.txt")
	st, err = src.GitStatus(repo)
	if err != nil {
		t.Fatalf("GitStatus: %v", err)
	}
	if st.Staged != 1 || st.Unstaged != 1 || st.Untracked != 0 {
		t.Errorf("staged+unstaged: got %+v", st)
	}
}

func TestGitStatus_AheadBehindRealRepo(t *testing.T) {
	origin := t.TempDir()
	runGit(t, origin, "init", "--bare")
	seed := initRepo(t)
	runGit(t, seed, "remote", "add", "origin", origin)
	runGit(t, seed, "push", "-u", "origin", "main")
	wt := cloneRepo(t, origin)
	src := &GitSource{}
	// Local commit ahead of origin/main.
	if err := os.WriteFile(filepath.Join(wt, "ahead.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, wt, "add", ".")
	runGit(t, wt, "commit", "-m", "ahead")
	st, err := src.GitStatus(wt)
	if err != nil {
		t.Fatalf("GitStatus: %v", err)
	}
	if st.Ahead != 1 {
		t.Errorf("Ahead = %d, want 1: %+v", st.Ahead, st)
	}
	if !st.Clean {
		t.Errorf("committed ahead should be clean: %+v", st)
	}
}

func TestGitStatus_EmptyPathErrors(t *testing.T) {
	src := &GitSource{}
	if _, err := src.GitStatus(""); err == nil {
		t.Error("expected error for empty path")
	}
	if _, err := src.GitStatus("   "); err == nil {
		t.Error("expected error for blank path")
	}
}

func TestGitStatus_NonRepoErrors(t *testing.T) {
	src := &GitSource{}
	if _, err := src.GitStatus(t.TempDir()); err == nil {
		t.Error("expected error for non-repo dir")
	}
}
