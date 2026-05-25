package pcgw

import (
	"regexp"
	"strings"
)

var (
	// gameDataTemplateRe matches {{Game data/saves|OS|...}} with any OS label.
	gameDataTemplateRe = regexp.MustCompile(`(?i)\{\{Game data/(saves|config|save)\|([^|]+)\|`)
	// pathRowRe matches table rows | OS || path | with flexible OS labels.
	pathRowRe = regexp.MustCompile(`(?m)^\|\s*([^|]+?)\s*\|\|\s*([^\n|]+)`)
	// inlinePathRe matches inline key=value path hints in wikitext.
	inlinePathRe = regexp.MustCompile(`(?m)\|\s*[^=]+\s*=\s*([^\n|{}]+(?:%[A-Za-z]+%|[<>][A-Za-z-]+[<>])?[^\n|{}]*)`)
)

// SystemToPlatform returns "windows", "linux", or "macos" for manifest storage.
func SystemToPlatform(system string) string {
	s := strings.ToLower(strings.TrimSpace(system))
	switch {
	case strings.Contains(s, "linux"), strings.Contains(s, "steam play"):
		return "linux"
	case strings.Contains(s, "mac"), strings.Contains(s, "os x"), strings.Contains(s, "osx"):
		return "macos"
	default:
		return "windows"
	}
}

// ParseGameDataSection parses save/config paths from a Game data section body.
func ParseGameDataSection(sectionWikitext, gameID string) ([]SaveLocationTemplate, map[string]interface{}) {
	var templates []SaveLocationTemplate
	seen := make(map[string]bool)
	for _, loc := range parseGameDataTemplates(sectionWikitext, gameID) {
		for _, path := range loc.Paths {
			key := loc.System + "\x00" + path
			if seen[key] {
				continue
			}
			seen[key] = true
			templates = append(templates, SaveLocationTemplate{
				GameID:   gameID,
				System:   loc.System,
				Paths:    []string{path},
				IsConfig: loc.IsConfig,
			})
		}
	}
	paths := extractPathsFromSection(sectionWikitext)
	for system, pathList := range paths {
		for _, p := range pathList {
			key := system + "\x00" + p
			if seen[key] {
				continue
			}
			seen[key] = true
			templates = append(templates, SaveLocationTemplate{
				GameID:   gameID,
				System:   system,
				Paths:    []string{p},
				IsConfig: strings.Contains(strings.ToLower(sectionWikitext), "configuration"),
			})
		}
	}
	data := map[string]interface{}{
		"save_locations":  filterTemplates(templates, false),
		"config_locations": filterTemplates(templates, true),
		"platforms":       collectSystems(templates),
	}
	return templates, data
}

func filterTemplates(all []SaveLocationTemplate, config bool) []map[string]interface{} {
	var out []map[string]interface{}
	for _, t := range all {
		if t.IsConfig != config {
			continue
		}
		out = append(out, map[string]interface{}{
			"system":            t.System,
			"platform":          SystemToPlatform(t.System),
			"platform_raw_label": t.System,
			"path_templates":    t.Paths,
		})
	}
	return out
}

func collectSystems(all []SaveLocationTemplate) []string {
	seen := make(map[string]bool)
	var out []string
	for _, t := range all {
		if !seen[t.System] {
			seen[t.System] = true
			out = append(out, t.System)
		}
	}
	return out
}

func parseGameDataTemplates(wikitext, gameID string) []SaveLocationTemplate {
	var out []SaveLocationTemplate
	s := wikitext
	for {
		idx := gameDataTemplateRe.FindStringIndex(s)
		if idx == nil {
			break
		}
		start := idx[0]
		sub := gameDataTemplateRe.FindStringSubmatch(s[start:])
		if len(sub) != 3 {
			s = s[idx[1]:]
			continue
		}
		kind, system := sub[1], strings.TrimSpace(sub[2])
		pathStart := start + len(sub[0])
		end := findTemplateEnd(s, pathStart)
		if end < 0 {
			s = s[start+1:]
			continue
		}
		if end-2 <= pathStart {
			s = s[end:]
			continue
		}
		pathRaw := s[pathStart : end-2]
		pathNorm := NormalizePathTemplate(pathRaw)
		if pathNorm != "" {
			out = append(out, SaveLocationTemplate{
				GameID:   gameID,
				System:   system,
				Paths:    []string{pathNorm},
				IsConfig: strings.EqualFold(kind, "config"),
			})
		}
		s = s[end:]
	}
	return out
}

func extractPathsFromSection(body string) map[string][]string {
	pathsBySystem := make(map[string][]string)
	for _, m := range pathRowRe.FindAllStringSubmatch(body, -1) {
		if len(m) < 3 {
			continue
		}
		sys := strings.TrimSpace(m[1])
		path := cleanPath(m[2])
		if path == "" {
			continue
		}
		path = NormalizePathTemplate(path)
		if path != "" {
			pathsBySystem[sys] = append(pathsBySystem[sys], path)
		}
	}
	for _, m := range inlinePathRe.FindAllStringSubmatch(body, -1) {
		if len(m) < 2 {
			continue
		}
		path := cleanPath(m[1])
		if path != "" && (strings.Contains(path, "save") || strings.Contains(path, "Save") || strings.Contains(path, "%") || strings.Contains(path, "<") || strings.Contains(path, "pfx")) {
			path = NormalizePathTemplate(path)
			if path != "" {
				pathsBySystem["Windows"] = append(pathsBySystem["Windows"], path)
			}
		}
	}
	return pathsBySystem
}

func cleanPath(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "[]")
	return s
}
