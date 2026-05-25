package saverule

import (
	"path/filepath"
	"strings"
)

// MatchInclude reports whether relativePath matches any include pattern.
// When syncAll is true and patterns is empty, all files match.
// Patterns are matched against the basename and the full relative path (forward slashes).
func MatchInclude(relativePath string, patterns []string, syncAll bool) bool {
	if syncAll && len(patterns) == 0 {
		return true
	}
	if len(patterns) == 0 {
		return false
	}
	rel := filepath.ToSlash(relativePath)
	base := filepath.Base(rel)
	for _, pat := range patterns {
		pat = strings.TrimSpace(pat)
		if pat == "" {
			continue
		}
		if ok, _ := filepath.Match(pat, base); ok {
			return true
		}
		if ok, _ := filepath.Match(pat, rel); ok {
			return true
		}
	}
	return false
}
