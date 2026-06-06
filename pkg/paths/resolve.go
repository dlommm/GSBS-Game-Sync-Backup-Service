package paths

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// OS type for path resolution.
type OS string

const (
	Windows OS = "windows"
	Linux   OS = "linux"
)

// CurrentOS returns the current OS for path resolution.
func CurrentOS() OS {
	if runtime.GOOS == "windows" {
		return Windows
	}
	return Linux
}

// Resolver holds known roots (Steam, Ubisoft, GOG, Epic, Xbox, etc.) for placeholder expansion.
type Resolver struct {
	SteamLibraries []string // Steam library roots (e.g. C:\Program Files (x86)\Steam, /home/user/.steam/steam)
	UbisoftConnect string   // Ubisoft Connect install path
	GOGGalaxy      string   // GOG Galaxy install path (e.g. C:\Program Files (x86)\GOG Galaxy)
	EpicGames      string   // Epic Games Store install path (e.g. C:\Program Files\Epic Games)
	XboxApp        string   // Xbox App / Game Pass root (e.g. C:\XboxGames or %LOCALAPPDATA%\Packages)
	UserID         string   // Launcher user ID if applicable
	Home           string   // $HOME or %USERPROFILE%
	LocalAppData   string   // %LOCALAPPDATA% or ~/.local/share
	AppData        string   // %APPDATA% or ~/.config
	Heroic         string   // Heroic Games Launcher config root
	Lutris         string   // Lutris config root
	ProgramData    string   // %PROGRAMDATA%
	ProgramFiles   string   // %PROGRAMFILES%
	EAApp          string   // EA App / Origin install root
	Bottles        string   // Bottles data root (Flatpak)
	Prism          string   // Prism Launcher data root
	FlatpakSteam   string   // Flatpak Steam data root
	XDGCacheHome   string   // XDG cache dir: $XDG_CACHE_HOME on Linux (default ~/.cache), %LOCALAPPDATA%\cache on Windows
	InstalledSteam []string // Steam App IDs installed locally (for Proton path expansion)
}

// NewResolver builds a resolver with current environment.
func NewResolver() *Resolver {
	home, _ := os.UserHomeDir()
	var localAppData, appData string
	if runtime.GOOS == "windows" {
		localAppData = os.Getenv("LOCALAPPDATA")
		if localAppData == "" {
			localAppData = filepath.Join(home, "AppData", "Local")
		}
		appData = os.Getenv("APPDATA")
		if appData == "" {
			appData = filepath.Join(home, "AppData", "Roaming")
		}
	} else {
		localAppData = filepath.Join(home, ".local", "share")
		appData = filepath.Join(home, ".config")
	}
	programData := ""
	programFiles := ""
	if runtime.GOOS == "windows" {
		programData = os.Getenv("PROGRAMDATA")
		if programData == "" {
			programData = filepath.Join(os.Getenv("SystemDrive")+"\\", "ProgramData")
		}
		programFiles = os.Getenv("ProgramFiles")
	}
	xdgCacheHome := getXDGCacheHome(home, localAppData)
	r := &Resolver{
		Home:           home,
		LocalAppData:   localAppData,
		AppData:        appData,
		ProgramData:    programData,
		ProgramFiles:   programFiles,
		XDGCacheHome:   xdgCacheHome,
		SteamLibraries: getSteamLibraryRoots(home),
		GOGGalaxy:      getDefaultGOGGalaxy(home),
		EpicGames:      getDefaultEpicGames(home),
		XboxApp:        getDefaultXboxApp(localAppData),
		Heroic:         filepath.Join(appData, "heroic"),
		Lutris:         filepath.Join(appData, "lutris"),
	}
	r.UbisoftConnect = getDefaultUbisoftConnect()
	return r
}

func getDefaultUbisoftConnect() string {
	if runtime.GOOS == "windows" {
		pf := os.Getenv("ProgramFiles(x86)")
		if pf == "" {
			pf = filepath.Join(os.Getenv("ProgramFiles"), "..", "Program Files (x86)")
		}
		p := filepath.Join(pf, "Ubisoft", "Ubisoft Game Launcher")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func getDefaultGOGGalaxy(home string) string {
	if runtime.GOOS == "windows" {
		pf := os.Getenv("ProgramFiles(x86)")
		if pf == "" {
			pf = filepath.Join(os.Getenv("ProgramFiles"), "..", "Program Files (x86)")
		}
		return filepath.Join(pf, "GOG Galaxy")
	}
	return filepath.Join(home, "GOG Games")
}

func getDefaultEpicGames(home string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("ProgramFiles"), "Epic Games")
	}
	return filepath.Join(home, ".local", "share", "Epic")
}

