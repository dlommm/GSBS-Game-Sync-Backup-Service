package pcgw

import "strings"

// ParseSaveLocationsFromWikitext returns path templates found in the "Game data" section.
// Parses both {{Game data/saves|OS|path}} templates and legacy section/table format.
func ParseSaveLocationsFromWikitext(wikitext, gameID string) []SaveLocationTemplate {
	var out []SaveLocationTemplate
	seen := make(map[string]bool)

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

	sections := SplitWikiSections(wikitext)
	for key, sec := range sections {
		if key != "game_data" && key != "other" {
			continue
		}
		if key == "other" && !sectionLooksLikeGameData(sec.rawTitle, sec.body) {
			continue
		}
		templates, _ := ParseGameDataSection(sec.body, gameID)
		for _, t := range templates {
			for _, path := range t.Paths {
				k := t.System + "\x00" + path
				if seen[k] {
					continue
				}
				seen[k] = true
				out = append(out, SaveLocationTemplate{
					GameID:   gameID,
					System:   t.System,
					Paths:    []string{path},
					IsConfig: t.IsConfig,
				})
			}
		}
	}
	return out
}

func sectionLooksLikeGameData(title, body string) bool {
	combined := strings.ToLower(title + " " + body)
	return strings.Contains(combined, "save") ||
		strings.Contains(combined, "configuration") ||
		strings.Contains(combined, "game data")
}
