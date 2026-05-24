package discovery

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// InstalledGame represents a game detected on the local machine.
type InstalledGame struct {
	GameID   string `json:"game_id"`   // primary ID (Steam app ID, etc.)
	Title    string `json:"title,omitempty"`
	Launcher string `json:"launcher"`  // steam, epic, gog, ubisoft, heroic, lutris, bottles, flatpak
}

// ScanInstalledGames returns all games detected across supported launchers.
func ScanInstalledGames() []InstalledGame {
	var out []InstalledGame
	seen := make(map[string]bool)
	add := func(g InstalledGame) {
		if g.GameID == "" {
			return
		}
		key := g.Launcher + ":" + g.GameID
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, g)
	}
	for _, g := range scanSteam() {
		add(g)
	}
	for _, g := range scanEpic() {
		add(g)
	}
	for _, g := range scanGOG() {
		add(g)
	}
	for _, g := range scanUbisoft() {
		add(g)
	}
	for _, g := range scanHeroic() {
		add(g)
	}
	for _, g := range scanLutris() {
		add(g)
	}
	for _, g := range scanBottles() {
		add(g)
	}
	return out
}

var steamAppIDRe = regexp.MustCompile(`(?m)"appid"\s+"(\d+)"`)
var steamNameRe = regexp.MustCompile(`(?m)"name"\s+"(.+)"`)

func scanSteam() []InstalledGame {
	var out []InstalledGame
	home, _ := os.UserHomeDir()
	var libraries []string
	if runtime.GOOS == "windows" {
		libraries = append(libraries,
			filepath.Join(os.Getenv("ProgramFiles(x86)"), "Steam"),
			filepath.Join(os.Getenv("ProgramFiles"), "Steam"),
		)
	} else {
		libraries = append(libraries,
			filepath.Join(home, ".steam", "steam"),
			filepath.Join(home, ".local", "share", "Steam"),
		)
	}
	for _, lib := range libraries {
		steamapps := filepath.Join(lib, "steamapps")
		matches, _ := filepath.Glob(filepath.Join(steamapps, "appmanifest_*.acf"))
		for _, m := range matches {
			data, err := os.ReadFile(m)
			if err != nil {
				continue
			}
			text := string(data)
			idMatch := steamAppIDRe.FindStringSubmatch(text)
			if len(idMatch) < 2 {
				continue
			}
			title := ""
			if nm := steamNameRe.FindStringSubmatch(text); len(nm) >= 2 {
				title = nm[1]
			}
			out = append(out, InstalledGame{
				GameID:   idMatch[1],
				Title:    title,
				Launcher: "steam",
			})
		}
	}
	return out
}

func scanEpic() []InstalledGame {
	var out []InstalledGame
	var manifestDirs []string
	if runtime.GOOS == "windows" {
		manifestDirs = append(manifestDirs, filepath.Join(os.Getenv("ProgramData"), "Epic", "UnrealEngineLauncher", "Data", "Manifests"))
	} else {
		home, _ := os.UserHomeDir()
		manifestDirs = append(manifestDirs, filepath.Join(home, ".local", "share", "Epic", "UnrealEngineLauncher", "Data", "Manifests"))
	}
	for _, dir := range manifestDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".item") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			var meta struct {
				AppName          string `json:"AppName"`
				DisplayName      string `json:"DisplayName"`
				MainGameAppName  string `json:"MainGameAppName"`
			}
			if json.Unmarshal(data, &meta) != nil {
				continue
			}
			id := meta.MainGameAppName
			if id == "" {
				id = meta.AppName
			}
			if id == "" {
				continue
			}
			out = append(out, InstalledGame{
				GameID:   id,
				Title:    meta.DisplayName,
				Launcher: "epic",
			})
		}
	}
	return out
}

