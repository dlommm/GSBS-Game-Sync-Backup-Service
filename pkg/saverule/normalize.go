package saverule

import (
	"sort"
	"strings"

	"github.com/gsbs/gsbs/pkg/types"
)

// NormalizeFunc converts a raw PCGW path segment to resolver-friendly form.
type NormalizeFunc func(raw string) string

type partial struct {
	directory string
	patterns  []string
	syncAll   bool
	recursive bool
}

// ParseSaveRules parses a raw PCGW path string into normalized save rules.
func ParseSaveRules(raw, platform string, isConfig bool, normalize NormalizeFunc) []types.SaveRule {
	raw = strings.TrimSpace(raw)
	if raw == "" || normalize == nil {
		return nil
	}

	var partials []partial
	for _, seg := range strings.Split(raw, "|") {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		norm := strings.TrimSpace(normalize(seg))
		if norm == "" {
			continue
		}
		p := parseNormalizedSegment(norm)
		if p.directory == "" {
			continue
		}
		partials = append(partials, p)
	}
	if len(partials) == 0 {
		return nil
	}

	merged := mergePartials(partials)
	var out []types.SaveRule
	seen := make(map[string]bool)
	for _, m := range merged {
		rule := types.SaveRule{
			Directory:       m.directory,
			IncludePatterns: append([]string(nil), m.patterns...),
			Recursive:       m.recursive,
			Platform:        platform,
			IsConfig:        isConfig,
			SyncAll:         m.syncAll,
		}
		key := ruleDedupeKey(rule)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, rule)
	}
	return out
}

// Directories returns unique directory paths from rules in stable order.
func Directories(rules []types.SaveRule) []string {
	var out []string
	seen := make(map[string]bool)
	for _, r := range rules {
		if r.Directory == "" || seen[r.Directory] {
			continue
		}
		seen[r.Directory] = true
		out = append(out, r.Directory)
	}
	return out
}

func parseNormalizedSegment(norm string) partial {
	lastSlash := strings.LastIndex(norm, "/")
	if lastSlash < 0 {
		if strings.ContainsAny(norm, "*?[") {
			return partial{directory: ".", patterns: []string{norm}}
		}
		if norm == "" || norm == "." || norm == ".." {
			return partial{}
		}
		return partial{directory: norm, syncAll: true}
	}

	tail := norm[lastSlash+1:]
	dir := strings.TrimRight(norm[:lastSlash], "/")
	if dir == "" {
		dir = "/"
	}

	if tail == "*" {
		return partial{directory: dir, syncAll: true, recursive: true}
	}
	if strings.HasPrefix(tail, "*.") && !strings.Contains(tail[2:], "/") {
		return partial{directory: dir, patterns: []string{tail}}
	}
	if strings.HasPrefix(tail, "Save") && strings.Contains(tail, "*") {
		return partial{directory: dir, patterns: []string{tail}, recursive: true}
	}
	if strings.ContainsAny(tail, "*?[") {
		return partial{directory: dir, patterns: []string{tail}}
	}
	if strings.Contains(tail, ".") {
		return partial{directory: dir, patterns: []string{tail}}
	}
	return partial{directory: norm, syncAll: true}
}

func mergePartials(partials []partial) []partial {
	byDir := make(map[string]*partial)
	var order []string
	for _, p := range partials {
		existing, ok := byDir[p.directory]
		if !ok {
			copy := p
			copy.patterns = append([]string(nil), p.patterns...)
			byDir[p.directory] = &copy
			order = append(order, p.directory)
			continue
		}
		if p.syncAll {
			existing.syncAll = true
		}
		if p.recursive {
			existing.recursive = true
		}
		for _, pat := range p.patterns {
			existing.patterns = appendUnique(existing.patterns, pat)
		}
	}

	out := make([]partial, 0, len(order))
	for _, dir := range order {
		p := byDir[dir]
		if len(p.patterns) > 0 {
			p.syncAll = false
		}
		sort.Strings(p.patterns)
		out = append(out, *p)
	}
	return out
}

func ruleDedupeKey(r types.SaveRule) string {
	patterns := append([]string(nil), r.IncludePatterns...)
	sort.Strings(patterns)
	return r.Directory + "\x00" + strings.Join(patterns, ",") + "\x00" + boolStr(r.SyncAll) + "\x00" + boolStr(r.Recursive)
}

func appendUnique(slice []string, s string) []string {
	for _, x := range slice {
		if x == s {
			return slice
		}
	}
	return append(slice, s)
}

func boolStr(v bool) string {
	if v {
		return "1"
	}
	return "0"
}
