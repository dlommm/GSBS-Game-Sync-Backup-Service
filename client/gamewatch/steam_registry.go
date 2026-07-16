package gamewatch

import (
	"os"
	"path/filepath"
	"regexp"
)

// Steam's registry.vdf carries the client's live state, including
// "RunningAppID" — the app currently running (0 when none). Reading it works
// from inside a Flatpak sandbox (plain file under $HOME), which makes it the
// game-detection signal on Steam Deck where the PID-scan detector is blocked
// by the sandbox's PID namespace. Steam-only by nature; non-Steam games under
// Flatpak stay undetected (documented limitation).
var runningAppIDRe = regexp.MustCompile(`"RunningAppID"\s*"(\d+)"`)

// SteamRegistryPaths returns the candidate registry.vdf locations for native
// and Flatpak Steam installs under the given home directory.
func SteamRegistryPaths(home string) []string {
	if home == "" {
		return nil
	}
	return []string{
		filepath.Join(home, ".steam", "registry.vdf"),
		filepath.Join(home, ".var", "app", "com.valvesoftware.Steam", ".steam", "registry.vdf"),
	}
}

// ReadRunningSteamAppID returns the running Steam app ID from the first
// readable registry file, or "" when none is running or the files are
// missing/unparseable (all failures are defensive no-signals, never errors).
func ReadRunningSteamAppID(paths []string) string {
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		m := runningAppIDRe.FindSubmatch(data)
		if m == nil {
			continue
		}
		id := string(m[1])
		if id == "0" || id == "" {
			continue
		}
		return id
	}
	return ""
}
