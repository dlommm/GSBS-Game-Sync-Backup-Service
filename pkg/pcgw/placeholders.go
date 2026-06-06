package pcgw

import (
	"regexp"
	"strings"

	"github.com/gsbs/gsbs/pkg/saverule"
	"github.com/gsbs/gsbs/pkg/types"
)

// pathPlaceholderRe matches PCGW Path templates: {{p|...}}, {{P|...}}, {{Path|...}}.
var pathPlaceholderRe = regexp.MustCompile(`(?i)\{\{(?:path|[pP])\|([^}]+)\}\}`)

// brTagRe matches all variants of the HTML <br> tag used as a path separator in PCGW wikitext.
var brTagRe = regexp.MustCompile(`(?i)<br\s*/?>`)

// placeholderMap maps normalized PCGW {{p|key}} names to resolver-friendly tokens.
// See https://www.pcgamingwiki.com/wiki/PCGamingWiki:Editing_guide/Game_data
var placeholderMap = map[string]string{
	"game":                             "<game-install-folder>",
	"appdata":                          "%APPDATA%",
	"localappdata":                     "%LOCALAPPDATA%",
	"userprofile":                      "%USERPROFILE%",
	"userprofile/documents":            "%USERPROFILE%/Documents",
	"userprofile\\documents":           "%USERPROFILE%/Documents",
	"userprofile/documents/my games":   "%USERPROFILE%/Documents/My Games",
	"userprofile\\documents\\my games": "%USERPROFILE%/Documents/My Games",
	"userprofile/saved games":          "%USERPROFILE%/Saved Games",
	"userprofile\\saved games":         "%USERPROFILE%/Saved Games",
	"userprofile/appdata/locallow":     "%USERPROFILE%/AppData/LocalLow",
	"userprofile\\appdata\\locallow":   "%USERPROFILE%/AppData/LocalLow",
	"public":                           "%PUBLIC%",
	"programdata":                      "%PROGRAMDATA%",
	"programfiles":                     "%PROGRAMFILES%",
	"programfiles(x86)":                "%PROGRAMFILES(x86)%",
	"programfiles (x86)":               "%PROGRAMFILES(x86)%",
	"uid":                              "<user-id>",
	"steam":                            "<SteamLibrary-folder>",
	"uplay":                            "<Ubisoft-Connect-folder>",
	"gog":                              "<GOG-Galaxy-folder>",
	"epic":                             "<Epic-Games-folder>",
	"ea":                               "<EA-App-folder>",
	"origin":                           "<EA-App-folder>",
	"xbox":                             "<Xbox-App-folder>",
	"microsoft":                        "<Xbox-App-folder>",
	"heroic":                           "<Heroic-folder>",
	"lutris":                           "<Lutris-folder>",
	"bottles":                          "<Bottles-folder>",
	"prism":                            "<Prism-folder>",
	"flatpak":                          "<Flatpak-Steam-folder>",
	"flatpak-steam":                    "<Flatpak-Steam-folder>",
	"flatpaksteam":                     "<Flatpak-Steam-folder>",
	"linuxhome":                        "%USERPROFILE%",
	"osxhome":                          "%USERPROFILE%",
	"machome":                          "%USERPROFILE%",
	"winhome":                          "%USERPROFILE%",
	"xdgdatahome":                      "%LOCALAPPDATA%",
	"xdgconfighome":                    "%APPDATA%",
	// xdgcachehome uses an OS-aware token: on Linux resolves to $XDG_CACHE_HOME
	// (default ~/.cache); on Windows resolves to %LOCALAPPDATA%\cache.
	"xdgcachehome":                     "<xdg-cache-home>",
}

// NormalizePathTemplate converts PCGW {{p|...}} / {{Path|...}} placeholders to resolver-friendly form.
// Known keys are mapped; unknown {{p|...}} tokens are preserved literally.
// Non-path templates (e.g. {{Game data/...}}) are never stripped.
func NormalizePathTemplate(raw string) string {
	s := pathPlaceholderRe.ReplaceAllStringFunc(raw, func(match string) string {
		sub := pathPlaceholderRe.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		key := strings.ToLower(strings.TrimSpace(sub[1]))
		key = strings.ReplaceAll(key, "\\", "/")
		if mapped, ok := placeholderMap[key]; ok {
			return mapped
		}
		return match
	})
	s = strings.ReplaceAll(s, "\\\\", "/")
	s = strings.ReplaceAll(s, "\\", "/")
	return strings.TrimSpace(s)
}

// ParseSaveRules parses a raw PCGW path string into structured save rules.
func ParseSaveRules(raw, platform string, isConfig bool) []types.SaveRule {
	return saverule.ParseSaveRules(raw, platform, isConfig, NormalizePathTemplate)
}

// SplitNormalizePathTemplates splits pipe-separated PCGW path strings, normalizes each
// segment, strips trailing file globs (/*, /*.ext) to directory-only paths, deduplicates,
// and returns non-empty results in stable order.
//
// Alternate paths separated by <br>, <br/>, or <br /> inside one template argument
// are normalized to the | separator before splitting.
func SplitNormalizePathTemplates(raw string) []string {
	raw = brTagRe.ReplaceAllString(raw, "|")
	return saverule.Directories(ParseSaveRules(raw, "", false))
}
