package main

import (
	"path/filepath"
	"testing"

	"github.com/gsbs/gsbs/pkg/paths"
	"github.com/gsbs/gsbs/pkg/types"
	"github.com/stretchr/testify/assert"
)

func otherPlatform() string {
	if string(paths.CurrentOS()) == "windows" {
		return "linux"
	}
	return "windows"
}

func TestDiagnoseGameSync_Reasons(t *testing.T) {
	existing := t.TempDir()
	missing := filepath.Join(existing, "does-not-exist")
	currentOS := paths.CurrentOS()
	resolver := paths.NewResolver()
	roots := map[string][]string{} // non-nil avoids reading manifest from disk

	entries := []types.GameSaveLocation{
		{GameID: "ready", Platform: string(currentOS), GameTitle: "Ready Game",
			SaveRules: []types.SaveRule{{Directory: existing, SyncAll: true, Platform: string(currentOS)}}},
		{GameID: "missing", Platform: string(currentOS), GameTitle: "Missing Game",
			SaveRules: []types.SaveRule{{Directory: missing, SyncAll: true, Platform: string(currentOS)}}},
		{GameID: "wrongos", Platform: otherPlatform(), GameTitle: "Other OS Game",
			SaveRules: []types.SaveRule{{Directory: existing, SyncAll: true, Platform: otherPlatform()}}},
		{GameID: "malformed", Platform: string(currentOS), GameTitle: "No Rules"},
	}

	cases := []struct {
		gameID string
		want   SyncReason
	}{
		{"ready", SyncReasonReady},
		{"missing", SyncReasonSaveDirMissing},
		{"wrongos", SyncReasonWrongPlatform},
		{"malformed", SyncReasonMalformedRules},
		{"unknown", SyncReasonNoManifest},
	}
	for _, c := range cases {
		got := DiagnoseGameSync(c.gameID, entries, resolver, currentOS, true, roots)
		assert.Equal(t, c.want, got.Reason, "game_id=%s", c.gameID)
	}
}

func TestDiagnoseGameSync_ReadyReportsExistingDir(t *testing.T) {
	existing := t.TempDir()
	currentOS := paths.CurrentOS()
	entries := []types.GameSaveLocation{
		{GameID: "g", Platform: string(currentOS),
			SaveRules: []types.SaveRule{{Directory: existing, SyncAll: true, Platform: string(currentOS)}}},
	}
	got := DiagnoseGameSync("g", entries, paths.NewResolver(), currentOS, true, map[string][]string{})
	assert.Equal(t, SyncReasonReady, got.Reason)
	assert.Contains(t, got.ExistingDirs, existing)
}

func TestSyncReasonFriendly(t *testing.T) {
	assert.Equal(t, "ready to sync", SyncReasonReady.Friendly())
	assert.Equal(t, "not in server manifest", SyncReasonNoManifest.Friendly())
	assert.Equal(t, "save folder not found", SyncReasonSaveDirMissing.Friendly())
	assert.NotEmpty(t, SyncReason("custom").Friendly())
}

func TestAddManualWatchPath_Validation(t *testing.T) {
	// These cases return before touching config or disk state.
	assert.Error(t, addManualWatchPath("", "", "", true, nil))
	assert.Error(t, addManualWatchPath("game", "", "", true, nil))
	missing := filepath.Join(t.TempDir(), "nope")
	assert.Error(t, addManualWatchPath("game", "Game", missing, true, nil))
}

func TestTrimPatterns(t *testing.T) {
	got := trimPatterns([]string{" *.sav ", "", "  ", "*.bak"})
	assert.Equal(t, []string{"*.sav", "*.bak"}, got)
}
