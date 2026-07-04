package main

import (
	"context"
	"log"
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
	p.OnGameStart = func(gameID string) {
		log.Printf("game-aware sync: %s started — deferring its sync until exit", gameID)
		SetGamesRunning(p.RunningCount())
	}
	p.OnGameStop = func(gameID string) {
		log.Printf("game-aware sync: %s exited — flushing pending saves", gameID)
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
