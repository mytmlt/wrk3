package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/mytmlt/wrk3/internal/ports"
)

// chdirRepo creates a temp git repo with a wrk3.yaml, cds into it, and
// returns the loaded state path for seeding. Callers must not parallelize.
func chdirRepo(t *testing.T) string {
	t.Helper()
	repo := initMainTestRepo(t)
	cfg := writeTestConfig(t, repo)
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	oldFile := fileFlag
	fileFlag = ""
	t.Cleanup(func() { fileFlag = oldFile })
	return cfg.StatePath()
}

func seedState(t *testing.T, statePath string, recs []ports.WorktreeRecord) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ports.Save(statePath, recs); err != nil {
		t.Fatal(err)
	}
}

func managedRec(t *testing.T, wt, branch, slug string, index, app int) ports.WorktreeRecord {
	t.Helper()
	return ports.WorktreeRecord{
		Branch: branch, Slug: slug,
		AbsPath: filepath.Join(wt, slug),
		Index:   index, Ports: map[string]int{"app": app},
		ComposeProject: "demo-" + slug,
	}
}

func completionValues(t *testing.T, args ...string) (lines []string, directive string) {
	t.Helper()
	out, _, err := executeCmd(args...)
	if err != nil {
		t.Fatalf("__complete %v: %v", args, err)
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "Completion ended with directive:") {
			if strings.HasPrefix(line, "Completion ended with directive:") {
				directive = line
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			directive = line
			continue
		}
		lines = append(lines, line)
	}
	return lines, directive
}

func hasValue(lines []string, want string) bool {
	for _, l := range lines {
		if v := strings.SplitN(l, "\t", 2)[0]; v == want {
			return true
		}
	}
	return false
}

// Subcommand (command-name) completion comes from cobra itself and must
// stay prefix-filtered, including nested groups like proxy and project.
func TestComplete_Subcommands(t *testing.T) {
	lines, _ := completionValues(t, "__complete", "")
	for _, want := range []string{"add", "checkout", "up", "down", "proxy", "project"} {
		if !hasValue(lines, want) {
			t.Errorf("__complete empty missing %q (got %v)", want, lines)
		}
	}
	lines, _ = completionValues(t, "__complete", "ch")
	if !hasValue(lines, "checkout") {
		t.Errorf("__complete ch missing checkout (got %v)", lines)
	}
	if hasValue(lines, "up") {
		t.Errorf("__complete ch unexpectedly contains up (got %v)", lines)
	}
	lines, _ = completionValues(t, "__complete", "proxy", "")
	for _, want := range []string{"up", "down", "status", "open", "hosts-sync"} {
		if !hasValue(lines, want) {
			t.Errorf("__complete proxy missing %q (got %v)", want, lines)
		}
	}
	lines, _ = completionValues(t, "__complete", "proxy", "o")
	if !hasValue(lines, "open") || hasValue(lines, "up") {
		t.Errorf("__complete proxy o = %v, want only open", lines)
	}
	lines, _ = completionValues(t, "__complete", "project", "")
	if !hasValue(lines, "ls") {
		t.Errorf("__complete project missing ls (got %v)", lines)
	}
}

// Worktree position completes both branch names and slugs,
// prefix-filtered on what was typed.
func TestComplete_WorktreesBranchAndSlug(t *testing.T) {
	sp := chdirRepo(t)
	wt := t.TempDir()
	seedState(t, sp, []ports.WorktreeRecord{
		managedRec(t, wt, "feat/foo", "feat-foo", 1, 8001),
	})

	// Branch prefix suggests the branch name.
	lines, _ := completionValues(t, "__complete", "checkout", "feat/")
	if !hasValue(lines, "feat/foo") {
		t.Errorf("checkout feat/ missing branch feat/foo (got %v)", lines)
	}
	// Slug prefix suggests the slug.
	lines, _ = completionValues(t, "__complete", "checkout", "feat-f")
	if !hasValue(lines, "feat-foo") {
		t.Errorf("checkout feat-f missing slug feat-foo (got %v)", lines)
	}
	// Unrelated prefix suggests nothing (but stays silent, no file spam).
	lines, _ = completionValues(t, "__complete", "checkout", "zzz")
	if len(lines) != 0 {
		t.Errorf("checkout zzz = %v, want empty", lines)
	}
}

