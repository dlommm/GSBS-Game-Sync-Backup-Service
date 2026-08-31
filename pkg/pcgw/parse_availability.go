package pcgw

import (
	"strings"
)

// storeIDsFromAvailability maps a PCGamingWiki store name to the store's own
// game ID, read from the {{Availability/row|<store>|<id>|…}} templates on a
// page.
//
// These IDs are NOT in {{Infobox game}} — only Steam and GOG are. Reading them
// out of the availability rows is the only way to populate them from wikitext,
// which matters now that the Cargo Availability table is unreachable.
func storeIDsFromAvailability(wikitext string) map[string]string {
	out := make(map[string]string)
	const marker = "{{Availability/row"
	for i := 0; ; {
		idx := strings.Index(wikitext[i:], marker)
		if idx < 0 {
			return out
		}
		start := i + idx
		end := findTemplateEnd(wikitext, start+2)
		if end < 0 {
			return out
		}
		body := wikitext[start+2 : end-2]
		args := splitTemplateArgs(body)
		// args[0] is the template name; args[1] the store, args[2] the ID.
		if len(args) >= 3 {
			store := normalizeStoreName(args[1])
			id := strings.TrimSpace(args[2])
			// First row for a store wins: later rows are usually alternate
			// editions pointing at the same game.
			if store != "" && id != "" && out[store] == "" {
				out[store] = id
			}
		}
		i = end
	}
}

// normalizeStoreName lowercases and collapses whitespace in a store label so
// "Epic Games Store" and "epic  games store" compare equal.
func normalizeStoreName(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

// AvailabilityStoreIDs returns the Epic Games Store and Ubisoft Store IDs for a
// page, or empty strings when the page lists neither.
//
// Caveat for callers doing install matching: these are PCGamingWiki's store
// IDs (Epic's URL slug, Ubisoft's store ID). A locally detected Epic install
// reports Epic's opaque catalog AppName and a Ubisoft install reports its
// install-folder name, so an exact ID match is not guaranteed and title
// matching remains the fallback.
func AvailabilityStoreIDs(wikitext string) (epicID, ubisoftID string) {
	ids := storeIDsFromAvailability(wikitext)
	return ids["epic games store"], ids["ubisoft store"]
}

// InfoboxValue reads an {{Infobox game}} parameter by name, tolerating the
// casing and spacing variations found across pages.
//
// PCGamingWiki writes these parameters in lowercase ("hltb", "igdb",
// "steam appid"). Looking them up by the Cargo column names ("HLTB", "IGDB")
// silently returned nothing on every page.
func InfoboxValue(infobox map[string]string, keys ...string) string {
	if len(infobox) == 0 {
		return ""
	}
	normalized := make(map[string]string, len(infobox))
	for k, v := range infobox {
		normalized[normalizeInfoboxKey(k)] = v
	}
	for _, k := range keys {
		if v := strings.TrimSpace(normalized[normalizeInfoboxKey(k)]); v != "" {
			return v
		}
	}
	return ""
}

// normalizeInfoboxKey lowercases a parameter name and treats underscores and
// runs of whitespace as single spaces ("Steam_AppID" == "steam appid").
func normalizeInfoboxKey(k string) string {
	k = strings.ReplaceAll(strings.ToLower(k), "_", " ")
	return strings.Join(strings.Fields(k), " ")
}
