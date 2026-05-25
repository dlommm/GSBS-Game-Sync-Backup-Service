package main

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/gen2brain/beeep"
	"github.com/gsbs/gsbs/pkg/discovery"
)

var (
	notifyMu          sync.Mutex
	pushDebounce      = make(map[string]time.Time)
	syncCompleteStats SyncEndStats
	lastSyncNotifyErr error
	setupToastShown   bool
)

func initTrayNotify() {
	// Hook global sync result for richer toasts (set by platform after controller init).
}

func notifySyncComplete(success bool, errMsg string) {
	notifyMu.Lock()
	stats := syncCompleteStats
	err := lastSyncNotifyErr
	notifyMu.Unlock()

	if success {
		msg := "Sync complete."
		if stats.GamesSynced > 0 || stats.SavesSynced > 0 {
			msg = fmt.Sprintf("Synced %d game(s), %d save(s)", stats.GamesSynced, stats.SavesSynced)
		}
		_ = beeep.Notify("GSBS", msg, "")
		return
	}
	msg := "Sync failed."
	if errMsg != "" {
		msg = truncateMsg(msg+" "+strings.TrimSpace(errMsg), 120)
	} else if err != nil {
		msg = truncateMsg(msg+" "+err.Error(), 120)
	}
	_ = beeep.Notify("GSBS", msg, "")
}

func notifyPushDebounced(gameID string) {
	title := gameTitleFor(gameID)
	notifyMu.Lock()
	last, ok := pushDebounce[gameID]
	if ok && time.Since(last) < 30*time.Second {
		notifyMu.Unlock()
		return
	}
	pushDebounce[gameID] = time.Now()
	notifyMu.Unlock()
	_ = beeep.Notify("GSBS", fmt.Sprintf("%s save uploaded", title), "")
}

func notifyConflictToast(gameID, pathKey string) {
	title := gameTitleFor(gameID)
	_ = beeep.Notify("GSBS", fmt.Sprintf("Conflict: %s — open tray to resolve", title), "")
	log.Printf("tray notify: conflict game=%s path_key=%s", gameID, pathKey)
}

func notifyFirstRunToast(games []discovery.MatchedGame) {
	n := len(games)
	if n == 0 {
		return
	}
	var names []string
	for i, g := range games {
		if i >= 3 {
			break
		}
		name := g.Title
		if name == "" {
			name = gameTitleFor(g.ManifestGameID)
		}
		names = append(names, name)
	}
	msg := fmt.Sprintf("Syncing saves for %d game(s): %s", n, strings.Join(names, ", "))
	if n > 3 {
		msg += fmt.Sprintf(" and %d more", n-3)
	}
	_ = beeep.Notify("GSBS", msg, "")
}

func notifyDiscoveryNew(count int) {
	if count <= 0 {
		return
	}
	_ = beeep.Notify("GSBS", fmt.Sprintf("Discovered %d new game(s)", count), "")
}

func notifySetupRequired() {
	notifyMu.Lock()
	if setupToastShown {
		notifyMu.Unlock()
		return
	}
	setupToastShown = true
	notifyMu.Unlock()
	_ = beeep.Notify("GSBS", "Complete setup in your browser to start syncing", "")
}

func notifyConfigWarnings(warnings []string) {
	if len(warnings) == 0 {
		return
	}
	msg := "Config issues: " + strings.Join(warnings, "; ")
	_ = beeep.Alert("GSBS", truncateMsg(msg, 120), "")
}

func notifyAuthError(msg string) {
	_ = beeep.Alert("GSBS", truncateMsg(msg, 120), "")
}

func notifyQuotaError(msg string) {
	if msg == "" {
		msg = "Storage quota exceeded — free space on the server or contact your admin"
	}
	_ = beeep.Alert("GSBS", truncateMsg("Upload failed: "+msg, 120), "")
	log.Printf("tray notify: quota error: %s", msg)
}

func notifyAlreadyRunning() {
	_ = beeep.Notify("GSBS", "GSBS is already running in the system tray", "")
}

func truncateMsg(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return strings.TrimSpace(s[:max-3]) + "..."
}

func recordSyncStatsForNotify(games, saves int) {
	notifyMu.Lock()
	syncCompleteStats = SyncEndStats{GamesSynced: games, SavesSynced: saves}
	notifyMu.Unlock()
}

func recordSyncErrorForNotify(err error) {
	notifyMu.Lock()
	lastSyncNotifyErr = err
	notifyMu.Unlock()
}
