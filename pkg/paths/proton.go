package paths

import (
	"os"
	"path/filepath"
	"strings"
)

// protonPlaceholder maps a Windows path placeholder to its Proton Wine prefix sub-path
// under <compatdata>/<appid>/pfx/drive_c/.
type protonPlaceholder struct {
	// winPrefix is the Windows env-var placeholder (uppercase, e.g. "%APPDATA%").
	winPrefix string
	// pfxSubPath is the relative path inside drive_c (forward-slash form).
	pfxSubPath string
}

// protonMappings lists all supported Windows placeholders in longest-first order
// so that more-specific prefixes match before shorter ones (e.g. %LOCALAPPDATA% before %APPDATA%).
var protonMappings = []protonPlaceholder{
	{"%LOCALAPPDATA%", "users/steamuser/AppData/Local"},
	{"%APPDATA%", "users/steamuser/AppData/Roaming"},
	{"%USERPROFILE%", "users/steamuser"},
	{"%PUBLIC%", "users/Public"},
	{"%PROGRAMDATA%", "ProgramData"},
}

// ResolveWindowsTemplateAsProton synthesises the Linux Proton compatdata path(s) that
// correspond to a Windows-style save-location template when a game is run via Proton.
//
//   - template: a Windows path template such as "%APPDATA%\Game\saves"
//   - appID: the Steam App ID string (e.g. "1245620")
//   - steamLibraries: list of Steam library roots to search
//
// A result is produced for each library that has
// <lib>/steamapps/compatdata/<appID>/ present on disk.  Libraries without that
// directory are silently skipped (game not installed there).
//
// Returns nil (not an error) when the template contains no recognised Windows
// placeholder — callers should treat that as "no Proton path available".
//
// Not wired into the main resolver pipeline yet; that is Step 6 (client integration).
func ResolveWindowsTemplateAsProton(template string, appID string, steamLibraries []string) ([]string, error) {
	template = strings.TrimSpace(template)
	if template == "" || appID == "" {
		return nil, nil
	}

	// Normalise backslashes to forward slashes for matching.
	normalised := strings.ReplaceAll(template, "\\", "/")

	var matched *protonPlaceholder
	var suffix string // remainder after the placeholder
	for i := range protonMappings {
		pm := &protonMappings[i]
		// Case-insensitive prefix match (PCGW templates are usually uppercase,
		// but tolerate lowercase/mixed from user-defined entries).
		if strings.HasPrefix(strings.ToUpper(normalised), pm.winPrefix) {
			matched = pm
			rest := normalised[len(pm.winPrefix):]
			suffix = strings.TrimLeft(rest, "/")
			break
		}
	}
	if matched == nil {
		// No recognised Windows placeholder — cannot synthesise a Proton path.
		return nil, nil
	}

	var out []string
	seen := make(map[string]bool)
	for _, lib := range steamLibraries {
		if lib == "" {
			continue
		}
		compatDataDir := filepath.Join(lib, "steamapps", "compatdata", appID)
		if _, err := os.Stat(compatDataDir); err != nil {
			continue // game not installed in this library
		}
		driveC := filepath.Join(compatDataDir, "pfx", "drive_c")
		pfxBase := filepath.Join(driveC, filepath.FromSlash(matched.pfxSubPath))

		var resolved string
		if suffix != "" {
			resolved = filepath.Join(pfxBase, filepath.FromSlash(suffix))
		} else {
			resolved = pfxBase
		}
		if !seen[resolved] {
			seen[resolved] = true
			out = append(out, resolved)
		}
	}
	return out, nil
}
