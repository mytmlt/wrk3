package telemetry

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	hexHashRe = regexp.MustCompile(`\b[0-9a-f]{7,40}\b`)
	uuidRe    = regexp.MustCompile(`\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)
	emailRe   = regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b`)
	ipv4Re    = regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`)
	absPathRe = regexp.MustCompile(`(?:^|\s)(/[^\s:"]*(?:/[^\s:"]+)+)`)
)

func Scrub(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	msg = scrubString(msg)
	if msg == "" {
		return ""
	}
	if len(msg) > 1024 {
		msg = msg[:1024]
	}
	return msg
}

func scrubString(s string) string {
	s = scrubHomeDir(s)
	s = scrubAbsPaths(s)
	s = uuidRe.ReplaceAllString(s, "<uuid>")
	s = hexHashRe.ReplaceAllString(s, "<hash>")
	s = emailRe.ReplaceAllString(s, "<email>")
	s = ipv4Re.ReplaceAllString(s, "<ip>")
	s = strings.TrimSpace(s)
	return s
}

func scrubHomeDir(s string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return s
	}
	home = filepath.Clean(home)
	return strings.ReplaceAll(s, home, "$HOME")
}

func scrubAbsPaths(s string) string {
	return absPathRe.ReplaceAllStringFunc(s, func(match string) string {
		match = strings.TrimSpace(match)
		return " <path>"
	})
}