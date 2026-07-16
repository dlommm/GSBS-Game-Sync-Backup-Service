package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gsbs/gsbs/pkg/discovery"
	"github.com/gsbs/gsbs/pkg/pcgw"
	"github.com/gsbs/gsbs/pkg/types"
)

// discoveryCache is persisted scan results.
type discoveryCache struct {
	LastScanAt           string                    `json:"last_scan_at"`
	InstalledGames       []discovery.InstalledGame `json:"installed_games"`
	MatchedGameIDs       []string                  `json:"matched_game_ids"`
	MatchedGames         []discovery.MatchedGame   `json:"matched_games,omitempty"`
	DisabledGameIDs      []string                  `json:"disabled_game_ids,omitempty"`
	IDMap                map[string]string         `json:"id_map,omitempty"`
	FirstRunSummaryShown bool                      `json:"first_run_summary_shown,omitempty"`
}

func discoveryPath() string {
	dir, _ := os.UserConfigDir()
	return filepath.Join(dir, "gsbs", "discovery.json")
}

func loadDiscoveryCache() discoveryCache {
	data, err := os.ReadFile(discoveryPath())
	if err != nil {
		return discoveryCache{IDMap: make(map[string]string)}
	}
	var c discoveryCache
	if json.Unmarshal(data, &c) != nil {
		return discoveryCache{IDMap: make(map[string]string)}
	}
	if c.IDMap == nil {
		c.IDMap = make(map[string]string)
	}
	return c
}

func saveDiscoveryCache(c discoveryCache) error {
	dir := filepath.Dir(discoveryPath())
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	c.LastScanAt = time.Now().UTC().Format(time.RFC3339)
	if c.IDMap == nil {
		c.IDMap = make(map[string]string)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := discoveryPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, discoveryPath())
}

// OnDiscoveryResult is called after discovery with count of newly matched games. Tray may show notification.
var OnDiscoveryResult func(newGames int)

// OnFirstRunDiscovery is called once when games are first discovered (summary notification).
var OnFirstRunDiscovery func(games []discovery.MatchedGame)

// OnDiscoveryUpdated is called after each discovery scan (for setup wizard / tray refresh).
var OnDiscoveryUpdated func()

// discoveryState holds the latest scan for watch-path filtering.
// All accesses must hold discoveryMu (RLock for reads, Lock for writes).
var discoveryMu sync.RWMutex
var discoveryState struct {
	MatchedGameIDs  map[string]bool
	MatchedGames    []discovery.MatchedGame
	DisabledGameIDs map[string]bool
	InstalledSteam  []string
}

func initDiscoveryState() {
	discoveryMu.Lock()
	discoveryState.MatchedGameIDs = make(map[string]bool)
	discoveryState.DisabledGameIDs = make(map[string]bool)
	discoveryMu.Unlock()
}

// activeGameIDSet returns manifest game IDs to watch (matched minus disabled).
func activeGameIDSet() map[string]bool {
	discoveryMu.RLock()
	defer discoveryMu.RUnlock()
	out := make(map[string]bool)
	for id := range discoveryState.MatchedGameIDs {
		if !discoveryState.DisabledGameIDs[id] {
			out[id] = true
		}
	}
	return out
}

// toggleDiscoveredGame enables or disables sync for a discovered game_id.
func toggleDiscoveredGame(gameID string, enabled bool) error {
	cache := loadDiscoveryCache()
	disabled := make(map[string]bool)
	for _, id := range cache.DisabledGameIDs {
		disabled[id] = true
	}
	if enabled {
		delete(disabled, gameID)
	} else {
		disabled[gameID] = true
	}
	var disabledList []string
	for id := range disabled {
		disabledList = append(disabledList, id)
	}
	cache.DisabledGameIDs = disabledList
	discoveryMu.Lock()
	discoveryState.DisabledGameIDs = disabled
	discoveryMu.Unlock()
	if err := saveDiscoveryCache(cache); err != nil {
		return err
	}
	if OnDiscoveryUpdated != nil {
		OnDiscoveryUpdated()
	}
	return nil
}

func isGameDisabled(gameID string) bool {
	discoveryMu.RLock()
	defer discoveryMu.RUnlock()
	return discoveryState.DisabledGameIDs[gameID]
}

func installedSteamAppIDs(games []discovery.InstalledGame) []string {
	var ids []string
	for _, g := range games {
		if g.Launcher == "steam" && g.GameID != "" {
			ids = append(ids, g.GameID)
		}
	}
	return ids
}

