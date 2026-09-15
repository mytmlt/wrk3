package source

import (
	"strings"
	"testing"
)

func TestSlugify(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"feature/foo", "feature-foo"},
		{"main", "main"},
		{"Feature/Foo", "feature-foo"},
		{"fix--double__dash", "fix-double-dash"},
		{"/leading/trailing/", "leading-trailing"},
		{"a@b#c d", "a-b-c-d"},
		{"", "worktree"},
		{"///", "worktree"},
		{"UPPER-CASE_123", "upper-case-123"},
		{"a/b/c", "a-b-c"},
	}
	for _, tc := range cases {
		if got := Slugify(tc.in); got != tc.want {
			t.Errorf("Slugify(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSlugify_MaxLen(t *testing.T) {
	long := strings.Repeat("a", 60)
	got := Slugify(long)
	if len(got) != MaxSlugLen {
		t.Fatalf("expected len %d, got %d (%q)", MaxSlugLen, len(got), got)
	}
	if strings.HasSuffix(got, "-") {
		t.Fatalf("slug must not end with dash: %q", got)
	}

	// Truncation boundary with dashes: must re-trim trailing dash.
	in := strings.Repeat("ab-", 30) // expands then collapses/trims
	got = Slugify(in)
	if len(got) > MaxSlugLen {
		t.Fatalf("slug too long: %d", len(got))
	}
	if strings.HasSuffix(got, "-") || strings.HasPrefix(got, "-") {
		t.Fatalf("slug must be dash-trimmed: %q", got)
	}
}

func TestSlugify_NoInvalidChars(t *testing.T) {
	inputs := []string{"feat/üñî", "a b", "x:y;z", "a---b___c", "FOO/BAR"}
	for _, in := range inputs {
		got := Slugify(in)
		for _, r := range got {
			ok := r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-'
			if !ok {
				t.Fatalf("Slugify(%q) = %q contains invalid rune %q", in, got, r)
			}
		}
		if strings.Contains(got, "--") {
			t.Fatalf("Slugify(%q) = %q contains collapsed dashes", in, got)
		}
	}
}
