package pcgw

import (
	"regexp"
	"strings"

	"github.com/gsbs/gsbs/pkg/saverule"
	"github.com/gsbs/gsbs/pkg/types"
)

// pPlaceholderRe matches PCGW Path shorthand {{p|...}} or {{P|...}}.
var pPlaceholderRe = regexp.MustCompile(`(?i)\{\{[pP]\|([^}]+)\}\}`)

// placeholderMap maps normalized PCGW {{p|key}} names to resolver-friendly tokens.
var placeholderMap = map[string]string{
	"appdata":                "%APPDATA%",
	"localappdata":           "%LOCALAPPDATA%",
	"userprofile":            "%USERPROFILE%",
	"userprofile/documents":  "%USERPROFILE%/Documents",
	"userprofile\\documents": "%USERPROFILE%/Documents",
	"public":                 "%PUBLIC%",
	"programdata":            "%PROGRAMDATA%",
	"programfiles":           "%PROGRAMFILES%",
	"uid":                    "<user-id>",
	"steam":                  "<SteamLibrary-folder>",
	"uplay":                  "<Ubisoft-Connect-folder>",
	"gog":                    "<GOG-Galaxy-folder>",
	"epic":                   "<Epic-Games-folder>",
	"ea":                     "<EA-App-folder>",
	"origin":                 "<EA-App-folder>",
	"xbox":                   "<Xbox-App-folder>",
	"microsoft":              "<Xbox-App-folder>",
	"heroic":                 "<Heroic-folder>",
	"lutris":                 "<Lutris-folder>",
	"bottles":                "<Bottles-folder>",
	"prism":                  "<Prism-folder>",
	"flatpak":                "<Flatpak-Steam-folder>",
	"flatpak-steam":          "<Flatpak-Steam-folder>",
	"flatpaksteam":           "<Flatpak-Steam-folder>",
	"linuxhome":              "%USERPROFILE%",
	"osxhome":                "%USERPROFILE%",
	"machome":                "%USERPROFILE%",
	"xdgdatahome":            "%LOCALAPPDATA%",
	"xdgconfighome":          "%APPDATA%",
}

// NormalizePathTemplate converts PCGW {{p|...}} placeholders to resolver-friendly form.
// Known keys are mapped; unknown {{p|...}} tokens are preserved literally.
// Non-p templates (e.g. {{Game data/...}}) are never stripped.
func NormalizePathTemplate(raw string) string {
	s := pPlaceholderRe.ReplaceAllStringFunc(raw, func(match string) string {
		sub := pPlaceholderRe.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		key := strings.ToLower(strings.TrimSpace(sub[1]))
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
func SplitNormalizePathTemplates(raw string) []string {
	return saverule.Directories(ParseSaveRules(raw, "", false))
}
