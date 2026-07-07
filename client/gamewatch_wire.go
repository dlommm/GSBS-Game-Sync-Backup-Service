package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	gosync "sync"
	"time"

	"github.com/gsbs/gsbs/client/gamewatch"
	"github.com/gsbs/gsbs/client/sync"
)

// startGameWatch enables game-aware sync: a periodic process scan detects
// running games (executable under a known install root) so pushes and pulls
// for that game are deferred until it exits, then flushed immediately.
// getWatcher is late-bound because the poller starts before the file watcher
// exists. Returns nil when disabled (config), unsupported, or sandboxed
// (Flatpak's PID namespace hides host processes) — sync then behaves exactly
// as it did without this feature.
func startGameWatch(ctx context.Context, cfg *config, getWatcher func() *sync.Watcher, rootsSnapshot func() map[string][]string) *gamewatch.Poller {
	if cfg.GameAwareSync != nil && !*cfg.GameAwareSync {
		log.Println("game-aware sync: disabled by config")
		return nil
	}
	if isFlatpak() {
		log.Println("game-aware sync: unavailable under Flatpak (sandbox hides host processes); sync behaves as before")
		return nil
	}
	interval := time.Duration(cfg.GameScanInterval)
	if interval <= 0 {
		interval = gamewatch.DefaultInterval
	}
	p := &gamewatch.Poller{
		Detector: gamewatch.NewDetector(),
		Interval: interval,
		Roots:    rootsSnapshot,
	}
	// Session capture (v5.2): remember when each game appeared; on exit,
	// report the finished session so the server's version timeline can show
	// "saved after a 2h session on <device>". Best-effort — failures only log.
	var sessionMu gosync.Mutex
	sessionStart := map[string]time.Time{}
	p.OnGameStart = func(gameID string) {
		log.Printf("game-aware sync: %s started — deferring its sync until exit", gameID)
		sessionMu.Lock()
		sessionStart[gameID] = time.Now().UTC()
		sessionMu.Unlock()
		SetGamesRunning(p.RunningCount())
	}
	p.OnGameStop = func(gameID string) {
		log.Printf("game-aware sync: %s exited — flushing pending saves", gameID)
		sessionMu.Lock()
		started, okStart := sessionStart[gameID]
		delete(sessionStart, gameID)
		sessionMu.Unlock()
		if okStart {
			go reportGameSession(ctx, cfg, gameID, started, time.Now().UTC())
		}
		SetGamesRunning(p.RunningCount())
		if w := getWatcher(); w != nil {
			w.FlushPendingFor(gameID)
		}
		triggerSyncNow()
	}
	go p.Run(ctx)
	log.Printf("game-aware sync: enabled (scan interval %s)", interval)
	return p
}

// reportGameSession posts one finished play session to the server (v5.2).
// Sessions shorter than a minute are noise (launchers, crashes) and skipped.
func reportGameSession(ctx context.Context, cfg *config, gameID string, started, ended time.Time) {
	if ended.Sub(started) < time.Minute || cfg == nil || cfg.Token == "" || cfg.ServerURL == "" {
		return
	}
	body, _ := json.Marshal(map[string]string{
		"game_id":    gameID,
		"started_at": started.Format(time.RFC3339),
		"ended_at":   ended.Format(time.RFC3339),
	})
	url := strings.TrimRight(cfg.ServerURL, "/") + "/api/sessions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	req.Header.Set("X-GSBS-Client-Version", Version)
	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		log.Printf("session report: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("session report: server returned %d", resp.StatusCode)
		return
	}
	log.Printf("session report: %s played %s", gameID, ended.Sub(started).Round(time.Minute))
}
