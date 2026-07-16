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

// notifyKind classifies toasts for the user's notification-level setting.
type notifyKind int

const (
	// notifyInfo: routine good news (sync complete, uploads, discovery).
	notifyInfo notifyKind = iota
	// notifyProblem: errors and conflicts the user should know about.
	notifyProblem
	// notifyEssential: direct feedback for a user action or an operational
	// must-see (setup required, already running) — shown even on "silent".
	notifyEssential
)

type notifyPrefsState struct {
	level     string // all, errors, silent
	perUpload bool
}

var notifyPrefs struct {
	mu    sync.RWMutex
	state notifyPrefsState
}

// SetNotifyPrefs installs the notification settings snapshot. Called from
// runSync start and settings save — never loadConfig() per toast (config
// loading touches the OS keyring).
func SetNotifyPrefs(level string, perUpload bool) {
	notifyPrefs.mu.Lock()
	notifyPrefs.state = notifyPrefsState{level: level, perUpload: perUpload}
	notifyPrefs.mu.Unlock()
}

func notifyAllowed(kind notifyKind) bool {
	notifyPrefs.mu.RLock()
	p := notifyPrefs.state
	notifyPrefs.mu.RUnlock()
	switch kind {
	case notifyEssential:
		return true
	case notifyProblem:
		return p.level != "silent"
	default:
		return p.level == "all" || p.level == ""
	}
}

func perUploadToastsEnabled() bool {
	notifyPrefs.mu.RLock()
	defer notifyPrefs.mu.RUnlock()
	// Zero value (never set) keeps the pre-5.4 default: toasts on.
	return notifyPrefs.state.level == "" || notifyPrefs.state.perUpload
}

func notifySyncComplete(success bool, errMsg string) {
	if !notifyAllowed(notifyInfo) {
		return
	}
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
	if !notifyAllowed(notifyInfo) || !perUploadToastsEnabled() {
		return
	}
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
	if !notifyAllowed(notifyProblem) {
		return
	}
	title := gameTitleFor(gameID)
	_ = beeep.Notify("GSBS", fmt.Sprintf("Conflict: %s — open tray to resolve", title), "")
	log.Printf("tray notify: conflict game=%s path_key=%s", gameID, pathKey)
}

func notifyFirstRunToast(games []discovery.MatchedGame) {
	if !notifyAllowed(notifyInfo) {
		return
	}
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
	if !notifyAllowed(notifyInfo) {
		return
	}
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
	if !notifyAllowed(notifyProblem) {
		return
	}
	if len(warnings) == 0 {
		return
	}
	msg := "Config issues: " + strings.Join(warnings, "; ")
	_ = beeep.Alert("GSBS", truncateMsg(msg, 120), "")
}

func notifyAuthError(msg string) {
	if !notifyAllowed(notifyProblem) {
		return
	}
	_ = beeep.Alert("GSBS", truncateMsg(msg, 120), "")
}

// notifyActionError surfaces a failed tray action (open folder/config,
// autostart toggle, …). These failures were log-only, so a menu click that
// did nothing gave the user zero feedback.
func notifyActionError(action string, err error) {
	if err == nil {
		return
	}
	_ = beeep.Alert("GSBS", truncateMsg(action+" failed: "+err.Error(), 120), "")
	log.Printf("tray notify: %s failed: %v", action, err)
}

func notifyPushError(gameID, pathKey, msg string) {
	if !notifyAllowed(notifyProblem) {
		return
	}
	title := gameTitleFor(gameID)
	_ = beeep.Alert("GSBS", truncateMsg(fmt.Sprintf("Upload failed for %s: %s", title, msg), 120), "")
	log.Printf("tray notify: push error game=%s path_key=%s: %s", gameID, pathKey, msg)
}

func notifyQuotaError(msg string) {
	if !notifyAllowed(notifyProblem) {
		return
	}
	if msg == "" {
		msg = "Storage quota exceeded — free space on the server or contact your admin"
	}
	_ = beeep.Alert("GSBS", truncateMsg("Upload failed: "+msg, 120), "")
	log.Printf("tray notify: quota error: %s", msg)
}

func notifyAddGameUnavailable() {
	_ = beeep.Alert("GSBS", "Can't open the Add-game page — the local setup server isn't running. Try restarting GSBS.", "")
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