func getDefaultXboxApp(localAppData string) string {
	if runtime.GOOS == "windows" {
		// Common Game Pass install location; user can override to C:\XboxGames etc.
		return filepath.Join(localAppData, "Packages")
	}
	return ""
}

// getXDGCacheHome returns the XDG cache directory.
// On Linux: $XDG_CACHE_HOME if set, otherwise ~/.cache.
// On Windows: %LOCALAPPDATA%\cache (mirrors common usage by ported Linux games).
func getXDGCacheHome(home, localAppData string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(localAppData, "cache")
	}
	if v := os.Getenv("XDG_CACHE_HOME"); v != "" {
		return v
	}
	return filepath.Join(home, ".cache")
}

func getSteamLibraryRoots(home string) []string {
	return GetSteamLibraryRoots(home)
}

// GetSteamLibraryRoots returns default Steam library roots plus VDF-discovered libraries.
func GetSteamLibraryRoots(home string) []string {
	var roots []string
	if runtime.GOOS == "windows" {
		roots = append(roots, filepath.Join(os.Getenv("ProgramFiles(x86)"), "Steam"))
		roots = append(roots, filepath.Join(os.Getenv("ProgramFiles"), "Steam"))
	} else {
		roots = append(roots, filepath.Join(home, ".steam", "steam"))
		roots = append(roots, filepath.Join(home, ".local", "share", "Steam"))
		flatpak := filepath.Join(home, ".var", "app", "com.valvesoftware.Steam", "data", "Steam")
		if _, err := os.Stat(flatpak); err == nil {
			roots = append(roots, flatpak)
		}
	}
	return appendSteamLibrariesFromVDF(roots)
}

// Resolve expands placeholders in a path template for the given OS (first matching Steam library).
func (r *Resolver) Resolve(template string, targetOS OS) []string {
	return r.ResolveAll(template, targetOS)
}

// ResolveAll expands placeholders and returns all valid path candidates (every Steam library).
func (r *Resolver) ResolveAll(template string, targetOS OS) []string {
	template = strings.TrimSpace(template)
	if template == "" {
		return nil
	}
	if strings.Contains(template, "<SteamLibrary-folder>") {
		var out []string
		seen := make(map[string]bool)
		for _, root := range r.SteamLibraries {
			if root == "" {
				continue
			}
			expanded := r.expandWithSteamRoot(template, root, targetOS)
			if expanded != "" && !seen[expanded] {
				seen[expanded] = true
				out = append(out, expanded)
			}
		}
		if len(out) == 0 && len(r.SteamLibraries) > 0 {
			if e := r.expandWithSteamRoot(template, r.SteamLibraries[0], targetOS); e != "" {
				out = append(out, e)
			}
		}
		return out
	}
	if e := r.expandOne(template, targetOS); e != "" {
		return []string{e}
	}
	return nil
}

