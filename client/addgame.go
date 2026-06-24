package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/gsbs/gsbs/pkg/paths"
	"github.com/gsbs/gsbs/pkg/saverule"
	"github.com/gsbs/gsbs/pkg/types"
)

// manualGameResult is one manifest match for the add-game search UI.
type manualGameResult struct {
	GameID    string `json:"game_id"`
	Title     string `json:"title"`
	Platform  string `json:"platform"`
	Directory string `json:"directory"` // resolved for current OS (best candidate); may be empty
	Template  string `json:"template"`  // raw path template from manifest
	Exists    bool   `json:"exists"`    // whether the resolved directory exists locally
	Unsafe    bool   `json:"unsafe"`    // resolved to a home/system root — too broad to watch
}

// searchManifestGames returns manifest games for the current OS whose title or
// game_id contains q (case-insensitive), with templates resolved for this machine.
// An empty query returns the first maxResults games (sorted by title).
func searchManifestGames(q string, maxResults int) []manualGameResult {
	q = strings.ToLower(strings.TrimSpace(q))
	cfg, _ := loadConfig()
	if cfg == nil {
		cfg = blankConfig()
	}
	resolver := configureResolverFromConfig(cfg)
	currentOS := paths.CurrentOS()
	installRoots := BuildInstallRootsByGame(cfg, loadDiscoveryCache())
	entries := LoadManifestFromDisk()

	var out []manualGameResult
	seen := make(map[string]bool)
	for _, e := range entries {
		// On Linux, a Windows-platform rule for a Steam game is a Proton candidate
		// (resolved via the compatdata prefix), so include those too.
		protonCandidate := currentOS == paths.Linux && e.Platform == string(paths.Windows) && len(e.SteamAppIDs) > 0
		if e.Platform != string(currentOS) && !protonCandidate {
			continue
		}
		title := strings.TrimSpace(e.GameTitle)
		if title == "" {
			title = e.GameID
		}
		if q != "" && !strings.Contains(strings.ToLower(title+" "+e.GameID), q) {
			continue
		}
		for _, rule := range saveRulesForEntry(e) {
			key := e.GameID + "\x00" + rule.Directory
			if seen[key] {
				continue
			}
			seen[key] = true
			var dir string
			var exists bool
			if protonCandidate {
				dir, exists = bestResolvedProtonDir(resolver, rule, e.SteamAppIDs)
			} else {
				dir, exists = bestResolvedDir(resolver, rule.Directory, currentOS, e.GameID, installRoots)
			}
			unsafe := dir != "" && resolver.UnsafeWatchTarget(dir, rule.SyncAll, rule.Recursive, rule.IncludePatterns)
			if unsafe {
				// Don't present a too-broad folder as an addable "folder found".
				exists = false
			}
			out = append(out, manualGameResult{
				GameID:    e.GameID,
				Title:     title,
				Platform:  e.Platform,
				Directory: dir,
				Template:  rule.Directory,
				Exists:    exists,
				Unsafe:    unsafe,
			})
		}
	}
	// Show games whose folder exists first, then alphabetically by title.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Exists != out[j].Exists {
			return out[i].Exists
		}
		return strings.ToLower(out[i].Title) < strings.ToLower(out[j].Title)
	})
	if maxResults > 0 && len(out) > maxResults {
		out = out[:maxResults]
	}
	return out
}

// bestResolvedDir resolves a template for the current OS and returns the first
// existing candidate (preferred) or the first resolved candidate otherwise.
func bestResolvedDir(resolver *paths.Resolver, template string, currentOS paths.OS, gameID string, installRoots map[string][]string) (string, bool) {
	first := ""
	for _, abs := range resolveManifestTemplate(resolver, template, currentOS, gameID, installRoots) {
		if abs == "" {
			continue
		}
		if first == "" {
			first = abs
		}
		if paths.WatchDirExists(abs) {
			return abs, true
		}
	}
	return first, false
}

// bestResolvedProtonDir resolves a Windows-style rule to its Proton compatdata
// path(s) for the given Steam App IDs, returning the first existing candidate
// (preferred) or the first resolved candidate otherwise.
func bestResolvedProtonDir(resolver *paths.Resolver, rule types.SaveRule, appIDs []string) (string, bool) {
	first := ""
	for _, abs := range resolveProtonPaths(resolver, rule, appIDs) {
		if abs == "" {
			continue
		}
		if first == "" {
			first = abs
		}
		if paths.WatchDirExists(abs) {
			return abs, true
		}
	}
	return first, false
}

// addManualWatchPath adds a user-specified game folder to config.WatchPaths and
// reloads sync so the watcher picks it up. The directory must already exist
// (GSBS never creates save folders). gameID is required.
func addManualWatchPath(gameID, title, directory string, syncAll bool, patterns []string) error {
	gameID = strings.TrimSpace(gameID)
	directory = strings.TrimSpace(directory)
	if gameID == "" {
		return fmt.Errorf("game id is required")
	}
	if directory == "" {
		return fmt.Errorf("folder path is required")
	}
	info, err := os.Stat(directory)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("folder does not exist: %s", directory)
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if cfg == nil {
		cfg = blankConfig()
	}

	patterns = trimPatterns(patterns)
	if len(patterns) > 0 {
		syncAll = false
	}
	// A top-level root (home/XDG/system) may only be watched non-recursively for
	// specific named files — never recursively, so we never sweep unrelated files.
	resolver := configureResolverFromConfig(cfg)
	recursive := true
	if resolver.UnsafeWatchDir(directory) {
		recursive = false
	}
	if resolver.UnsafeWatchTarget(directory, syncAll, recursive, patterns) {
		return fmt.Errorf("refusing to watch %q — it's a top-level folder. Pick the game's specific save folder, or list the exact file name(s) to sync from it (no wildcards like *)", directory)
	}
	rule := types.SaveRule{Directory: directory, SyncAll: syncAll, IncludePatterns: patterns}
	ruleKey := saverule.RuleKey(gameID, rule)
	wp := watchPath{
		GameID:          gameID,
		PathKey:         ruleKey,
		RuleKey:         ruleKey,
		Directory:       directory,
		IncludePatterns: patterns,
		Recursive:       recursive,
		SyncAll:         syncAll,
	}
	for _, existing := range cfg.WatchPaths {
		if existing.GameID == wp.GameID && (existing.PathKey == wp.PathKey || existing.Directory == wp.Directory) {
			return fmt.Errorf("this game folder is already being watched")
		}
	}
	cfg.WatchPaths = append(cfg.WatchPaths, wp)
	if err := saveConfig(cfg); err != nil {
		return err
	}
	if strings.TrimSpace(title) != "" {
		cacheGameTitle(gameID, title)
	}
	// Restart sync with the updated config so the new watch path takes effect.
	restartSync(cfg)
	return nil
}

func trimPatterns(in []string) []string {
	var out []string
	for _, p := range in {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
