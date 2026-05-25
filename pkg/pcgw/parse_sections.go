package pcgw

import (
	"regexp"
	"strings"
)

var sectionHeaderRe = regexp.MustCompile(`(?m)^==\s*(.+?)\s*==\s*$`)

// wikiSection is a raw split section before key normalization.
type wikiSection struct {
	rawTitle string
	body     string
}

// SplitWikiSections splits wikitext on level-2 == headers and maps titles to normalized keys.
// Content before the first header is stored under key "lead".
func SplitWikiSections(wikitext string) map[string]wikiSection {
	out := make(map[string]wikiSection)
	idx := sectionHeaderRe.FindAllStringSubmatchIndex(wikitext, -1)
	if len(idx) == 0 {
		if strings.TrimSpace(wikitext) != "" {
			out["lead"] = wikiSection{rawTitle: "lead", body: wikitext}
		}
		return out
	}
	if idx[0][0] > 0 {
		lead := strings.TrimSpace(wikitext[:idx[0][0]])
		if lead != "" {
			out["lead"] = wikiSection{rawTitle: "lead", body: lead}
		}
	}
	for i := 0; i < len(idx); i++ {
		start := idx[i][0]
		end := len(wikitext)
		if i+1 < len(idx) {
			end = idx[i+1][0]
		}
		title := wikitext[idx[i][2]:idx[i][3]]
		body := wikitext[start:end]
		key := NormalizeSectionKey(title)
		out[key] = wikiSection{rawTitle: title, body: body}
	}
	return out
}

// NormalizeSectionKey maps a PCGW section title to a stable storage key.
// Unknown sections map to "other".
func NormalizeSectionKey(title string) string {
	t := strings.ToLower(strings.TrimSpace(title))
	switch {
	case t == "introduction":
		return "introduction"
	case strings.Contains(t, "availability"):
		return "availability"
	case strings.Contains(t, "game data"):
		return "game_data"
	case strings.Contains(t, "monetization"):
		return "monetization"
	case strings.Contains(t, "microtransaction"):
		return "microtransactions"
	case t == "video":
		return "video"
	case t == "input":
		return "input"
	case t == "audio":
		return "audio"
	case t == "network":
		return "network"
	case strings.Contains(t, "localization"):
		return "localizations"
	case strings.Contains(t, "vr support"):
		return "vr_support"
	case strings.Contains(t, "system requirement"):
		return "system_requirements"
	case strings.Contains(t, "essential improvement"):
		return "essential_improvements"
	case strings.Contains(t, "issues unresolved"):
		return "issues_unresolved"
	case strings.Contains(t, "issues fixed"):
		return "issues_fixed"
	case strings.Contains(t, "other information"):
		return "other_information"
	case t == "files":
		return "files"
	case t == "notes" || strings.Contains(t, "additional notes"):
		return "notes"
	case t == "references":
		return "references"
	case strings.Contains(t, "external link"):
		return "external_links"
	case strings.Contains(t, "save game data") || strings.Contains(t, "configuration file"):
		return "game_data"
	default:
		return "other"
	}
}
