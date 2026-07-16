package discovery

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/gsbs/gsbs/pkg/paths"
)

// InstalledGame represents a game detected on the local machine.
type InstalledGame struct {
	GameID      string `json:"game_id"` // primary ID (Steam app ID, etc.)
	Title       string `json:"title,omitempty"`
	Launcher    string `json:"launcher"`               // steam, epic, gog, ubisoft, heroic, lutris, bottles, flatpak
	InstallPath string `json:"install_path,omitempty"` // absolute install folder when known (e.g. Steam common/)
}

// ScanOptions prepends user-configured launcher roots ahead of the per-OS
// default candidates (the client sets these from heroic_folder etc. in
// config.json and auto-detected paths).
type ScanOptions struct {
	HeroicRoots  []string // heroic config dirs (containing Games/ or store_cache/)
	LutrisRoots  []string // lutris config dirs (containing games/)
	BottlesRoots []string // bottles data dirs (containing bottles/<name>/bottle.yml)
	PrismRoots   []string // PrismLauncher data dirs (containing instances/)
}

// ScanInstalledGames returns all games detected across supported launchers
// using only the per-OS default locations.
func ScanInstalledGames() []InstalledGame {
	return ScanInstalledGamesOpts(ScanOptions{})
}

// ScanInstalledGamesOpts is ScanInstalledGames with configured launcher roots.
func ScanInstalledGamesOpts(opts ScanOptions) []InstalledGame {
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
	for _, g := range scanHeroic(opts.HeroicRoots) {
		add(g)
	}
	for _, g := range scanLutris(opts.LutrisRoots) {
		add(g)
	}
	for _, g := range scanBottles(opts.BottlesRoots) {
		add(g)
	}
	for _, g := range scanEA() {
		add(g)
	}
	for _, g := range scanPrism(opts.PrismRoots) {
		add(g)
	}
	return out
}

// candidateRoots prepends configured roots to defaults, dropping empties and
// duplicates while preserving order (configured roots win).
func candidateRoots(configured, defaults []string) []string {
	var out []string
	seen := make(map[string]bool)
	for _, r := range append(append([]string(nil), configured...), defaults...) {
		r = strings.TrimSpace(r)
		if r == "" || seen[r] {
			continue
		}
		seen[r] = true
		out = append(out, r)
	}
	return out
}

var steamAppIDRe = regexp.MustCompile(`(?m)"appid"\s+"(\d+)"`)
var steamNameRe = regexp.MustCompile(`(?m)"name"\s+"(.+)"`)
var steamInstalldirRe = regexp.MustCompile(`(?m)"installdir"\s+"([^"]+)"`)

func scanSteam() []InstalledGame {
	var out []InstalledGame
	home, _ := os.UserHomeDir()
	libraries := paths.GetSteamLibraryRoots(home)
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
			installPath := ""
			if im := steamInstalldirRe.FindStringSubmatch(text); len(im) >= 2 {
				candidate := filepath.Join(steamapps, "common", im[1])
				if _, err := os.Stat(candidate); err == nil {
					installPath = candidate
				}
			}
			out = append(out, InstalledGame{
				GameID:      idMatch[1],
				Title:       title,
				Launcher:    "steam",
				InstallPath: installPath,
			})
		}
	}
	return out
}

func scanEpic() []InstalledGame {
	var out []InstalledGame
	var manifestDirs []string
	switch runtime.GOOS {
	case "windows":
		manifestDirs = append(manifestDirs, filepath.Join(os.Getenv("ProgramData"), "Epic", "UnrealEngineLauncher", "Data", "Manifests"))
	case "darwin":
		home, _ := os.UserHomeDir()
		manifestDirs = append(manifestDirs, filepath.Join(home, "Library", "Application Support", "Epic", "UnrealEngineLauncher", "Data", "Manifests"))
	default:
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
				AppName         string `json:"AppName"`
				DisplayName     string `json:"DisplayName"`
				MainGameAppName string `json:"MainGameAppName"`
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

// heroicDefaultRoots are the per-OS Heroic config locations. The old scanner
// hardcoded ~/.config/heroic (a Linux path) everywhere except darwin, so
// Windows (%APPDATA%\heroic) and Flatpak Heroic installs were never scanned —
// even though the launcher DETECTOR already knew the Flatpak path.
func heroicDefaultRoots() []string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "windows":
		if appData := os.Getenv("APPDATA"); appData != "" {
			return []string{filepath.Join(appData, "heroic")}
		}
		return nil
	case "darwin":
		return []string{filepath.Join(home, "Library", "Application Support", "heroic")}
	default:
		return []string{
			filepath.Join(home, ".config", "heroic"),
			filepath.Join(home, ".var", "app", "com.heroicgameslauncher.hgl", "config", "heroic"),
		}
	}
}

