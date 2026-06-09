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

func TestPersistIngestResult_PreservesExistingTitleWhenMissing(t *testing.T) {
	st, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	const pageID = int64(88888)
	const gameID = "88888"
	const existingTitle = "Already Known Title"

	if err := st.UpsertPCGWGame(ctx, &types.PCGWGame{
		PageID: pageID, PageName: existingTitle, Title: existingTitle, ParseStatus: "ok",
	}); err != nil {
		t.Fatal(err)
	}

	saveLocs := []pcgw.SaveLocationTemplate{
		{GameID: gameID, System: "Windows", Paths: []string{`%APPDATA%\GameY\Save`}, IsConfig: false},
	}
	result := &pcgw.IngestResult{
		Bundle: pcgw.GameBundle{
			PageID:      pageID,
			ParseStatus: "ok",
			PageInfo:    pcgw.PageInfo{PageID: pageID}, // title intentionally missing
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

	if _, err := PersistIngestResult(ctx, st, "", result, PCGWFilters{}); err != nil {
		t.Fatalf("PersistIngestResult: %v", err)
	}

	game, err := st.GetPCGWGame(ctx, pageID)
	if err != nil {
		t.Fatalf("GetPCGWGame: %v", err)
	}
	if game.Title != existingTitle {
		t.Fatalf("expected preserved title %q, got %q", existingTitle, game.Title)
	}

	entries, err := st.ListGameSaveLocations(ctx)
	if err != nil {
		t.Fatalf("ListGameSaveLocations: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].GameTitle != existingTitle {
		t.Fatalf("expected projected title %q, got %q", existingTitle, entries[0].GameTitle)
	}
}

func TestPersistIngestResult_UsesSeededPageInfoTitle(t *testing.T) {
	st, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	const pageID = int64(99991)
	const expectedTitle = "Catalog Seeded Title"

	saveLocs := []pcgw.SaveLocationTemplate{
		{GameID: "99991", System: "Windows", Paths: []string{`%APPDATA%\GameZ\Save`}, IsConfig: false},
	}
	result := &pcgw.IngestResult{
		Bundle: pcgw.GameBundle{
			PageID:      pageID,
			ParseStatus: "ok",
			PageInfo: pcgw.PageInfo{
				PageID: pageID,
				Title:  expectedTitle, // seeded from catalog title hint
			},
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

	if _, err := PersistIngestResult(ctx, st, "", result, PCGWFilters{}); err != nil {
		t.Fatalf("PersistIngestResult: %v", err)
	}

	game, err := st.GetPCGWGame(ctx, pageID)
	if err != nil {
		t.Fatalf("GetPCGWGame: %v", err)
	}
	if game.Title != expectedTitle {
		t.Fatalf("expected title %q, got %q", expectedTitle, game.Title)
	}

	entries, err := st.ListGameSaveLocations(ctx)
	if err != nil {
		t.Fatalf("ListGameSaveLocations: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].GameTitle != expectedTitle {
		t.Fatalf("expected projected title %q, got %q", expectedTitle, entries[0].GameTitle)
	}
}

// TestPersistIngestResult_FailedFetchWritesStubRow verifies that a partial IngestResult
// returned when ParsePageWikitext fails (ParseStatus="failed", no sections, no wikitext)
// is still written to pcgw_games. This is the precondition for the syncOnePage fix that
// prevents fetch-failed pages from staying in the "missing" queue forever.
func TestPersistIngestResult_FailedFetchWritesStubRow(t *testing.T) {
	st, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	const pageID = int64(55555)

	// Seed the catalog so the page shows up in ListPCGWCatalogMissing initially.
	_ = st.UpsertPCGWCatalogBatch(ctx, []types.PCGWCatalogEntry{
		{PageID: pageID, Title: "Test Game", FirstSeenAt: "2025-01-01T00:00:00Z", LastSeenAt: "2025-01-01T00:00:00Z"},
	})

	missing, _ := st.ListPCGWCatalogMissing(ctx, 0, 0)
	if len(missing) != 1 {
		t.Fatalf("expected page in missing queue before stub write, got %d", len(missing))
	}

	// Simulate the partial result returned by IngestPage when ParsePageWikitext errors.
	failedResult := &pcgw.IngestResult{
		Bundle: pcgw.GameBundle{
			PageID:      pageID,
			ParseStatus: "failed",
			PageInfo:    pcgw.PageInfo{PageID: pageID, Title: "Test Game"},
			Sections:    make(map[string]pcgw.SectionResult),
		},
		Errors: []string{"http: connection refused"},
	}

	n, persistErr := PersistIngestResult(ctx, st, "", failedResult, PCGWFilters{})
	if persistErr != nil {
		t.Fatalf("PersistIngestResult with failed result: %v", persistErr)
	}
	if n != 0 {
		t.Errorf("expected 0 entries (no save locations), got %d", n)
	}

	// The stub row must exist in pcgw_games with parse_status="failed".
	game, err := st.GetPCGWGame(ctx, pageID)
	if err != nil {
		t.Fatalf("GetPCGWGame after stub write: %v", err)
	}
	if game.ParseStatus != "failed" {
		t.Errorf("stub row parse_status: got %q, want %q", game.ParseStatus, "failed")
	}

	// The page must no longer appear in the "missing" queue.
	missing2, _ := st.ListPCGWCatalogMissing(ctx, 0, 0)
	if len(missing2) != 0 {
		t.Errorf("page should not appear in missing queue after stub write, got %d", len(missing2))
	}

	// The page must appear in the "failed/partial" queue so it can be retried.
	failed, _ := st.ListPCGWCatalogFailedPartial(ctx, 0, 0)
	if len(failed) != 1 || failed[0] != pageID {
		t.Errorf("page should appear in failed queue after stub write, got %v", failed)
	}
}

func TestPhase2StartCursor_IgnoresStaleResumeCursor(t *testing.T) {
	tests := []struct {
		name              string
		resumeCursor      int
		queueSize         int
		resumeCatalogScan bool
		want              int
	}{
		{
			name:              "fresh run",
			resumeCursor:      0,
			queueSize:         1200,
			resumeCatalogScan: false,
			want:              0,
		},
		{
			name:              "resume run ignores old cursor",
			resumeCursor:      500,
			queueSize:         54000,
			resumeCatalogScan: true,
			want:              0,
		},
		{
			name:              "resume run with empty queue",
			resumeCursor:      500,
			queueSize:         0,
			resumeCatalogScan: true,
			want:              0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := phase2StartCursor(tt.resumeCursor, tt.queueSize, tt.resumeCatalogScan)
			if got != tt.want {
				t.Fatalf("phase2StartCursor(%d, %d, %v) = %d, want %d",
					tt.resumeCursor, tt.queueSize, tt.resumeCatalogScan, got, tt.want)
			}
		})
	}
}
