package main

import (
	gosync "sync"
	"time"
)

// Per-game snooze: temporarily defer sync for one game without permanently
// disabling it (the existing per-game toggle is a hard disable). In-memory
// only — a restart clears snoozes by design.
var gameSnooze struct {
	mu    gosync.Mutex
	until map[string]time.Time
}

// SnoozeGame defers sync for the game until the given time. A zero time
// clears the snooze.
func SnoozeGame(gameID string, until time.Time) {
	if gameID == "" {
		return
	}
	gameSnooze.mu.Lock()
	defer gameSnooze.mu.Unlock()
	if gameSnooze.until == nil {
		gameSnooze.until = make(map[string]time.Time)
	}
	if until.IsZero() {
		delete(gameSnooze.until, gameID)
		return
	}
	gameSnooze.until[gameID] = until
}

// GameSnoozedUntil returns the active snooze deadline (zero when none).
func GameSnoozedUntil(gameID string) time.Time {
	gameSnooze.mu.Lock()
	defer gameSnooze.mu.Unlock()
	until, ok := gameSnooze.until[gameID]
	if !ok {
		return time.Time{}
	}
	if time.Now().After(until) {
		delete(gameSnooze.until, gameID)
		return time.Time{}
	}
	return until
}

func gameSnoozed(gameID string) bool {
	return !GameSnoozedUntil(gameID).IsZero()
}

// expiredSnoozes removes and returns games whose snooze deadline passed, so
// the sync loop can flush their deferred pushes.
func expiredSnoozes() []string {
	gameSnooze.mu.Lock()
	defer gameSnooze.mu.Unlock()
	var out []string
	now := time.Now()
	for id, until := range gameSnooze.until {
		if now.After(until) {
			delete(gameSnooze.until, id)
			out = append(out, id)
		}
	}
	return out
}

// FlushGame fires any deferred pushes for a game and triggers a pull. Set by
// runSync (where the watcher exists); nil until the sync loop starts.
var flushGameHook struct {
	mu gosync.Mutex
	fn func(gameID string)
}

func setFlushGameHook(fn func(gameID string)) {
	flushGameHook.mu.Lock()
	flushGameHook.fn = fn
	flushGameHook.mu.Unlock()
}

// FlushGame syncs one game now (tray "Sync now" per game / Insights button).
// Falls back to a plain global sync trigger before the hook is installed.
// Note: there is no targeted-pull API; the pull half is a normal summaries
// pull, which is cheap.
func FlushGame(gameID string) {
	flushGameHook.mu.Lock()
	fn := flushGameHook.fn
	flushGameHook.mu.Unlock()
	SnoozeGame(gameID, time.Time{}) // an explicit sync cancels a snooze
	if fn != nil {
		fn(gameID)
	}
	triggerSyncNow()
}

// Resume catch-up: pauses DROP watcher pushes, so resuming must rescan the
// watched dirs (hash cache dedups) before pulling. Set by runSync.
var resumeCatchUpHook struct {
	mu gosync.Mutex
	fn func()
}

func setResumeCatchUpHook(fn func()) {
	resumeCatchUpHook.mu.Lock()
	resumeCatchUpHook.fn = fn
	resumeCatchUpHook.mu.Unlock()
}

// RunResumeCatchUp fires the rescan+pull catch-up after a pause/snooze ends.
func RunResumeCatchUp() {
	resumeCatchUpHook.mu.Lock()
	fn := resumeCatchUpHook.fn
	resumeCatchUpHook.mu.Unlock()
	if fn != nil {
		fn()
		return
	}
	triggerSyncNow()
}