func scanGOG() []InstalledGame {
	var out []InstalledGame
	home, _ := os.UserHomeDir()
	var roots []string
	if runtime.GOOS == "windows" {
		pf := os.Getenv("ProgramFiles(x86)")
		if pf != "" {
			roots = append(roots, filepath.Join(pf, "GOG Galaxy", "Games"))
		}
		roots = append(roots, filepath.Join(home, "GOG Games"))
	} else {
		roots = append(roots, filepath.Join(home, "GOG Games"))
	}
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			out = append(out, InstalledGame{
				GameID:   name,
				Title:    name,
				Launcher: "gog",
			})
		}
	}
	return out
}

func scanUbisoft() []InstalledGame {
	var out []InstalledGame
	if runtime.GOOS != "windows" {
		return out
	}
	roots := []string{
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Ubisoft", "Ubisoft Game Launcher", "games"),
		filepath.Join(os.Getenv("ProgramFiles"), "Ubisoft", "Ubisoft Game Launcher", "games"),
	}
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			out = append(out, InstalledGame{
				GameID:   e.Name(),
				Title:    e.Name(),
				Launcher: "ubisoft",
			})
		}
	}
	return out
}

func scanHeroic() []InstalledGame {
	var out []InstalledGame
	home, _ := os.UserHomeDir()
	configPath := filepath.Join(home, ".config", "heroic", "Games", "legendary", "library.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		// Also try games store
		configPath = filepath.Join(home, ".config", "heroic", "store_cache", "legendary", "library.json")
		data, err = os.ReadFile(configPath)
		if err != nil {
			return out
		}
	}
	var library map[string]struct {
		Title   string `json:"title"`
		AppName string `json:"app_name"`
	}
	if json.Unmarshal(data, &library) != nil {
		return out
	}
	for id, g := range library {
		gameID := g.AppName
		if gameID == "" {
			gameID = id
		}
		out = append(out, InstalledGame{
			GameID:   gameID,
			Title:    g.Title,
			Launcher: "heroic",
		})
	}
	return out
}

func scanLutris() []InstalledGame {
	var out []InstalledGame
	home, _ := os.UserHomeDir()
	gamesDir := filepath.Join(home, ".config", "lutris", "games")
	matches, _ := filepath.Glob(filepath.Join(gamesDir, "*.yml"))
	slugRe := regexp.MustCompile(`(?m)^slug:\s*(.+)$`)
	nameRe := regexp.MustCompile(`(?m)^name:\s*(.+)$`)
	for _, m := range matches {
		data, err := os.ReadFile(m)
		if err != nil {
			continue
		}
		text := string(data)
		id := filepath.Base(m)
		id = strings.TrimSuffix(id, ".yml")
		if sm := slugRe.FindStringSubmatch(text); len(sm) >= 2 {
			id = strings.TrimSpace(sm[1])
		}
		title := id
		if nm := nameRe.FindStringSubmatch(text); len(nm) >= 2 {
			title = strings.TrimSpace(nm[1])
		}
		out = append(out, InstalledGame{
			GameID:   id,
			Title:    title,
			Launcher: "lutris",
		})
	}
	return out
}

func scanBottles() []InstalledGame {
	var out []InstalledGame
	home, _ := os.UserHomeDir()
	bottlesDir := filepath.Join(home, ".var", "app", "com.usebottles.bottles", "data", "bottles", "bottles")
	entries, err := os.ReadDir(bottlesDir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		out = append(out, InstalledGame{
			GameID:   e.Name(),
			Title:    e.Name(),
			Launcher: "bottles",
		})
	}
	return out
}

// MatchManifest filters manifest game IDs against installed games.
// Returns installed game IDs that appear in the manifest (by game_id or title match).
func MatchManifest(installed []InstalledGame, manifestGameIDs map[string]bool) []InstalledGame {
	var matched []InstalledGame
	for _, g := range installed {
		if manifestGameIDs[g.GameID] {
			matched = append(matched, g)
			continue
		}
		// Title-based fuzzy match for GOG folder names vs manifest titles
		for id := range manifestGameIDs {
			if strings.EqualFold(g.Title, id) || strings.Contains(strings.ToLower(id), strings.ToLower(g.GameID)) {
				matched = append(matched, g)
				break
			}
		}
	}
	return matched
}
