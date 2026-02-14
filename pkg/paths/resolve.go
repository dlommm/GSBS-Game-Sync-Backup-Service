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

// Resolver holds known roots (Steam, Ubisoft, etc.) for placeholder expansion.
type Resolver struct {
	SteamLibraries   []string // Steam library roots (e.g. C:\Program Files (x86)\Steam, /home/user/.steam/steam)
	UbisoftConnect   string   // Ubisoft Connect install path
	UserID           string   // Launcher user ID if applicable
	Home             string   // $HOME or %USERPROFILE%
	LocalAppData     string   // %LOCALAPPDATA% or ~/.local/share
	AppData          string   // %APPDATA% or ~/.config
}

// NewResolver builds a resolver with current environment.
func NewResolver() *Resolver {
	home, _ := os.UserHomeDir()
	localAppData := home
	appData := home
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
	return &Resolver{
		Home:         home,
		LocalAppData: localAppData,
		AppData:      appData,
		SteamLibraries: getSteamLibraryRoots(home),
	}
}

func getSteamLibraryRoots(home string) []string {
	var roots []string
	if runtime.GOOS == "windows" {
		roots = append(roots, filepath.Join(os.Getenv("ProgramFiles(x86)"), "Steam"))
		roots = append(roots, filepath.Join(os.Getenv("ProgramFiles"), "Steam"))
	} else {
		roots = append(roots, filepath.Join(home, ".steam", "steam"))
		roots = append(roots, filepath.Join(home, ".local", "share", "Steam"))
	}
	return appendSteamLibrariesFromVDF(roots)
}

// Resolve expands placeholders in a path template for the given OS.
// Placeholders: %USERPROFILE%, %LOCALAPPDATA%, %APPDATA%, <SteamLibrary-folder>, <Ubisoft-Connect-folder>, <user-id>
func (r *Resolver) Resolve(template string, targetOS OS) []string {
	// Normalize: Windows template uses \ and %VAR%; Linux may use <SteamLibrary-folder>/...
	template = strings.TrimSpace(template)
	if template == "" {
		return nil
	}
	// Single path expansion
	expanded := r.expandOne(template, targetOS)
	if expanded == "" {
		return nil
	}
	return []string{expanded}
}

// expandOne returns one absolute path from one template.
func (r *Resolver) expandOne(template string, targetOS OS) string {
	s := template
	// Windows-style env vars
	s = replaceEnv(s, "%USERPROFILE%", r.Home)
	s = replaceEnv(s, "%LOCALAPPDATA%", r.LocalAppData)
	s = replaceEnv(s, "%APPDATA%", r.AppData)
	s = replaceEnv(s, "<user-id>", r.UserID)
	s = replaceEnv(s, "<Ubisoft-Connect-folder>", r.UbisoftConnect)
	// Steam: first library that exists
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
				return out
			}
		}
		// No library found; use first root as default
		if len(r.SteamLibraries) > 0 {
			out := strings.ReplaceAll(s, "<SteamLibrary-folder>", r.SteamLibraries[0])
			if targetOS == Windows {
				out = filepath.FromSlash(strings.ReplaceAll(out, "/", string(filepath.Separator)))
			}
			return out
		}
	}
	if targetOS == Windows {
		s = filepath.FromSlash(strings.ReplaceAll(s, "/", string(filepath.Separator)))
	}
	return s
}

func replaceEnv(s, key, value string) string {
	if value == "" {
		return s
	}
	return strings.ReplaceAll(s, key, value)
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
