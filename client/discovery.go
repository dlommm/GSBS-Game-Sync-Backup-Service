package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/gsbs/gsbs/pkg/discovery"
	"github.com/gsbs/gsbs/pkg/types"
)

// discoveryCache is persisted scan results.
type discoveryCache struct {
	LastScanAt      string                    `json:"last_scan_at"`
	InstalledGames  []discovery.InstalledGame `json:"installed_games"`
	MatchedGameIDs  []string                  `json:"matched_game_ids"`
}

func discoveryPath() string {
	dir, _ := os.UserConfigDir()
	return filepath.Join(dir, "gsbs", "discovery.json")
}

func loadDiscoveryCache() discoveryCache {
	data, err := os.ReadFile(discoveryPath())
	if err != nil {
		return discoveryCache{}
	}
	var c discoveryCache
	if json.Unmarshal(data, &c) != nil {
		return discoveryCache{}
	}
	return c
}

func saveDiscoveryCache(c discoveryCache) error {
	dir := filepath.Dir(discoveryPath())
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	c.LastScanAt = time.Now().UTC().Format(time.RFC3339)
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

// runDiscovery scans launchers, matches against manifest, returns count of newly discovered games.
func runDiscovery(manifestGameIDs map[string]bool) int {
	prev := loadDiscoveryCache()
	prevSet := make(map[string]bool, len(prev.MatchedGameIDs))
	for _, id := range prev.MatchedGameIDs {
		prevSet[id] = true
	}

	installed := discovery.ScanInstalledGames()
	matched := discovery.MatchManifest(installed, manifestGameIDs)

	var matchedIDs []string
	newCount := 0
	for _, g := range matched {
		matchedIDs = append(matchedIDs, g.GameID)
		if !prevSet[g.GameID] {
			newCount++
		}
	}

	_ = saveDiscoveryCache(discoveryCache{
		InstalledGames: installed,
		MatchedGameIDs: matchedIDs,
	})

	if newCount > 0 && OnDiscoveryResult != nil {
		OnDiscoveryResult(newCount)
	}
	return newCount
}

func manifestGameIDSet(entries []types.GameSaveLocation) map[string]bool {
	set := make(map[string]bool)
	for _, e := range entries {
		set[e.GameID] = true
	}
	return set
}
