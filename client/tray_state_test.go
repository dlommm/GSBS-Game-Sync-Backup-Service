package main

import (
	"testing"
	"time"
	"unicode/utf8"
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

// Tray labels were cut by byte index, which splits multi-byte UTF-8 sequences
// and rendered non-ASCII game titles as mojibake.
func TestTruncateDisplayIsRuneSafe(t *testing.T) {
	for _, tc := range []struct {
		name, in string
		max      int
		want     string
	}{
		{"short ascii untouched", "Portal 2", 22, "Portal 2"},
		{"exact length untouched", "abcde", 5, "abcde"},
		{"ascii cut", "abcdefghij", 8, "abcde..."},
		{"multibyte cut stays valid", "日本語のゲームタイトルです", 8, "日本語のゲ..."},
		{"accented cut stays valid", "Pokémon Légendes Arceus", 10, "Pokémon..."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := truncateDisplay(tc.in, tc.max, "...")
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
			if !utf8.ValidString(got) {
				t.Errorf("result is not valid UTF-8: %q", got)
			}
			if n := utf8.RuneCountInString(got); n > tc.max {
				t.Errorf("result is %d runes, over the %d limit", n, tc.max)
			}
		})
	}
}

// Every truncated tray label must stay valid UTF-8 at any width.
func TestTruncateDisplayNeverProducesInvalidUTF8(t *testing.T) {
	title := "デス・ストランディング — Director's Cut"
	for max := 1; max <= utf8.RuneCountInString(title)+2; max++ {
		got := truncateDisplay(title, max, "...")
		if !utf8.ValidString(got) {
			t.Fatalf("max=%d produced invalid UTF-8: %q", max, got)
		}
	}
}