// ResolveAllForGame expands placeholders and resolves <game-install-folder> using per-game install roots.
func (r *Resolver) ResolveAllForGame(template string, targetOS OS, installRoots []string) []string {
	template = strings.TrimSpace(template)
	if template == "" {
		return nil
	}
	if strings.Contains(template, "<game-install-folder>") {
		if len(installRoots) == 0 {
			return nil
		}
		var out []string
		seen := make(map[string]bool)
		for _, root := range installRoots {
			root = strings.TrimSpace(root)
			if root == "" {
				continue
			}
			root = strings.TrimRight(root, `/\`)
			expanded := strings.ReplaceAll(template, "<game-install-folder>", root)
			for _, p := range r.ResolveAll(expanded, targetOS) {
				if p != "" && !seen[p] {
					seen[p] = true
					out = append(out, p)
				}
			}
		}
		return out
	}
	return r.ResolveAll(template, targetOS)
}

func cleanResolvedPath(p string) string {
	if p == "" {
		return ""
	}
	return filepath.Clean(p)
}

func (r *Resolver) expandWithSteamRoot(template, root string, targetOS OS) string {
	s := template
	s = replaceEnv(s, "<SteamLibrary-folder>", root)
	return cleanResolvedPath(r.expandOne(s, targetOS))
}

// expandOne returns one absolute path from one template.
func (r *Resolver) expandOne(template string, targetOS OS) string {
	s := template
	// Windows-style env vars
	s = replaceEnv(s, "%USERPROFILE%", r.Home)
	s = replaceEnv(s, "%LOCALAPPDATA%", r.LocalAppData)
	s = replaceEnv(s, "%APPDATA%", r.AppData)
	s = replaceEnv(s, "%PROGRAMDATA%", r.ProgramData)
	s = replaceEnv(s, "%PROGRAMFILES%", r.ProgramFiles)
	s = replaceEnv(s, "%PROGRAMFILES(x86)%", r.programFilesX86())
	s = replaceEnv(s, "%PUBLIC%", r.publicFolder())
	s = replaceEnv(s, "<user-id>", r.UserID)
	s = replaceEnv(s, "<Ubisoft-Connect-folder>", r.UbisoftConnect)
	s = replaceEnv(s, "<GOG-Galaxy-folder>", r.GOGGalaxy)
	s = replaceEnv(s, "<Epic-Games-folder>", r.EpicGames)
	s = replaceEnv(s, "<Xbox-App-folder>", r.XboxApp)
	s = replaceEnv(s, "<Heroic-folder>", r.Heroic)
	s = replaceEnv(s, "<Lutris-folder>", r.Lutris)
	s = replaceEnv(s, "<EA-App-folder>", r.EAApp)
	s = replaceEnv(s, "<Bottles-folder>", r.Bottles)
	s = replaceEnv(s, "<Prism-folder>", r.Prism)
	s = replaceEnv(s, "<Flatpak-Steam-folder>", r.FlatpakSteam)
	s = replaceEnv(s, "<xdg-cache-home>", r.XDGCacheHome)
	// Steam: first library that exists (when expandOne called without pre-replaced steam root)
	if strings.Contains(s, "<SteamLibrary-folder>") {
		for _, root := range r.SteamLibraries {
			if _, err := os.Stat(root); err == nil {
				out := strings.ReplaceAll(s, "<SteamLibrary-folder>", root)
				// Normalize separators for target OS
				if targetOS == Windows {
					out = filepath.FromSlash(strings.ReplaceAll(out, "/", string(filepath.Separator)))
				} else {
					out = filepath.ToSlash(out)
				}
				return cleanResolvedPath(out)
			}
		}
		// No library found; use first root as default
		if len(r.SteamLibraries) > 0 {
			out := strings.ReplaceAll(s, "<SteamLibrary-folder>", r.SteamLibraries[0])
			if targetOS == Windows {
				out = filepath.FromSlash(strings.ReplaceAll(out, "/", string(filepath.Separator)))
			}
			return cleanResolvedPath(out)
		}
	}
	if targetOS == Windows {
		s = filepath.FromSlash(strings.ReplaceAll(s, "/", string(filepath.Separator)))
	}
	return cleanResolvedPath(s)
}

func (r *Resolver) programFilesX86() string {
	if runtime.GOOS == "windows" {
		if p := os.Getenv("ProgramFiles(x86)"); p != "" {
			return p
		}
		pf := r.ProgramFiles
		if pf != "" {
			return filepath.Join(filepath.Dir(pf), "Program Files (x86)")
		}
	}
	return ""
}

func replaceEnv(s, key, value string) string {
	if value == "" {
		return s
	}
	return strings.ReplaceAll(s, key, value)
}

func (r *Resolver) publicFolder() string {
	if runtime.GOOS == "windows" {
		if p := os.Getenv("PUBLIC"); p != "" {
			return p
		}
		return filepath.Join(r.Home, "..", "Public")
	}
	return r.Home
}

// PathExists returns true if the directory for the given path exists (for "don't push until folder exists").
func PathExists(absPath string) bool {
	dir := filepath.Dir(absPath)
	info, err := os.Stat(dir)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// EnsureDir creates the parent directory of path if needed.
func EnsureDir(path string) error {
	return os.MkdirAll(filepath.Dir(path), 0755)
}
