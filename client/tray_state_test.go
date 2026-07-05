package main

import (
	"testing"
	"time"
)

// Regression: RecordSaveEvent for a game with no cached row used to call
// gameTitleFor (which takes globalTrayState.mu) while already holding the
// write lock. sync.RWMutex is not reentrant, so that goroutine deadlocked
// holding the lock and every later locker — the tray menu and the local
// WebUI /status endpoint (GetTraySnapshot) — hung forever.
func TestRecordSaveEventNewGameDoesNotDeadlock(t *testing.T) {
	done := make(chan struct{})
	go func() {
		RecordSaveEvent("tray-deadlock-test-game", "pk1", SaveDirPush, nil)
		RecordPendingUpload("tray-deadlock-test-game-2", "pk2")
		RecordGameConflict("tray-deadlock-test-game-3", "pk3")
		_ = GetTraySnapshot()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("tray state deadlocked: gameTitleFor must not be called while holding globalTrayState.mu")
	}
}
