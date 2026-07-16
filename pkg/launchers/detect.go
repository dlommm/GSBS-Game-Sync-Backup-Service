package launchers

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/gsbs/gsbs/pkg/paths"
)

// DetectedPaths holds launcher install/config paths detected on this machine.
type DetectedPaths struct {
	SteamLibraries []string
	SteamUserID    string
	UbisoftConnect string
	GOGGalaxy      string
	EpicGames      string
	XboxApp        string
	EAApp          string
	Heroic         string
	Lutris         string
	Bottles        string
	Prism          string
	FlatpakSteam   string
}

// pathExists returns true if path exists on disk.
func pathExists(p string) bool {
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

// DetectPaths scans the system for known launcher locations.
func DetectPaths() DetectedPaths {
	home, _ := os.UserHomeDir()
	var out DetectedPaths

	out.SteamLibraries = paths.GetSteamLibraryRoots(home)
	if uid := paths.DetectSteamUserID(out.SteamLibraries); uid != "" {
		out.SteamUserID = uid
	}
	if runtime.GOOS == "linux" {
		flatpakSteam := filepath.Join(home, ".var", "app", "com.valvesoftware.Steam", "data", "Steam")
		if pathExists(flatpakSteam) {
			out.FlatpakSteam = flatpakSteam
			out.SteamLibraries = appendUnique(out.SteamLibraries, flatpakSteam)
		}
	}

	switch runtime.GOOS {
	case "windows":
		pf86 := os.Getenv("ProgramFiles(x86)")
		if pf86 == "" {
			pf86 = filepath.Join(os.Getenv("ProgramFiles"), "..", "Program Files (x86)")
		}
		ubisoft := filepath.Join(pf86, "Ubisoft", "Ubisoft Game Launcher")
		if pathExists(ubisoft) {
			out.UbisoftConnect = ubisoft
		}
		gog := filepath.Join(pf86, "GOG Galaxy")
		if pathExists(gog) {
			out.GOGGalaxy = gog
		}
		epic := filepath.Join(os.Getenv("ProgramFiles"), "Epic Games")
		if pathExists(epic) {
			out.EpicGames = epic
		}
		ea := filepath.Join(os.Getenv("ProgramFiles"), "Electronic Arts", "EA Desktop")
		if pathExists(ea) {
			out.EAApp = ea
		}
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData == "" {
			localAppData = filepath.Join(home, "AppData", "Local")
		}
		xbox := filepath.Join(localAppData, "Packages")
		if pathExists(xbox) {
			out.XboxApp = xbox
		}
	case "darwin":
		// macOS previously fell into the Linux branch and probed XDG/.var
		// paths that never exist there, so "Detect launcher paths" found
		// only Steam. These match the discovery scanners' darwin knowledge.
		appSupport := filepath.Join(home, "Library", "Application Support")
		gogGames := filepath.Join(home, "GOG Games")
		if pathExists(gogGames) {
			out.GOGGalaxy = gogGames
		}
		epic := filepath.Join(appSupport, "Epic")
		if pathExists(epic) {
			out.EpicGames = epic
		}
		heroic := filepath.Join(appSupport, "heroic")
		if pathExists(heroic) {
			out.Heroic = heroic
		}
	default: // linux
		gogGames := filepath.Join(home, "GOG Games")
		if pathExists(gogGames) {
			out.GOGGalaxy = gogGames
		}
		epic := filepath.Join(home, ".local", "share", "Epic")
		if pathExists(epic) {
			out.EpicGames = epic
		}
		ea := filepath.Join(home, ".local", "share", "Electronic Arts", "EA Desktop")
		if pathExists(ea) {
			out.EAApp = ea
		}
		heroic := filepath.Join(home, ".config", "heroic")
		if pathExists(heroic) {
			out.Heroic = heroic
		}
		heroicFlatpak := filepath.Join(home, ".var", "app", "com.heroicgameslauncher.hgl", "config", "heroic")
		if pathExists(heroicFlatpak) {
			if out.Heroic == "" {
				out.Heroic = heroicFlatpak
			}
		}
		lutris := filepath.Join(home, ".config", "lutris")
		if pathExists(lutris) {
			out.Lutris = lutris
		}
		bottles := filepath.Join(home, ".var", "app", "com.usebottles.bottles", "data", "bottles")
		if pathExists(bottles) {
			out.Bottles = bottles
		}
		prism := filepath.Join(home, ".local", "share", "PrismLauncher")
		if pathExists(prism) {
			out.Prism = prism
		}
	}
	return out
}

func appendUnique(slice []string, p string) []string {
	for _, s := range slice {
		if s == p {
			return slice
		}
	}
	return append(slice, p)
}

// ApplyToResolver fills empty resolver fields from detected paths.
func (d DetectedPaths) ApplyToResolver(r *paths.Resolver) {
	if len(d.SteamLibraries) > 0 {
		r.SteamLibraries = mergeRoots(r.SteamLibraries, d.SteamLibraries)
	}
	if d.SteamUserID != "" && r.UserID == "" {
		r.UserID = d.SteamUserID
	}
	if d.UbisoftConnect != "" && r.UbisoftConnect == "" {
		r.UbisoftConnect = d.UbisoftConnect
	}
	if d.GOGGalaxy != "" && r.GOGGalaxy == "" {
		r.GOGGalaxy = d.GOGGalaxy
	}
	if d.EpicGames != "" && r.EpicGames == "" {
		r.EpicGames = d.EpicGames
	}
	if d.XboxApp != "" && r.XboxApp == "" {
		r.XboxApp = d.XboxApp
	}
	if d.EAApp != "" && r.EAApp == "" {
		r.EAApp = d.EAApp
	}
	if d.Heroic != "" && r.Heroic == "" {
		r.Heroic = d.Heroic
	}
	if d.Lutris != "" && r.Lutris == "" {
		r.Lutris = d.Lutris
	}
	if d.Bottles != "" && r.Bottles == "" {
		r.Bottles = d.Bottles
	}
	if d.Prism != "" && r.Prism == "" {
		r.Prism = d.Prism
	}
	if d.FlatpakSteam != "" && r.FlatpakSteam == "" {
		r.FlatpakSteam = d.FlatpakSteam
	}
}

func mergeRoots(a, b []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, r := range append(a, b...) {
		if r == "" || seen[r] {
			continue
		}
		seen[r] = true
		out = append(out, r)
	}
	return out
}
