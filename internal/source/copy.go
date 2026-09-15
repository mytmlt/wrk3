package source

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// CopyIncluded copies repo-root-relative files/dirs matched by patterns
// from repoRoot into worktreePath after git worktree add.
//
// Semantics: missing sources are skipped with a warning; existing
// destinations are never overwritten (dirs merge — only missing children
// copy). It returns warnings for skipped entries; hard failures
// (unsafe pattern, I/O error) return an error.
func CopyIncluded(repoRoot, worktreePath string, patterns []string) ([]string, error) {
	if len(patterns) == 0 {
		return nil, nil
	}
	if strings.TrimSpace(repoRoot) == "" {
		return nil, fmt.Errorf("copy includes: empty repo path")
	}
	if strings.TrimSpace(worktreePath) == "" {
		return nil, fmt.Errorf("copy includes: empty worktree path")
	}
	repoAbs, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("copy includes: resolve repo path: %w", err)
	}
	repoAbs = filepath.Clean(repoAbs)
	wtAbs, err := filepath.Abs(worktreePath)
	if err != nil {
		return nil, fmt.Errorf("copy includes: resolve worktree path: %w", err)
	}
	wtAbs = filepath.Clean(wtAbs)

	var warns []string
	seen := map[string]struct{}{}
	for _, raw := range patterns {
		pattern := strings.TrimSpace(raw)
		if pattern == "" {
			warns = append(warns, fmt.Sprintf("copy %q: empty pattern, skipping", raw))
			continue
		}
		if filepath.IsAbs(pattern) {
			return warns, fmt.Errorf("copy %q: must be repo-relative, not absolute", raw)
		}
		clean := filepath.Clean(strings.TrimSuffix(pattern, "/"))
		if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return warns, fmt.Errorf("copy %q: must not escape the repo root", raw)
		}
		matches, err := expandPattern(repoAbs, pattern)
		if err != nil {
			return warns, fmt.Errorf("copy %q: %w", raw, err)
		}
		if len(matches) == 0 {
			warns = append(warns, fmt.Sprintf("copy %q: no match in repo root, skipping", pattern))
			continue
		}
		for _, src := range matches {
			if _, dup := seen[src]; dup {
				continue
			}
			seen[src] = struct{}{}
			rel, err := filepath.Rel(repoAbs, src)
			if err != nil {
				return warns, fmt.Errorf("copy %q: %w", raw, err)
			}
			if rel == "." {
				warns = append(warns, fmt.Sprintf("copy %q: refusing to copy repo root itself, skipping", pattern))
				continue
			}
			if rel == ".git" || strings.HasPrefix(rel, ".git"+string(filepath.Separator)) {
				warns = append(warns, fmt.Sprintf("copy %q: refusing to copy .git metadata, skipping", pattern))
				continue
			}
			if err := copyTree(repoAbs, src, rel, wtAbs, pattern, &warns); err != nil {
				return warns, err
			}
		}
	}
	return warns, nil
}

// expandPattern resolves one repo-relative glob against repoAbs.
// Patterns containing "**" use a WalkDir matcher; the rest use
// filepath.Glob. Results are absolute, cleaned, and sorted.
func expandPattern(repoAbs, pattern string) ([]string, error) {
	slash := filepath.ToSlash(strings.TrimSpace(pattern))
	// A trailing slash is a dir marker — Join/Clean drops it, and the
	// IsDir check in copyTree recovers recursive behavior.
	slash = strings.TrimSuffix(slash, "/")
	if strings.Contains(slash, "**") {
		return globDoubleStar(repoAbs, slash)
	}
	abs := filepath.Join(repoAbs, filepath.FromSlash(slash))
	matches, err := filepath.Glob(abs)
	if err != nil {
		return nil, fmt.Errorf("bad pattern: %w", err)
	}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, filepath.Clean(m))
	}
	sort.Strings(out)
	return out, nil
}

// globDoubleStar walks root matching slash-relative pattern segments where
// "**" spans zero or more directories. Other segments use path.Match
// (* ? [...] per segment). Bad segment patterns error.
func globDoubleStar(root, pattern string) ([]string, error) {
	pSegs := strings.Split(filepath.ToSlash(pattern), "/")
	for _, s := range pSegs {
		if s == "**" {
			continue
		}
		if _, err := path.Match(s, ""); err != nil {
			return nil, fmt.Errorf("bad pattern: %w", err)
		}
	}
	var out []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return nil
		}
		if rel == "." {
			return nil
		}
		slashRel := filepath.ToSlash(rel)
		// Never match .git metadata via recursive patterns.
		if slashRel == ".git" || strings.HasPrefix(slashRel, ".git/") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if matchSegments(pSegs, strings.Split(slashRel, "/")) {
			out = append(out, filepath.Clean(p))
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk repo root: %w", err)
	}
	sort.Strings(out)
	return out, nil
}

