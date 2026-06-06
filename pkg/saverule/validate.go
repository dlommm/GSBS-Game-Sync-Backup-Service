package saverule

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/gsbs/gsbs/pkg/types"
)

// ValidateTemplate checks a raw path template string for security issues before it is
// parsed into save rules. Returns a non-nil error if the template should be rejected.
func ValidateTemplate(template string) error {
	if strings.Contains(template, "..") {
		return fmt.Errorf("path traversal (..) not allowed in template %q", template)
	}
	return nil
}

// ValidateRule checks a single save rule for basic sanity. Returns empty string if valid.
func ValidateRule(rule types.SaveRule, expectedPlatform string) string {
	dir := strings.TrimSpace(rule.Directory)
	if dir == "" || dir == "." || dir == ".." {
		return "empty or invalid directory"
	}
	if strings.Contains(dir, "..") {
		return "path traversal (..) not allowed"
	}
	if expectedPlatform != "" && rule.Platform != "" && rule.Platform != expectedPlatform {
		return fmt.Sprintf("platform mismatch: rule=%s expected=%s", rule.Platform, expectedPlatform)
	}
	if !rule.SyncAll && len(rule.IncludePatterns) == 0 {
		return "no include patterns and sync_all=false"
	}
	for _, pat := range rule.IncludePatterns {
		pat = strings.TrimSpace(pat)
		if pat == "" {
			return "empty include pattern"
		}
		if _, err := filepath.Match(pat, "test"); err != nil {
			return fmt.Sprintf("invalid glob %q: %v", pat, err)
		}
	}
	return ""
}

// FilterValidRules returns rules that pass validation and skip reasons for rejected rules.
func FilterValidRules(rules []types.SaveRule, expectedPlatform string) (valid []types.SaveRule, skipReasons []string) {
	for i, rule := range rules {
		if reason := ValidateRule(rule, expectedPlatform); reason != "" {
			skipReasons = append(skipReasons, fmt.Sprintf("rule[%d] dir=%q: %s", i, rule.Directory, reason))
			continue
		}
		valid = append(valid, rule)
	}
	return valid, skipReasons
}
