package cmd

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/mytmlt/wrk3/internal/config"
	"github.com/mytmlt/wrk3/internal/ports"
)

// stateRecordsForCompletion loads worktree records for shell completion.
// It never fails hard: outside a repo or with a broken config it returns
// nil so TAB completion degrades to nothing instead of spamming errors.
func stateRecordsForCompletion() []ports.WorktreeRecord {
	path, err := ResolveConfigPath()
	if err != nil || path == "" {
		return nil
	}
	cfg, err := config.Load(path)
	if err != nil {
		return nil
	}
	recs, err := ports.Load(cfg.StatePath())
	if err != nil {
		return nil
	}
	return recs
}

// completeWorktrees completes existing worktree branch names (with the
// slug as description), including the implicit main checkout.
// Used by up/down/logs/exec/remove.
func completeWorktrees(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	recs := stateRecordsForCompletion()
	// Best-effort main checkout (never fails hard for completion).
	if path, err := ResolveConfigPath(); err == nil && path != "" {
		if cfg, err := config.Load(path); err == nil {
			if src, err := newSource(cfg); err == nil {
				r := &resolved{cfg: cfg, src: src}
				if main, err := mainRecord(r, recs); err == nil && main != nil {
					recs = append(recs, *main)
				}
			}
		}
	}
	var out []string
	for _, rec := range recs {
		for _, cand := range []string{rec.Branch, rec.Slug} {
			if cand == "" || !strings.HasPrefix(cand, toComplete) {
				continue
			}
			out = append(out, cobra.CompletionWithDesc(cand, rec.Slug))
		}
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}

// completeWorktreesFirstOnly completes only the first positional arg
// (the branch); further args are the inner command, not worktrees.
func completeWorktreesFirstOnly(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return completeWorktrees(cmd, args, toComplete)
}

// remoteBranchesForCompletion lists remote branches minus already-added
// worktrees. No network fetch here — completion must stay fast and
// offline-friendly (uses the last fetched refs).
func remoteBranchesForCompletion(cmd *cobra.Command) []string {
	path, err := ResolveConfigPath()
	if err != nil || path == "" {
		return nil
	}
	cfg, err := config.Load(path)
	if err != nil {
		return nil
	}
	src, err := newSource(cfg)
	if err != nil {
		return nil
	}
	remote := cfg.EffectiveRemote()
	if cmd != nil {
		if v, err := cmd.Flags().GetString("remote"); err == nil && strings.TrimSpace(v) != "" {
			remote = strings.TrimSpace(v)
		}
	}
	refs, err := src.Refs(cfg.RepoPath(), remote)
	if err != nil {
		return nil
	}
	taken := map[string]struct{}{}
	if recs, err := ports.Load(cfg.StatePath()); err == nil {
		for _, rec := range recs {
			taken[rec.Branch] = struct{}{}
			taken[rec.Slug] = struct{}{}
		}
	}
	var out []string
	for _, ref := range refs {
		if _, ok := taken[ref]; !ok {
			out = append(out, ref)
		}
	}
	return out
}

// completeRemoteBranches completes remote branch names for add.
func completeRemoteBranches(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	var out []string
	for _, ref := range remoteBranchesForCompletion(cmd) {
		if strings.HasPrefix(ref, toComplete) {
			out = append(out, ref)
		}
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}

// completeAddBranches completes add targets: local worktree branches when
// --local is set, remote branches otherwise.
func completeAddBranches(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if local, _ := cmd.Flags().GetBool("local"); local {
		return completeLocalWorktreeBranches(cmd, args, toComplete)
	}
	return completeRemoteBranches(cmd, args, toComplete)
}

// completeProjectNames completes registry project names for ls --project.
// Never fails hard: with no registry it degrades to nothing.
func completeProjectNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	store, err := newProjectStore()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	projects, err := store.List()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var out []string
	for _, p := range projects {
		if strings.HasPrefix(p.Name, toComplete) {
			out = append(out, cobra.CompletionWithDesc(p.Name, p.ConfigPath))
		}
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}

// completeLocalWorktreeBranches completes branches that have local git
// worktrees but aren't registered in wrk3 state yet (for add --local).
func completeLocalWorktreeBranches(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	path, err := ResolveConfigPath()
	if err != nil || path == "" {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	cfg, err := config.Load(path)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	src, err := newSource(cfg)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	infos, err := src.List(cfg.RepoPath())
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	r := &resolved{cfg: cfg, src: src}
	byBranch := localWorktreesByBranch(r, infos)
	taken := map[string]struct{}{}
	if recs, err := ports.Load(cfg.StatePath()); err == nil {
		for _, rec := range recs {
			taken[rec.Branch] = struct{}{}
		}
	}
	var out []string
	for branch := range byBranch {
		if _, ok := taken[branch]; ok {
			continue
		}
		if strings.HasPrefix(branch, toComplete) {
			out = append(out, branch)
		}
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}

// completeRemotes completes --remote flag values via `git remote`.
// Offline-safe and never fails hard: degrades to nothing outside a repo.
func completeRemotes(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	path, err := ResolveConfigPath()
	if err != nil || path == "" {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	cfg, err := config.Load(path)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, "git", "-C", cfg.RepoPath(), "remote")
	out, err := c.Output()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var names []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && strings.HasPrefix(line, toComplete) {
			names = append(names, line)
		}
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}
