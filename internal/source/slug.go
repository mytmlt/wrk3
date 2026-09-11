package source

import "strings"

// MaxSlugLen caps Slugify output (SPEC: max 50 chars).
const MaxSlugLen = 50

// Slugify maps a branch name to a filesystem/compose-safe slug:
// lowercase, non [a-z0-9-] become "-", dashes collapsed, trimmed,
// truncated to MaxSlugLen (trailing dashes re-trimmed).
// feature/foo -> feature-foo. Empty input yields "worktree".
func Slugify(branch string) string {
	s := strings.ToLower(branch)
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		isAlnum := r >= 'a' && r <= 'z' || r >= '0' && r <= '9'
		if isAlnum {
			b.WriteRune(r)
			prevDash = false
			continue
		}
		if !prevDash {
			b.WriteRune('-')
			prevDash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if len(slug) > MaxSlugLen {
		slug = strings.Trim(strings.TrimSpace(slug[:MaxSlugLen]), "-")
	}
	if slug == "" {
		return "worktree"
	}
	return slug
}
