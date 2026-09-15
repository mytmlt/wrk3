package forge

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// defaultDetectTimeout bounds the `git remote get-url` probe.
const defaultDetectTimeout = 15 * time.Second

// Well-known forge kinds returned by Detect.
const (
	KindGitHub  = "github"
	KindGitLab  = "gitlab"
	KindGitea   = "gitea"
	KindUnknown = "unknown"
)

// Detect reports which forge hosts remote in the repo at repoPath by
// reading `git remote get-url <remote>`. SSH
// (git@github.com:owner/repo.git) and HTTPS
// (https://github.com/owner/repo[.git]) forms both parse; hosts that
// match no known forge return KindUnknown with nil error so callers can
// gate forge-gated flags with a helpful message instead of failing.
func Detect(repoPath, remote string) (string, error) {
	raw, err := remoteURL(repoPath, remote)
	if err != nil {
		return "", err
	}
	return Classify(raw), nil
}

// remoteURL returns the URL of remote via `git remote get-url`.
func remoteURL(repoPath, remote string) (string, error) {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		remote = "origin"
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultDetectTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "remote", "get-url", remote)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git remote get-url %s: %w: %s",
			remote, err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// Classify maps a remote URL to a forge kind. Matching is suffix-based so
// self-hosted instances work: hosts ending in gitlab.<domain> classify as
// gitlab, gitea-bearing hosts as gitea; everything else is unknown.
// GitHub Enterprise (custom domains) is deliberately unknown until a
// `gh --hostname` aware provider ships.
func Classify(rawURL string) string {
	host := hostOf(rawURL)
	if host == "" {
		return KindUnknown
	}
	switch {
	case host == "github.com" || strings.HasSuffix(host, ".ghe.com"):
		return KindGitHub
	case strings.Contains(host, "gitlab"):
		return KindGitLab
	case strings.Contains(host, "gitea"):
		return KindGitea
	default:
		return KindUnknown
	}
}

// hostOf extracts the host from SSH (git@host:path, ssh://host/...) or
// HTTPS remote URLs, lowercased. Empty when unparseable.
func hostOf(rawURL string) string {
	u := strings.TrimSpace(rawURL)
	if u == "" {
		return ""
	}
	// SCP-like syntax: [user@]host:path (no scheme, single colon).
	if !strings.Contains(u, "://") {
		at := strings.LastIndex(u, "@")
		rest := u
		if at >= 0 {
			rest = u[at+1:]
		}
		colon := strings.Index(rest, ":")
		if colon <= 0 {
			return ""
		}
		return strings.ToLower(rest[:colon])
	}
	// Scheme syntax: scheme://[user@]host[:port]/path.
	rest := u[strings.Index(u, "://")+3:]
	if at := strings.LastIndex(rest, "@"); at >= 0 {
		rest = rest[at+1:]
	}
	if i := strings.IndexAny(rest, "/?#"); i >= 0 {
		rest = rest[:i]
	}
	// Strip :port.
	if i := strings.LastIndex(rest, ":"); i >= 0 {
		rest = rest[:i]
	}
	return strings.ToLower(strings.TrimSpace(rest))
}
