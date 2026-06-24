package store

import (
	"context"
	"testing"

	"github.com/gsbs/gsbs/pkg/types"
)

// TestManifestSteamAppIDInfoboxEnrichment verifies that a manifest entry whose
// stored steam_app_ids is empty (as with bundle-imported catalogs) is enriched
// at serve time from the game's PCGW infobox — the fix that lets Linux/Proton
// clients resolve Windows save paths for Steam games (e.g. Ori, page 137485).
func TestManifestSteamAppIDInfoboxEnrichment(t *testing.T) {
	st, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	g := &types.PCGWGame{
		PageID:   137485,
		PageName: "Ori and the Will of the Wisps",
		Title:    "Ori and the Will of the Wisps",
		Infobox:  map[string]interface{}{"steam appid": "1057090", "developers": "Moon Studios"},
	}
	if err := st.UpsertPCGWGame(ctx, g); err != nil {
		t.Fatal(err)
	}
	// Save location with NO steam_app_ids (mimics a bundle-imported row).
	locs := []types.GameSaveLocation{{
		GameID: "137485", PCGWPageID: 137485, GameTitle: "Ori and the Will of the Wisps",
		Platform: "windows", PathTemplate: "%LOCALAPPDATA%/Ori and the Will of The Wisps", Source: "pcgw",
	}}
	if err := st.ReplaceGameSaveLocationsForGame(ctx, "137485", locs); err != nil {
		t.Fatal(err)
	}

	entries, err := st.ListGameSaveLocations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, e := range entries {
		if e.GameID == "137485" {
			found = true
			if len(e.SteamAppIDs) != 1 || e.SteamAppIDs[0] != "1057090" {
				t.Fatalf("expected steam_app_ids [1057090] from infobox, got %v", e.SteamAppIDs)
			}
		}
	}
	if !found {
		t.Fatal("manifest entry for 137485 not found")
	}
}
