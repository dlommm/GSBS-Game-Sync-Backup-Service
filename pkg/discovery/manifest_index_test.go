package discovery

import (
	"testing"

	"github.com/gsbs/gsbs/pkg/types"
)

func TestBuildManifestIndexSteam(t *testing.T) {
	idx := BuildManifestIndex([]types.GameSaveLocation{
		{GameID: "999", GameTitle: "Test Game", SteamAppIDs: []string{"311560", "123"}},
	})
	g := InstalledGame{GameID: "311560", Launcher: "steam", Title: "Test Game"}
	if got := idx.ResolveManifestGameID(g, nil); got != "999" {
		t.Fatalf("expected 999, got %q", got)
	}
}

func TestMatchManifestWithIndex(t *testing.T) {
	idx := BuildManifestIndex([]types.GameSaveLocation{
		{GameID: "42", SteamAppIDs: []string{"100"}},
	})
	installed := []InstalledGame{{GameID: "100", Launcher: "steam", Title: "Foo"}}
	matched := MatchManifestWithIndex(installed, idx, nil)
	if len(matched) != 1 || matched[0].ManifestGameID != "42" {
		t.Fatalf("unexpected match: %+v", matched)
	}
}
