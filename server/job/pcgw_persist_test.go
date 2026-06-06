package job

import (
	"context"
	"testing"

	"github.com/gsbs/gsbs/pkg/pcgw"
	"github.com/gsbs/gsbs/pkg/saverule"
	"github.com/gsbs/gsbs/pkg/types"
	"github.com/gsbs/gsbs/server/store"
)

// TestPersistIngestResult_SlotLabelAlignment verifies that Windows and Linux
// SaveRules produced from the same logical save location share the same
// SlotLabel, making saverule.RuleKey identical across OSes for each slot.
func TestPersistIngestResult_SlotLabelAlignment(t *testing.T) {
	st, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	const pageID = int64(77777)
	const gameID = "77777"

	// Two logical save slots, each with a Windows and a Linux template.
	// Slot 0: Windows = %APPDATA%\GameX\Save, Linux = ~/.config/GameX
	// Slot 1: Windows = %APPDATA%\GameX\Backup, Linux = ~/.local/share/GameX
	saveLocs := []pcgw.SaveLocationTemplate{
		{GameID: gameID, System: "Windows", Paths: []string{`%APPDATA%\GameX\Save`}, IsConfig: false},
		{GameID: gameID, System: "Linux", Paths: []string{`~/.config/GameX`}, IsConfig: false},
		{GameID: gameID, System: "Windows", Paths: []string{`%APPDATA%\GameX\Backup`}, IsConfig: false},
		{GameID: gameID, System: "Linux", Paths: []string{`~/.local/share/GameX`}, IsConfig: false},
	}
	result := &pcgw.IngestResult{
		Bundle: pcgw.GameBundle{
			PageID:      pageID,
			ParseStatus: "ok",
			PageInfo: pcgw.PageInfo{
				PageID: pageID,
				Title:  "GameX",
			},
			// Sections must include "game_data" so PersistIngestResult sets
			// gameDataOK=true and actually writes to game_save_locations.
			Sections: map[string]pcgw.SectionResult{
				"game_data": {
					Key: "game_data",
					Data: map[string]interface{}{
						"templates": saveLocs,
					},
				},
			},
			SaveLocations: saveLocs,
		},
	}

	// Seed the pcgw_games row so ReplaceGameSaveLocationsForGame can run.
	if err := st.UpsertPCGWGame(ctx, &types.PCGWGame{
		PageID: pageID, PageName: "GameX", Title: "GameX", ParseStatus: "ok",
	}); err != nil {
		t.Fatal(err)
	}

	filters := PCGWFilters{} // no exclusions
	_, persistErr := PersistIngestResult(ctx, st, "", result, filters)
	if persistErr != nil {
		t.Fatalf("PersistIngestResult: %v", persistErr)
	}

	entries, err := st.ListGameSaveLocations(ctx)
	if err != nil {
		t.Fatalf("ListGameSaveLocations: %v", err)
	}
	if len(entries) != 4 {
		t.Fatalf("expected 4 entries (2 slots × 2 OSes), got %d", len(entries))
	}

	// Index entries by (platform, slotLabel).
	type key struct{ platform, slotLabel string }
	byKey := map[key]types.SaveRule{}
	for _, e := range entries {
		if len(e.SaveRules) != 1 {
			t.Fatalf("expected exactly 1 SaveRule per entry, got %d for %s/%s", len(e.SaveRules), e.Platform, e.PathTemplate)
		}
		r := e.SaveRules[0]
		if r.SlotLabel == "" {
			t.Fatalf("SlotLabel is empty for platform=%s path=%s", e.Platform, e.PathTemplate)
		}
		k := key{e.Platform, r.SlotLabel}
		byKey[k] = r
	}

	// For each of the 2 slots, the Windows and Linux rules must share the same
	// SlotLabel and therefore produce the same RuleKey.
	for slot := 0; slot <= 1; slot++ {
		slotStr := string(rune('0' + slot))
		winRule, ok := byKey[key{"windows", slotStr}]
		if !ok {
			t.Fatalf("no windows rule for slot %s", slotStr)
		}
		linRule, ok := byKey[key{"linux", slotStr}]
		if !ok {
			t.Fatalf("no linux rule for slot %s", slotStr)
		}
		if winRule.SlotLabel != linRule.SlotLabel {
			t.Errorf("slot %s: SlotLabel mismatch windows=%q linux=%q", slotStr, winRule.SlotLabel, linRule.SlotLabel)
		}
		winKey := saverule.RuleKey(gameID, winRule)
		linKey := saverule.RuleKey(gameID, linRule)
		if winKey != linKey {
			t.Errorf("slot %s: RuleKey mismatch windows=%q linux=%q", slotStr, winKey, linKey)
		}
	}

	// Slot 0 and Slot 1 must produce DIFFERENT RuleKeys (different logical saves).
	win0Key := saverule.RuleKey(gameID, byKey[key{"windows", "0"}])
	win1Key := saverule.RuleKey(gameID, byKey[key{"windows", "1"}])
	if win0Key == win1Key {
		t.Errorf("slot 0 and slot 1 produced identical RuleKey %q", win0Key)
	}
}
