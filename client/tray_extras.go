package main

import (
	"fmt"
	"net/url"

	clientsync "github.com/gsbs/gsbs/client/sync"
	"github.com/skratchdot/open-golang/open"
)

// openDashboard opens the GSBS server dashboard in the default browser.
func openDashboard(cfg *config) {
	if cfg == nil || cfg.ServerURL == "" {
		return
	}
	_ = open.Run(cfg.ServerURL + "/dashboard")
}

// openSaveVersions opens the WebUI version history for a save slot.
func openSaveVersions(cfg *config, gameID, pathKey string) {
	if cfg == nil || cfg.ServerURL == "" {
		return
	}
	u := fmt.Sprintf("%s/dashboard/save/versions?game_id=%s&path_key=%s",
		cfg.ServerURL, url.QueryEscape(gameID), url.QueryEscape(pathKey))
	_ = open.Run(u)
}

// resolveAllConflictsKeepLocal pushes local files for all pending conflicts.
func resolveAllConflictsKeepLocal() {
	for _, c := range clientsync.ListConflicts() {
		resolveConflictAction(c.GameID, c.PathKey, c.FilePath, clientsync.ResolveKeepLocal)
	}
}

// resolveAllConflictsUseServer pulls server version for all pending conflicts.
func resolveAllConflictsUseServer() {
	for _, c := range clientsync.ListConflicts() {
		resolveConflictAction(c.GameID, c.PathKey, c.FilePath, clientsync.ResolveUseServer)
	}
}

// mergeDetectedIntoConfig merges DetectLauncherPaths into cfg (empty fields only).
func mergeDetectedIntoConfig(cfg *config, detected DetectedLauncherPaths) bool {
	if cfg == nil {
		return false
	}
	merged := false
	if detected.UbisoftConnect != "" && cfg.UbisoftConnectFolder == "" {
		cfg.UbisoftConnectFolder = detected.UbisoftConnect
		merged = true
	}
	if detected.GOGGalaxy != "" && cfg.GOGGalaxyFolder == "" {
		cfg.GOGGalaxyFolder = detected.GOGGalaxy
		merged = true
	}
	if detected.EpicGames != "" && cfg.EpicGamesFolder == "" {
		cfg.EpicGamesFolder = detected.EpicGames
		merged = true
	}
	if detected.XboxApp != "" && cfg.XboxAppFolder == "" {
		cfg.XboxAppFolder = detected.XboxApp
		merged = true
	}
	if detected.HeroicFolder != "" && cfg.HeroicFolder == "" {
		cfg.HeroicFolder = detected.HeroicFolder
		merged = true
	}
	if detected.LutrisFolder != "" && cfg.LutrisFolder == "" {
		cfg.LutrisFolder = detected.LutrisFolder
		merged = true
	}
	if detected.EAAppFolder != "" && cfg.EAAppFolder == "" {
		cfg.EAAppFolder = detected.EAAppFolder
		merged = true
	}
	return merged
}
