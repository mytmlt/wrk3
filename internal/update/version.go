package update

import (
	"strconv"
	"strings"
)

// Normalize strips a leading "v"/"V" and surrounding whitespace so
// "v0.2.0" and "0.2.0" compare equal.
func Normalize(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	v = strings.TrimPrefix(v, "V")
	return v
}

// IsDev reports whether version is a local/dev build that should never
// trigger an update check (no ldflags stamp).
func IsDev(v string) bool {
	n := Normalize(v)
	return n == "" || n == "dev" || n == "none" || n == "unknown" || strings.Contains(n, "next")
}

// parseTriple splits "1.2.3[-prerelease][+build]" into numeric parts and
// the prerelease suffix. Missing parts default to 0. Non-numeric numeric
// parts (e.g. "dev") return ok=false.
func parseTriple(v string) (parts [3]int, prerelease string, ok bool) {
	n := Normalize(v)
	// Strip build metadata.
	if i := strings.Index(n, "+"); i >= 0 {
		n = n[:i]
	}
	// Split prerelease.
	if i := strings.Index(n, "-"); i >= 0 {
		prerelease = n[i+1:]
		n = n[:i]
	}
	segs := strings.Split(n, ".")
	if len(segs) > 3 {
		return parts, prerelease, false
	}
	for i := 0; i < 3; i++ {
		if i >= len(segs) || segs[i] == "" {
			parts[i] = 0
			continue
		}
		num := segs[i]
		// Allow trailing non-digit noise? No — strict.
		for _, r := range num {
			if r < '0' || r > '9' {
				return parts, prerelease, false
			}
		}
		p, err := strconv.Atoi(num)
		if err != nil {
			return parts, prerelease, false
		}
		parts[i] = p
	}
	return parts, prerelease, true
}

// IsNewer reports whether latest is a newer release than current.
// Unknown/unparseable inputs return false (never nag on garbage).
// A release (no prerelease) beats the same-numbers prerelease.
func IsNewer(current, latest string) bool {
	if IsDev(current) || IsDev(latest) {
		// Dev latest makes no sense; dev current never nags.
		// (A "-next" snapshot latest is not an upgrade.)
		return false
	}
	c, _, okC := parseTriple(current)
	l, lPre, okL := parseTriple(latest)
	if !okC || !okL {
		return false
	}
	_, cPre, _ := parseTriple(current)
	for i := 0; i < 3; i++ {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	// Same numbers: release > prerelease.
	if cPre != "" && lPre == "" {
		return true
	}
	return false
}

// NormalizeVersion ensures a "v" prefix for GitHub tag/asset URLs.
// "0.2.0" -> "v0.2.0"; "v0.2.0" unchanged; "latest" unchanged.
func NormalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || v == "latest" {
		return v
	}
	if strings.HasPrefix(v, "v") || strings.HasPrefix(v, "V") {
		return "v" + strings.TrimPrefix(strings.TrimPrefix(v, "v"), "V")
	}
	return "v" + v
}