func scanHeroic(configuredRoots []string) []InstalledGame {
	var out []InstalledGame
	for _, heroicRoot := range candidateRoots(configuredRoots, heroicDefaultRoots()) {
		// Both library layouts exist in the wild; read whichever is present
		// under EACH root (the old either/or applied to a single root).
		for _, configPath := range []string{
			filepath.Join(heroicRoot, "Games", "legendary", "library.json"),
			filepath.Join(heroicRoot, "store_cache", "legendary", "library.json"),
		} {
			data, err := os.ReadFile(configPath)
			if err != nil {
				continue
			}
			var library map[string]struct {
				Title   string `json:"title"`
				AppName string `json:"app_name"`
			}
			if json.Unmarshal(data, &library) != nil {
				continue
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
		}
	}
	return out
}

// lutrisDefaultRoots covers native and Flatpak Lutris (the Flatpak path was
// granted in the GSBS Flatpak manifest but never scanned).
func lutrisDefaultRoots() []string {
	home, _ := os.UserHomeDir()
	return []string{
		filepath.Join(home, ".config", "lutris"),
		filepath.Join(home, ".var", "app", "net.lutris.Lutris", "config", "lutris"),
	}
}

func scanLutris(configuredRoots []string) []InstalledGame {
	var out []InstalledGame
	var matches []string
	for _, root := range candidateRoots(configuredRoots, lutrisDefaultRoots()) {
		m, _ := filepath.Glob(filepath.Join(root, "games", "*.yml"))
		matches = append(matches, m...)
	}
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

// bottlesDefaultRoots covers Flatpak Bottles (the common install) and native.
func bottlesDefaultRoots() []string {
	home, _ := os.UserHomeDir()
	return []string{
		filepath.Join(home, ".var", "app", "com.usebottles.bottles", "data", "bottles"),
		filepath.Join(home, ".local", "share", "bottles"),
	}
}

func scanBottles(configuredRoots []string) []InstalledGame {
	var out []InstalledGame
	for _, root := range candidateRoots(configuredRoots, bottlesDefaultRoots()) {
		bottlesDir := filepath.Join(root, "bottles")
		entries, err := os.ReadDir(bottlesDir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			yml := filepath.Join(bottlesDir, e.Name(), "bottle.yml")
			title := e.Name()
			if data, err := os.ReadFile(yml); err == nil {
				if nm := regexp.MustCompile(`(?m)^Name:\s*(.+)$`).FindStringSubmatch(string(data)); len(nm) >= 2 {
					title = strings.TrimSpace(nm[1])
				}
			}
			out = append(out, InstalledGame{
				GameID:   e.Name(),
				Title:    title,
				Launcher: "bottles",
			})
		}
	}
	return out
}

func scanEA() []InstalledGame {
	var out []InstalledGame
	var roots []string
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "windows" {
		roots = append(roots, filepath.Join(os.Getenv("ProgramData"), "Electronic Arts", "EA Desktop", "InstallCache"))
	} else {
		roots = append(roots, filepath.Join(home, ".local", "share", "Electronic Arts", "EA Desktop", "InstallCache"))
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
				Launcher: "ea",
			})
		}
	}
	return out
}

// prismDefaultRoots covers native and Flatpak PrismLauncher.
func prismDefaultRoots() []string {
	home, _ := os.UserHomeDir()
	return []string{
		filepath.Join(home, ".local", "share", "PrismLauncher"),
		filepath.Join(home, ".var", "app", "org.prismlauncher.PrismLauncher", "data", "PrismLauncher"),
	}
}

func scanPrism(configuredRoots []string) []InstalledGame {
	var out []InstalledGame
	var matches []string
	for _, root := range candidateRoots(configuredRoots, prismDefaultRoots()) {
		m, _ := filepath.Glob(filepath.Join(root, "instances", "*", "instance.cfg"))
		matches = append(matches, m...)
	}
	for _, cfgPath := range matches {
		data, err := os.ReadFile(cfgPath)
		if err != nil {
			continue
		}
		text := string(data)
		id := filepath.Base(filepath.Dir(cfgPath))
		title := id
		if nm := regexp.MustCompile(`(?m)^name=(.+)$`).FindStringSubmatch(text); len(nm) >= 2 {
			title = strings.TrimSpace(nm[1])
		}
		out = append(out, InstalledGame{
			GameID:   id,
			Title:    title,
			Launcher: "prism",
		})
	}
	return out
}
