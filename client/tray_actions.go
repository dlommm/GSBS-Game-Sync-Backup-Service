package main

import (
	"context"
	"fmt"
	"log"
	gosync "sync"
	"time"

	"github.com/gen2brain/beeep"
	clientsync "github.com/gsbs/gsbs/client/sync"
	"github.com/gsbs/gsbs/pkg/discovery"
)

var (
	syncClientMu gosync.Mutex
	syncClient   *clientsync.Client
)

// SetSyncClient stores the active sync client for tray conflict/version actions.
func SetSyncClient(c *clientsync.Client) {
	syncClientMu.Lock()
	syncClient = c
	syncClientMu.Unlock()
}

func getSyncClient() *clientsync.Client {
	syncClientMu.Lock()
	defer syncClientMu.Unlock()
	return syncClient
}

func setupTrayCallbacks() {
	clientsync.OnConflictDetected = notifyConflict
	OnFirstRunDiscovery = func(games []discovery.MatchedGame) {
		notifyFirstRunDiscoveryGames(games)
	}
}

func notifyConflict(gameID, pathKey, filePath string) {
	RecordGameConflict(gameID, pathKey)
	notifyConflictToast(gameID, pathKey)
	log.Printf("tray: conflict detected game=%s path_key=%s file=%s", gameID, pathKey, filePath)
}

func notifyFirstRunDiscoveryGames(games []discovery.MatchedGame) {
	if len(games) == 0 {
		return
	}
	notifyFirstRunToast(games)
}

func resolveConflictAction(gameID, pathKey, filePath string, choice clientsync.ResolveChoice) {
	c := getSyncClient()
	if c == nil {
		log.Printf("tray: no sync client for conflict resolve")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := clientsync.ResolveConflict(ctx, c, gameID, pathKey, choice, filePath); err != nil {
		log.Printf("tray: resolve conflict: %v", err)
		_ = beeep.Notify("GSBS", "Failed to resolve conflict", "")
		return
	}
	ClearGameConflict(gameID)
	refreshTrayCounts()
	triggerSyncNow()
}

func restoreSaveVersion(gameID, pathKey string, version int) {
	c := getSyncClient()
	if c == nil {
		log.Printf("tray: no sync client for version restore")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := c.RestoreVersion(ctx, gameID, pathKey, version); err != nil {
		log.Printf("tray: restore version: %v", err)
		_ = beeep.Notify("GSBS", "Restore failed", "")
		return
	}
	_ = beeep.Notify("GSBS", fmt.Sprintf("Restored version %d on server", version), "")
	triggerSyncNow()
}
