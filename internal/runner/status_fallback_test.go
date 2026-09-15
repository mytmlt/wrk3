package runner

import "testing"

func TestCandidateProjects_IncludesPrimaryFirst(t *testing.T) {
	opts := Options{ProjectPrefix: "demo", Slug: "feat-foo"}
	got := CandidateProjects(opts, "/repo/.worktrees/feat-foo", "")
	if len(got) == 0 || got[0] != "demo-feat-foo" {
		t.Errorf("CandidateProjects = %v, want primary demo-feat-foo first", got)
	}
	found := false
	for _, g := range got {
		if g == "feat-foo" {
			found = true
		}
	}
	if !found {
		t.Errorf("CandidateProjects = %v, want fallback feat-foo", got)
	}
}

func TestFallbackProjects_ExcludesPrimary(t *testing.T) {
	opts := Options{ProjectPrefix: "demo", Slug: "feat-foo"}
	got := FallbackProjects(opts, "/repo/.worktrees/feat-foo", "")
	// Primary demo-feat-foo already probed via compose ps, so fallbacks
	// exclude it and cover the out-of-band folder-name project.
	for _, want := range []string{"feat-foo"} {
		found := false
		for _, g := range got {
			if g == want {
				found = true
			}
		}
		if !found {
			t.Errorf("CandidateProjects = %v, want %q", got, want)
		}
	}
	for _, g := range got {
		if g == "demo-feat-foo" {
			t.Errorf("fallbacks must exclude already-probed primary, got %v", got)
		}
	}
}

func TestCandidateProjects_DedupesAndSanitizes(t *testing.T) {
	opts := Options{ProjectPrefix: "demo", Slug: "Feature/Foo"}
	got := CandidateProjects(opts, "/x/Feature-Foo", "demo-feature-foo")
	seen := map[string]struct{}{}
	for _, g := range got {
		if _, ok := seen[g]; ok {
			t.Errorf("duplicate candidate %q in %v", g, got)
		}
		seen[g] = struct{}{}
		if g != SanitizeProjectName(g) {
			t.Errorf("candidate %q not sanitized", g)
		}
	}
}

func TestCountIDs(t *testing.T) {
	if got := countIDs("abc123\ndef456\n"); got != 2 {
		t.Errorf("countIDs = %d, want 2", got)
	}
	if got := countIDs("\n  \n"); got != 0 {
		t.Errorf("countIDs blank = %d, want 0", got)
	}
	if got := countIDs(""); got != 0 {
		t.Errorf("countIDs empty = %d, want 0", got)
	}
}
