package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

var (
	syncMu            sync.Mutex
	syncCancel        context.CancelFunc
	syncNowCh         chan struct{}
	refreshManifestCh chan struct{}
)

// restartSync cancels the current sync loop and starts a new one with the given config.
func restartSync(cfg *config) {
	syncMu.Lock()
	defer syncMu.Unlock()
	if syncCancel != nil {
		syncCancel()
	}
	syncNowCh = make(chan struct{})
	refreshManifestCh = make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	syncCancel = cancel
	log.Printf("tray: sync started server=%s", cfg.ServerURL)
	go func() {
		if err := runSync(ctx, cfg, syncNowCh, refreshManifestCh); err != nil {
			log.Println("sync:", err)
		}
	}()
}

// triggerSyncNow sends on the syncNow channel if a sync loop is running.
func triggerSyncNow() {
	syncMu.Lock()
	ch := syncNowCh
	syncMu.Unlock()
	if ch != nil {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// triggerManifestRefresh sends on the refresh channel if a sync loop is running.
func triggerManifestRefresh() {
	syncMu.Lock()
	ch := refreshManifestCh
	syncMu.Unlock()
	if ch != nil {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func lastSyncTooltip() string {
	if d := GetNextRetryIn(); d > 0 {
		sec := int(d.Round(time.Second).Seconds())
		return fmt.Sprintf("GSBS — Last sync failed; retrying in %ds", sec)
	}
	at, err := getLastSync()
	if at.IsZero() {
		return "Game Sync & Backup Service"
	}
	ago := time.Since(at)
	var agoStr string
	switch {
	case ago < time.Minute:
		agoStr = "just now"
	case ago < time.Hour:
		agoStr = fmt.Sprintf("%.0fm ago", ago.Minutes())
	case ago < 24*time.Hour:
		agoStr = fmt.Sprintf("%.1fh ago", ago.Hours())
	default:
		agoStr = fmt.Sprintf("%.0fd ago", ago.Hours()/24)
	}
	status := "ok"
	if err != nil {
		status = "failed"
	}
	return fmt.Sprintf("GSBS — Last sync: %s (%s)", agoStr, status)
}