// DiscoveredInstallRootsByGame maps manifest game_id to install folders found during discovery.
func DiscoveredInstallRootsByGame(cache discoveryCache) map[string][]string {
	if len(cache.InstalledGames) == 0 {
		return nil
	}
	m := make(map[string][]string)
	for _, g := range cache.InstalledGames {
		if g.InstallPath == "" {
			continue
		}
		manifestID := g.GameID
		if cache.IDMap != nil {
			if mapped := cache.IDMap[g.Launcher+":"+g.GameID]; mapped != "" {
				manifestID = mapped
			}
		}
		m[manifestID] = appendUniquePath(m[manifestID], g.InstallPath)
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

func appendUniquePath(slice []string, p string) []string {
	for _, s := range slice {
		if s == p {
			return slice
		}
	}
	return append(slice, p)
}

// resolveUnmatchedSteam tries PCGW lookup for unmatched Steam games (rate-limited, cached).
func resolveUnmatchedSteam(installed []discovery.InstalledGame, idx *discovery.ManifestV2Index, idMap map[string]string) {
	client := pcgw.NewClient()
	ctx := context.Background()
	for _, g := range installed {
		if g.Launcher != "steam" {
			continue
		}
		key := g.Launcher + ":" + g.GameID
		if idMap[key] != "" {
			continue
		}
		if idx.ResolveManifestGameID(g, idMap) != "" {
			continue
		}
		pageID, err := client.GetPageIDBySteamAppID(ctx, g.GameID)
		if err != nil {
			log.Printf("discovery: PCGW lookup failed steam:%s: %v", g.GameID, err)
			continue
		}
		idMap[key] = pageID
		log.Printf("discovery: resolved steam:%s -> manifest %s", g.GameID, pageID)
	}
}

// currentScanOptions prepends the user's configured launcher folders (config
// heroic_folder/lutris_folder/…) to the per-OS scan defaults — they were
// previously honored by the path RESOLVER but ignored by discovery.
func currentScanOptions() discovery.ScanOptions {
	cfg, err := loadConfig()
	if err != nil || cfg == nil {
		return discovery.ScanOptions{}
	}
	opts := discovery.ScanOptions{}
	if cfg.HeroicFolder != "" {
		opts.HeroicRoots = []string{cfg.HeroicFolder}
	}
	if cfg.LutrisFolder != "" {
		opts.LutrisRoots = []string{cfg.LutrisFolder}
	}
	if cfg.BottlesFolder != "" {
		opts.BottlesRoots = []string{cfg.BottlesFolder}
	}
	if cfg.PrismFolder != "" {
		opts.PrismRoots = []string{cfg.PrismFolder}
	}
	return opts
}

// runDiscovery scans launchers, matches against manifest, returns count of newly discovered games.
func runDiscovery(manifestEntries []types.GameSaveLocation) int {
	prev := loadDiscoveryCache()
	prevSet := make(map[string]bool, len(prev.MatchedGameIDs))
	for _, id := range prev.MatchedGameIDs {
		prevSet[id] = true
	}

	mf := LoadManifestFile()
	idx := discovery.BuildManifestV2Index(mf.Games, manifestEntries)
	idMap := prev.IDMap
	if idMap == nil {
		idMap = make(map[string]string)
	}

	installed := discovery.ScanInstalledGamesOpts(currentScanOptions())
	resolveUnmatchedSteam(installed, idx, idMap)
	matched := discovery.MatchManifestWithV2Index(installed, idx, idMap)

	var matchedIDs []string
	newCount := 0
	for _, g := range matched {
		matchedIDs = append(matchedIDs, g.ManifestGameID)
		if !prevSet[g.ManifestGameID] {
			newCount++
		}
	}

	disabled := make(map[string]bool)
	for _, id := range prev.DisabledGameIDs {
		disabled[id] = true
	}
	matchedSet := make(map[string]bool)
	for _, id := range matchedIDs {
		matchedSet[id] = true
	}
	discoveryMu.Lock()
	discoveryState.MatchedGameIDs = matchedSet
	discoveryState.MatchedGames = matched
	discoveryState.DisabledGameIDs = disabled
	discoveryState.InstalledSteam = installedSteamAppIDs(installed)
	discoveryMu.Unlock()

	cache := discoveryCache{
		InstalledGames:       installed,
		MatchedGameIDs:       matchedIDs,
		MatchedGames:         matched,
		DisabledGameIDs:      prev.DisabledGameIDs,
		IDMap:                idMap,
		FirstRunSummaryShown: prev.FirstRunSummaryShown,
	}
	_ = saveDiscoveryCache(cache)

	if !prev.FirstRunSummaryShown && len(matched) > 0 {
		cache.FirstRunSummaryShown = true
		_ = saveDiscoveryCache(cache)
		if OnFirstRunDiscovery != nil {
			OnFirstRunDiscovery(matched)
		}
	}

	if newCount > 0 && OnDiscoveryResult != nil {
		OnDiscoveryResult(newCount)
	}
	if OnDiscoveryUpdated != nil {
		OnDiscoveryUpdated()
	}
	RecordDiscovery(matched, newCount)
	return newCount
}

func manifestGameIDSet(entries []types.GameSaveLocation) map[string]bool {
	set := make(map[string]bool)
	for _, e := range entries {
		set[e.GameID] = true
	}
	return set
}
