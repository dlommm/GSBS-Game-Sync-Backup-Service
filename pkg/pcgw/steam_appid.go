package pcgw

import "strings"

// ParseSteamAppIDList splits PCGW "steam appid" infobox values (which may be
// comma/space/newline separated and contain stray text) into clean, de-duped
// numeric Steam App IDs, preserving first-seen order.
func ParseSteamAppIDList(vals ...string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range vals {
		fields := strings.FieldsFunc(v, func(r rune) bool {
			return r == ',' || r == ' ' || r == '\n' || r == '\t' || r == ';'
		})
		for _, f := range fields {
			f = strings.TrimSpace(f)
			if f == "" || !isAllDigits(f) || seen[f] {
				continue
			}
			seen[f] = true
			out = append(out, f)
		}
	}
	return out
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// infoboxSteamKeys are the PCGW infobox parameters that carry Steam App IDs,
// matched case-insensitively ("steam appid" plus the "side" variant for
// alternate editions).
// infoboxSteamKeyOrder lists the recognised infobox Steam-ID keys in a fixed
// order (main edition before the "side" alternate edition) so the extracted IDs
// are deterministic regardless of map iteration order.
var infoboxSteamKeyOrder = []string{"steam appid", "steam appid side"}

// SteamAppIDsFromInfobox extracts Steam App IDs from a parsed infobox map
// (raw wikitext keys, e.g. "steam appid"). PCGW's Cargo "Steam_AppID" field is
// sometimes empty even when the infobox carries the ID, so this is used as a
// fallback when the Cargo-derived list is empty.
func SteamAppIDsFromInfobox(infobox map[string]string) []string {
	if len(infobox) == 0 {
		return nil
	}
	norm := make(map[string]string, len(infobox))
	for k, v := range infobox {
		norm[strings.ToLower(strings.TrimSpace(k))] = v
	}
	var vals []string
	for _, key := range infoboxSteamKeyOrder {
		if v, ok := norm[key]; ok {
			vals = append(vals, v)
		}
	}
	return ParseSteamAppIDList(vals...)
}

// SteamAppIDsFromInfoboxAny is SteamAppIDsFromInfobox for the JSON-decoded
// infobox form (map[string]interface{}) stored on PCGWGame.
func SteamAppIDsFromInfoboxAny(infobox map[string]interface{}) []string {
	if len(infobox) == 0 {
		return nil
	}
	norm := make(map[string]string, len(infobox))
	for k, v := range infobox {
		if s, ok := v.(string); ok {
			norm[strings.ToLower(strings.TrimSpace(k))] = s
		}
	}
	var vals []string
	for _, key := range infoboxSteamKeyOrder {
		if v, ok := norm[key]; ok {
			vals = append(vals, v)
		}
	}
	return ParseSteamAppIDList(vals...)
}