// Already-typed worktrees are not re-suggested for multi-value commands.
func TestComplete_WorktreesExcludeTyped(t *testing.T) {
	sp := chdirRepo(t)
	wt := t.TempDir()
	seedState(t, sp, []ports.WorktreeRecord{
		managedRec(t, wt, "aaa", "aaa", 1, 8001),
		managedRec(t, wt, "aab", "aab", 2, 8002),
	})
	lines, _ := completionValues(t, "__complete", "up", "aaa", "")
	if hasValue(lines, "aaa") {
		t.Errorf("up with aaa typed still suggests aaa (got %v)", lines)
	}
	if !hasValue(lines, "aab") {
		t.Errorf("up with aaa typed missing aab (got %v)", lines)
	}
}

// remove must not complete the implicit main checkout (it refuses main
// instead of reporting "unknown", so suggesting it would mislead).
func TestComplete_RemoveExcludesMain(t *testing.T) {
	sp := chdirRepo(t)
	wt := t.TempDir()
	seedState(t, sp, []ports.WorktreeRecord{
		managedRec(t, wt, "feat/x", "feat-x", 1, 8001),
	})
	lines, _ := completionValues(t, "__complete", "remove", "")
	if !hasValue(lines, "feat/x") {
		t.Errorf("remove missing managed worktree feat/x (got %v)", lines)
	}
	if hasValue(lines, "main") {
		t.Errorf("remove suggests main (got %v)", lines)
	}
}

// exec completes the worktree only in first position; after that the
// inner command keeps normal file/command completion.
func TestComplete_ExecFirstOnly(t *testing.T) {
	sp := chdirRepo(t)
	wt := t.TempDir()
	seedState(t, sp, []ports.WorktreeRecord{
		managedRec(t, wt, "feat/x", "feat-x", 1, 8001),
	})
	lines, _ := completionValues(t, "__complete", "exec", "")
	if !hasValue(lines, "feat/x") {
		t.Errorf("exec first position missing feat/x (got %v)", lines)
	}
	_, directive := completionValues(t, "__complete", "exec", "feat/x", "")
	if !strings.Contains(directive, "ShellCompDirectiveDefault") && directive != ":0" {
		t.Errorf("exec second position directive = %q, want file completion enabled (:0)", directive)
	}
	if fn := execCmd.ValidArgsFunction; fn == nil {
		t.Fatal("exec ValidArgsFunction is nil")
	} else if _, d := fn(execCmd, []string{"feat/x"}, ""); d != cobra.ShellCompDirectiveDefault {
		t.Errorf("completeWorktreesFirstOnly past first arg = %v, want Default", d)
	}
}

// Commands taking no positional args must suppress file completion.
func TestComplete_NoArgsSuppressFiles(t *testing.T) {
	cmds := map[string]*cobra.Command{
		"status": statusCmd, "fetch": fetchCmd, "skill": skillCmd,
		"version": versionCmd, "update": updateCmd, "ls": lsCmd,
		"log": logCmd, "dashboard": dashboardCmd,
		"project": projectCmd, "project ls": projectLsCmd,
		"proxy": proxyCmd, "proxy up": proxyUpCmd, "proxy down": proxyDownCmd,
		"proxy status": proxyStatusCmd, "proxy hosts-sync": proxyHostsSyncCmd,
	}
	for name, c := range cmds {
		if c.ValidArgsFunction == nil {
			t.Errorf("%s: ValidArgsFunction is nil (TAB would complete files)", name)
			continue
		}
		if _, d := c.ValidArgsFunction(c, nil, ""); d != cobra.ShellCompDirectiveNoFileComp {
			t.Errorf("%s: directive = %v, want NoFileComp", name, d)
		}
	}
}

// Branch completion for add degrades silently outside a repo.
func TestComplete_AddOutsideRepo(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	oldFile := fileFlag
	fileFlag = ""
	defer func() { fileFlag = oldFile }()
	if out, d := completeRemoteBranches(addCmd, nil, ""); len(out) != 0 || d != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("completeRemoteBranches outside repo = (%v, %v), want (empty, NoFileComp)", out, d)
	}
}
