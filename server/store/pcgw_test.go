package store

import (
	"context"
	"testing"

	"github.com/gsbs/gsbs/pkg/types"
)

func TestPCGWGameCRUD(t *testing.T) {
	st, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	g := &types.PCGWGame{
		PageID: 12345, PageName: "Test_Game", Title: "Test Game",
		SteamAppIDs: []string{"999"}, ParseStatus: "ok",
		Taxonomy: map[string]interface{}{"genres": []string{"Action"}},
	}
	if err := st.UpsertPCGWGame(ctx, g); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetPCGWGame(ctx, 12345)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Test Game" || len(got.SteamAppIDs) != 1 {
		t.Fatalf("got %+v", got)
	}

	gd := &types.PCGWGameData{
		PageID: 12345, PlatformKey: "windows", PlatformRawLabel: "Windows",
		SaveLocations: []types.PCGWPathEntry{{PathTemplates: []string{"%APPDATA%/Test"}}},
	}
	if err := st.UpsertPCGWGameData(ctx, gd); err != nil {
		t.Fatal(err)
	}
	rows, err := st.ListPCGWGameData(ctx, 12345)
	if err != nil || len(rows) != 1 {
		t.Fatalf("game data: %v %d", err, len(rows))
	}

	sec := &types.PCGWSectionRow{PageID: 12345, Data: map[string]interface{}{"steam": true}, SectionWikitext: "raw"}
	if err := st.UpsertPCGWAvailability(ctx, sec); err != nil {
		t.Fatal(err)
	}

	meta := &types.PCGWMetadata{
		PageID: 12345, ContentHash: "abc", UncompressedSize: 100,
		SectionHashes: map[string]string{"game_data": "def"},
	}
	if err := st.UpsertPCGWMetadata(ctx, meta); err != nil {
		t.Fatal(err)
	}
	hash, sh, err := st.GetPCGWContentHash(ctx, 12345)
	if err != nil || hash != "abc" || sh["game_data"] != "def" {
		t.Fatalf("hash: %q %v %v", hash, sh, err)
	}

	entries := []types.GameSaveLocation{{
		GameID: "12345", PCGWPageID: 12345, GameTitle: "Test Game",
		Platform: "windows", PathTemplate: "%APPDATA%/Test", Source: "pcgw",
	}}
	if err := st.ReplaceGameSaveLocationsForGame(ctx, "12345", entries); err != nil {
		t.Fatal(err)
	}
	list, err := st.ListGameSaveLocations(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("manifest projection: %d", len(list))
	}

	etag, _ := st.BumpManifestVersion(ctx, "sha256:test")
	v2, err := st.BuildManifestV2(ctx, "", "", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if v2.Version != etag || len(v2.Games) != 1 || v2.GamesTotal != 1 {
		t.Fatalf("v2: version=%d games=%d total=%d", v2.Version, len(v2.Games), v2.GamesTotal)
	}
	if !v2.Games[0].HasSaveData {
		t.Fatal("expected has_save_data")
	}

	stats, err := st.GetPCGWStats(ctx)
	if err != nil || stats.TotalGames != 1 {
		t.Fatalf("stats: %+v", stats)
	}

	runID, err := st.StartPCGWSyncRun(ctx, "incremental")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.FinishPCGWSyncRun(ctx, runID, "success", "", PCGWSyncRunStats{
		GamesTotal: 1, GamesOK: 1, AvgParseMs: 42,
	}); err != nil {
		t.Fatal(err)
	}
	runs, err := st.ListPCGWSyncRuns(ctx, 10)
	if err != nil || len(runs) != 1 {
		t.Fatalf("ListPCGWSyncRuns: %v len=%d", err, len(runs))
	}
	if runs[0].Mode != "incremental" || runs[0].Status != "success" || runs[0].GamesOK != 1 {
		t.Fatalf("run: %+v", runs[0])
	}
	if n, err := st.CountPCGWParseFailures(ctx); err != nil || n != 0 {
		t.Fatalf("parse failures count: %d %v", n, err)
	}
}

func TestPCGWBackfillFromManifest(t *testing.T) {
	st, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	// Backfill runs at migrate time only; simulate legacy row via direct upsert
	if err := st.UpsertPCGWGame(ctx, &types.PCGWGame{
		PageID: 42, PageName: "Legacy", Title: "Legacy", ParseStatus: "pending",
	}); err != nil {
		t.Fatal(err)
	}
	g, err := st.GetPCGWGame(ctx, 42)
	if err != nil {
		t.Fatal(err)
	}
	if g.Title != "Legacy" || g.ParseStatus != "pending" {
		t.Fatalf("backfill: %+v", g)
	}
}

func TestBuildManifestV2_UsesGameSaveLocationsNotPCGWGamesCount(t *testing.T) {
	st, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	// pcgw_games mirror alone would only expose this one title.
	if err := st.UpsertPCGWGame(ctx, &types.PCGWGame{
		PageID: 100, PageName: "Mirrored", Title: "Mirrored Game",
		PlatformsPresent: []string{"windows"}, ParseStatus: "ok",
	}); err != nil {
		t.Fatal(err)
	}

	// game_save_locations holds the full manifest projection (bundle import path).
	if err := st.UpsertGameSaveLocations(ctx, []types.GameSaveLocation{
		{GameID: "100", GameTitle: "Mirrored Game", Platform: "windows", PathTemplate: "C:\\a", Source: "pcgw"},
		{GameID: "200", GameTitle: "Bundle Only", Platform: "windows", PathTemplate: "C:\\b", Source: "pcgw"},
		{GameID: "201", GameTitle: "Another Bundle", Platform: "windows", PathTemplate: "C:\\c", Source: "pcgw"},
	}); err != nil {
		t.Fatal(err)
	}

	v2, err := st.BuildManifestV2(ctx, "", "windows", 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if v2.GamesTotal != 3 || len(v2.Games) != 3 {
		t.Fatalf("games_total=%d len(games)=%d, want 3 (from game_save_locations)", v2.GamesTotal, len(v2.Games))
	}
}