// matchSegments reports whether pattern segments match path segments.
// "**" consumes zero or more segments; other segments match exactly one
// via path.Match.
func matchSegments(pSegs, rSegs []string) bool {
	if len(pSegs) == 0 {
		return len(rSegs) == 0
	}
	if pSegs[0] == "**" {
		for i := 0; i <= len(rSegs); i++ {
			if matchSegments(pSegs[1:], rSegs[i:]) {
				return true
			}
		}
		return false
	}
	if len(rSegs) == 0 {
		return false
	}
	ok, err := path.Match(pSegs[0], rSegs[0])
	if err != nil || !ok {
		return false
	}
	return matchSegments(pSegs[1:], rSegs[1:])
}

// copyTree copies src (under repoAbs) to wtAbs/rel. Dirs merge recursively;
// existing files/symlinks are skipped with a warning, never overwritten.
func copyTree(repoAbs, src, rel, wtAbs, pattern string, warns *[]string) error {
	dst := filepath.Join(wtAbs, rel)
	if !withinDir(wtAbs, dst) {
		return fmt.Errorf("copy %q: destination escapes worktree", pattern)
	}
	info, err := os.Lstat(src)
	if err != nil {
		*warns = append(*warns, fmt.Sprintf("copy %q: %s vanished, skipping", pattern, rel))
		return nil
	}
	if !info.IsDir() {
		return copySingle(src, dst, rel, pattern, info, warns)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return fmt.Errorf("copy %q: create dir %s: %w", pattern, rel, err)
	}
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("copy %q: walk %s: %w", pattern, rel, err)
		}
		inner, err := filepath.Rel(src, p)
		if err != nil {
			return fmt.Errorf("copy %q: %w", pattern, err)
		}
		if inner == "." {
			return nil
		}
		// Keep .git metadata out even when the user copies a parent dir.
		repoRel, err := filepath.Rel(repoAbs, p)
		if err == nil && (repoRel == ".git" || strings.HasPrefix(repoRel, ".git"+string(filepath.Separator))) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		dstInner := filepath.Join(dst, inner)
		if !withinDir(wtAbs, dstInner) {
			return fmt.Errorf("copy %q: destination escapes worktree", pattern)
		}
		li, err := os.Lstat(p)
		if err != nil {
			*warns = append(*warns, fmt.Sprintf("copy %q: %s vanished, skipping", pattern, filepath.Join(rel, inner)))
			return nil
		}
		if li.IsDir() {
			if err := os.MkdirAll(dstInner, 0o755); err != nil {
				return fmt.Errorf("copy %q: create dir %s: %w", pattern, filepath.Join(rel, inner), err)
			}
			return nil
		}
		return copySingle(p, dstInner, filepath.Join(rel, inner), pattern, li, warns)
	})
}

// copySingle copies one non-dir entry, skipping existing destinations.
func copySingle(src, dst, rel, pattern string, info os.FileInfo, warns *[]string) error {
	if _, err := os.Lstat(dst); err == nil {
		*warns = append(*warns, fmt.Sprintf("copy %q: %s already exists in worktree, leaving intact", pattern, rel))
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("copy %q: stat %s: %w", pattern, rel, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("copy %q: create parent for %s: %w", pattern, rel, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return fmt.Errorf("copy %q: read link %s: %w", pattern, rel, err)
		}
		if err := os.Symlink(target, dst); err != nil {
			return fmt.Errorf("copy %q: link %s: %w", pattern, rel, err)
		}
		return nil
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("copy %q: read %s: %w", pattern, rel, err)
	}
	mode := info.Mode().Perm()
	if mode == 0 {
		mode = 0o644
	}
	if err := os.WriteFile(dst, data, mode); err != nil {
		return fmt.Errorf("copy %q: write %s: %w", pattern, rel, err)
	}
	return nil
}

// withinDir reports whether child equals parent or lives under it.
func withinDir(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
