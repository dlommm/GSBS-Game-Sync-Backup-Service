package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"fyne.io/systray"
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

func pauseResumeMenuTitle(paused bool) string {
	if paused {
		return "Resume syncing"
	}
	return "Pause syncing"
}

func updateSyncIntervalLabel(m *systray.MenuItem, d Duration) {
	interval := d.Duration()
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	m.SetTitle("Sync every " + interval.String())
}

func updateServerLabel(m *systray.MenuItem, url string) {
	if url == "" {
		m.SetTitle("Server: (not set) — click Login to connect")
		return
	}
	label := url
	if len(label) > 40 {
		label = label[:37] + "..."
	}
	m.SetTitle("Server: " + label)
}
