package pcgw

import (
	"regexp"
	"strings"
)

// ParseSaveLocationsFromWikitext extracts save/config path templates from PCGW wikitext.
// PCGW uses templates like {{Game data/saves|Windows|{{p|appdata}}\\...}} and table rows.
// Paths are normalized to resolver placeholders: %APPDATA%, %LOCALAPPDATA%, %USERPROFILE%, <user-id>.
var (
	// Match table row with path: | Windows || path here  (capture system and path). Include Steam (Linux), GOG, Epic.
	pathRowRe = regexp.MustCompile(`(?m)^\|\s*(Windows|Steam Play \(Linux\)|Linux|Steam \(Linux\)|Ubisoft Connect \(Windows\)|Steam \(Windows\)|GOG|Epic)\s*\|\|\s*([^\n|]+)`)
	// Match inline path in template args (simplified)
	inlinePathRe = regexp.MustCompile(`(?m)\|\s*[^=]+\s*=\s*([^\n|{}]+(?:%[A-Za-z]+%|[<>][A-Za-z-]+[<>])?[^\n|{}]*)`)
	// Match start of {{Game data/saves|OS| or {{Game data/config|OS| or {{Game data/save|OS| (singular)
	gameDataTemplateRe = regexp.MustCompile(`\{\{Game data/(saves|config|save)\|(Windows|Linux|Steam Play \(Linux\))\|`)
)

// NormalizePathTemplate converts PCGW placeholders to resolver-friendly form.
// Maps: {{p|appdata}} -> %APPDATA%, {{p|localappdata}} -> %LOCALAPPDATA%,
// {{p|userprofile}} -> %USERPROFILE%, {{p|uid}} -> <user-id>.
func NormalizePathTemplate(raw string) string {
	s := raw
	// PCGW path template placeholders (see PCGW wiki) to resolver placeholders
	replacements := []struct{ from, to string }{
		{"{{p|appdata}}", "%APPDATA%"},
		{"{{p|localappdata}}", "%LOCALAPPDATA%"},
		{"{{p|userprofile}}", "%USERPROFILE%"},
		{"{{p|userprofile\\documents}}", "%USERPROFILE%/Documents"},
		{"{{p|public}}", "%PUBLIC%"},
		{"{{p|programdata}}", "%PROGRAMDATA%"},
		{"{{p|programfiles}}", "%PROGRAMFILES%"},
		{"{{p|uid}}", "<user-id>"},
		{"{{p|steam}}", "<SteamLibrary-folder>"},
		{"{{p|uplay}}", "<Ubisoft-Connect-folder>"},
	}
	for _, r := range replacements {
		s = strings.ReplaceAll(s, r.from, r.to)
	}
	// Remove any remaining {{...}} we don't map (optional: could add more mappings)
	for strings.Contains(s, "{{") {
		start := strings.Index(s, "{{")
		end := strings.Index(s, "}}")
		if end < start {
			break
		}
		s = s[:start] + s[end+2:]
	}
	// Normalize backslashes to forward slashes for consistency; resolver will convert per OS
	s = strings.ReplaceAll(s, "\\\\", "/")
	s = strings.ReplaceAll(s, "\\", "/")
	return strings.TrimSpace(s)
}

// SystemToPlatform returns "windows" or "linux" for manifest storage.
func SystemToPlatform(system string) string {
	switch {
	case strings.HasPrefix(system, "Windows"), system == "Steam (Windows)", system == "Ubisoft Connect (Windows)", system == "GOG", system == "Epic":
		return "windows"
	case system == "Linux", system == "Steam Play (Linux)", system == "Steam (Linux)":
		return "linux"
	default:
		return "windows"
	}
}

// ParseSaveLocationsFromWikitext returns path templates found in the "Game data" section.
// Parses both {{Game data/saves|OS|path}} templates and legacy section/table format.
func ParseSaveLocationsFromWikitext(wikitext, gameID string) []SaveLocationTemplate {
	var out []SaveLocationTemplate
	seen := make(map[string]bool) // dedupe by system+path

	// 1. Parse {{Game data/saves|Windows|...}} and {{Game data/config|...}} templates
	for _, loc := range parseGameDataTemplates(wikitext, gameID) {
		for _, path := range loc.Paths {
			key := loc.System + "\x00" + path
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, SaveLocationTemplate{
				GameID:   gameID,
				System:   loc.System,
				Paths:    []string{path},
				IsConfig: loc.IsConfig,
			})
		}
	}

	// 2. Fallback: section/table parsing
	sections := splitSections(wikitext)
	for _, sec := range sections {
		isConfig := strings.Contains(sec.title, "Configuration")
		paths := extractPathsFromSection(sec.body)
		for system, pathList := range paths {
			for _, p := range pathList {
				normalized := NormalizePathTemplate(p)
				if normalized == "" {
					continue
				}
				key := system + "\x00" + normalized
				if seen[key] {
					continue
				}
				seen[key] = true
				out = append(out, SaveLocationTemplate{
					GameID:   gameID,
					System:   system,
					Paths:    []string{normalized},
					IsConfig: isConfig,
				})
			}
		}
	}
	return out
}

// parseGameDataTemplates finds {{Game data/saves|OS|path}} and {{Game data/config|OS|path}}.
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
		kind, system := sub[1], sub[2] // saves|config, Windows|Linux|...
		pathStart := start + len(sub[0]) // after the second |
		end := findTemplateEnd(s, pathStart)
		if end < 0 {
			s = s[start+1:]
			continue
		}
		// end points past the closing }}; exclude those 2 chars from the path
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
				IsConfig: kind == "config", // "saves" or "save" -> false
			})
		}
		s = s[end:]
	}
	return out
}

// findTemplateEnd returns the index of the closing }} for the template starting before pathStart.
func findTemplateEnd(s string, pathStart int) int {
	depth := 1
	for i := pathStart; i < len(s)-1; i++ {
		if s[i] == '{' && s[i+1] == '{' {
			depth++
			i++
			continue
		}
		if s[i] == '}' && s[i+1] == '}' {
			depth--
			if depth == 0 {
				return i + 2
			}
			i++
		}
	}
	return -1
}

type section struct {
	title string
	body  string
}

func splitSections(wikitext string) []section {
	var out []section
	// Level 2 headers: == Save game data location ==
	re := regexp.MustCompile(`(?m)^==\s*(.+?)\s*==\s*$`)
	idx := re.FindAllStringSubmatchIndex(wikitext, -1)
	if len(idx) == 0 {
		return out
	}
	for i := 0; i < len(idx); i++ {
		start := idx[i][0]
		end := len(wikitext)
		if i+1 < len(idx) {
			end = idx[i+1][0]
		}
		title := wikitext[idx[i][2]:idx[i][3]]
		body := wikitext[start:end]
		if strings.Contains(title, "Save") || strings.Contains(title, "Configuration") ||
			strings.Contains(title, "Additional notes") || strings.Contains(title, "Notes") {
			out = append(out, section{title: title, body: body})
		}
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
	// Also try inline paths
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
