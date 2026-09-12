package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/mytmlt/wrk3/internal/config"
	"github.com/mytmlt/wrk3/internal/ports"
	"github.com/mytmlt/wrk3/internal/runner"
	"github.com/mytmlt/wrk3/internal/source"
)

// resolved holds a loaded config + backends.
type resolved struct {
	cfg    *config.Config
	src    source.Source
	base   string
	stateP string
}

// resolveConfig resolves -f/--file (or upward scan) and loads wrk3.yaml.
func resolveConfig() (*resolved, error) {
	path, err := ResolveConfigPath()
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(path)
	if err != nil {
		return nil, fmt.Errorf("load config %q: %w", path, err)
	}
	src, err := newSource(cfg)
	if err != nil {
		return nil, err
	}
	base := cfg.AbsWorktreeBase()
	return &resolved{
		cfg:    cfg,
		src:    src,
		base:   base,
		stateP: cfg.StatePath(),
	}, nil
}

// newSource builds the Source backend via registry (interface-only).
func newSource(cfg *config.Config) (source.Source, error) {
	f, err := source.Resolve(cfg.Source.Type)
	if err != nil {
		return nil, err
	}
	return f(), nil
}

// newRunner builds the Runner backend for slug via registry.
// Docker runners carry compose files + prefix + slug; other backends
// use their registered factory as-is (stubs return not implemented).
func newRunner(cfg *config.Config, slug string) (runner.Runner, error) {
	if cfg.Runner.Type == "docker" {
		return runner.New(cfg.ComposeOptions(slug)), nil
	}
	f, err := runner.Resolve(cfg.Runner.Type)
	if err != nil {
		return nil, err
	}
	return f(), nil
}

// loadState reads the state file for cfg (missing -> empty).
func loadState(r *resolved) ([]ports.WorktreeRecord, error) {
	recs, err := ports.Load(r.stateP)
	if err != nil {
		return nil, fmt.Errorf("load state %q: %w", r.stateP, err)
	}
	return recs, nil
}

// saveState writes records to the state file.
func saveState(r *resolved, recs []ports.WorktreeRecord) error {
	if err := ports.Save(r.stateP, recs); err != nil {
		return fmt.Errorf("save state %q: %w", r.stateP, err)
	}
	return nil
}

// nextIndex returns max(index)+1, or 0 when empty.
func nextIndex(recs []ports.WorktreeRecord) int {
	max := -1
	for _, rec := range recs {
		if rec.Index > max {
			max = rec.Index
		}
	}
	return max + 1
}

// findRecord matches a worktree by branch name or slug.
func findRecord(recs []ports.WorktreeRecord, branchOrSlug string) *ports.WorktreeRecord {
	for i := range recs {
		if recs[i].Branch == branchOrSlug || recs[i].Slug == branchOrSlug {
			return &recs[i]
		}
	}
	return nil
}

// resolveTargets filters records by explicit branches; empty args means all
// (docker compose style: bare up/down applies to every worktree).
func resolveTargets(recs []ports.WorktreeRecord, args []string) ([]ports.WorktreeRecord, error) {
	if len(args) == 0 {
		if len(recs) == 0 {
			return nil, fmt.Errorf("no worktrees registered")
		}
		out := append([]ports.WorktreeRecord(nil), recs...)
		sort.Slice(out, func(i, j int) bool { return out[i].Branch < out[j].Branch })
		return out, nil
	}
	var out []ports.WorktreeRecord
	for _, a := range args {
		rec := findRecord(recs, a)
		if rec == nil {
			return nil, fmt.Errorf("unknown worktree %q (see status)", a)
		}
		out = append(out, *rec)
	}
	return out, nil
}

// resolveTargetsRequired is like resolveTargets but bare args is an error
// (used by remove to avoid nuking everything by accident).
// all=true selects every worktree; passing both names and --all is an error.
func resolveTargetsRequired(recs []ports.WorktreeRecord, args []string, all bool) ([]ports.WorktreeRecord, error) {
	if all {
		if len(args) > 0 {
			return nil, fmt.Errorf("pass either branch names or --all, not both")
		}
		if len(recs) == 0 {
			return nil, fmt.Errorf("no worktrees registered")
		}
		out := append([]ports.WorktreeRecord(nil), recs...)
		sort.Slice(out, func(i, j int) bool { return out[i].Branch < out[j].Branch })
		return out, nil
	}
	if len(args) == 0 {
		return nil, fmt.Errorf("pass branch names or --all")
	}
	return resolveTargets(recs, args)
}

// envFromPorts maps an allocation to runner env vars (generic
// <NAME>_PORT names + derived BASE_URL family). COMPOSE_PROJECT_NAME is forced by
// the docker runner and is not set here.
func envFromPorts(p map[string]int) map[string]string {
	out := make(map[string]string, len(p)+3)
	for name, v := range p {
		out[ports.EnvVarForPort(name)] = strconv.Itoa(v)
	}
	if app, ok := p[ports.PortApp]; ok {
		origin := "http://localhost:" + strconv.Itoa(app)
		out[ports.EnvBaseURL] = origin
		out[ports.EnvWebhooksBaseURL] = origin
		out[ports.EnvAllowedWSOrigins] = origin
	}
	return out
}

// shellCmd wraps an entry string for runner.Exec (sh -c preserves
// shell semantics for strings like "docker compose up --wait").
func shellCmd(s string) []string {
	return []string{"sh", "-c", s}
}

// selectBranches prompts for branches from refs (used by add --select).
func selectBranches(refs []string) ([]string, error) {
	if len(refs) == 0 {
		return nil, fmt.Errorf("no remote branches to select from")
	}
	sorted := append([]string(nil), refs...)
	sort.Strings(sorted)
	_, _ = fmt.Println("Remote branches:")
	for i, b := range sorted {
		_, _ = fmt.Printf("  %d) %s\n", i+1, b)
	}
	_, _ = fmt.Print("Select branches (comma-separated numbers or names): ")
	sc := bufio.NewScanner(os.Stdin)
	if !sc.Scan() {
		if err := sc.Err(); err != nil {
			return nil, fmt.Errorf("read selection: %w", err)
		}
		return nil, fmt.Errorf("no selection given")
	}
	raw := strings.TrimSpace(sc.Text())
	if raw == "" {
		return nil, fmt.Errorf("no selection given")
	}
	var out []string
	seen := map[string]struct{}{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if n, err := strconv.Atoi(part); err == nil {
			if n < 1 || n > len(sorted) {
				return nil, fmt.Errorf("selection %q out of range 1-%d", part, len(sorted))
			}
			part = sorted[n-1]
		} else {
			matched := false
			for _, b := range sorted {
				if b == part {
					matched = true
					break
				}
			}
			if !matched {
				return nil, fmt.Errorf("unknown branch %q", part)
			}
		}
		if _, ok := seen[part]; !ok {
			seen[part] = struct{}{}
			out = append(out, part)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no selection given")
	}
	return out, nil
}

// ensureBase creates the worktree base directory.
func ensureBase(base string) error {
	if err := os.MkdirAll(base, 0o755); err != nil {
		return fmt.Errorf("create worktree base %q: %w", base, err)
	}
	return nil
}

// worktreePath joins base with slug and cleans the result.
func worktreePath(base, slug string) string {
	return filepath.Clean(filepath.Join(base, slug))
}
