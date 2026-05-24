package discovery

import (
	"testing"
)

func TestMatchManifest(t *testing.T) {
	installed := []InstalledGame{
		{GameID: "12345", Launcher: "steam", Title: "Test Game"},
		{GameID: "unknown", Launcher: "gog", Title: "Other"},
	}
	manifest := map[string]bool{"12345": true}
	matched := MatchManifest(installed, manifest)
	if len(matched) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matched))
	}
	if matched[0].GameID != "12345" {
		t.Fatalf("expected 12345, got %s", matched[0].GameID)
	}
}

func TestScanInstalledGamesEmpty(t *testing.T) {
	// Should not panic on systems with no launchers
	games := ScanInstalledGames()
	_ = games
}
