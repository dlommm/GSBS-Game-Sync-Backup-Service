package discovery

import (
	"testing"

	"github.com/gsbs/gsbs/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildManifestV2Index_OtherIDs(t *testing.T) {
	games := []types.ManifestV2Game{{
		GameID: "100",
		Title:  "Doom",
		OtherIDs: map[string]string{
			"gog": "1207658930",
		},
	}}
	entries := []types.GameSaveLocation{{
		GameID: "100", GameTitle: "Doom", GOGID: "1207658930",
	}}
	idx := BuildManifestV2Index(games, entries)
	require.NotNil(t, idx)

	g := InstalledGame{Launcher: "gog", GameID: "1207658930", Title: "Doom"}
	assert.Equal(t, "100", idx.ResolveManifestGameID(g, nil))
	assert.Equal(t, "gog:1207658930", idx.MatchReason(g, "100", nil))
}

func TestMatchManifestWithV2Index(t *testing.T) {
	games := []types.ManifestV2Game{{
		GameID:      "55",
		Title:       "Hades",
		SteamAppIDs: []string{"1145360"},
	}}
	entries := []types.GameSaveLocation{{
		GameID: "55", GameTitle: "Hades", SteamAppIDs: []string{"1145360"}, Platform: "linux", PathTemplate: "/x",
	}}
	idx := BuildManifestV2Index(games, entries)
	installed := []InstalledGame{{Launcher: "steam", GameID: "1145360", Title: "Hades"}}
	matched := MatchManifestWithV2Index(installed, idx, nil)
	require.Len(t, matched, 1)
	assert.Equal(t, "55", matched[0].ManifestGameID)
	assert.Equal(t, "steam:1145360", matched[0].MatchReason)
}

func TestManifestV2Index_Meta(t *testing.T) {
	games := []types.ManifestV2Game{{
		GameID:             "1",
		ProtonSupportLevel: "platinum",
		CommonInstallPaths: []string{"/steam/steamapps/common/foo"},
	}}
	idx := BuildManifestV2Index(games, nil)
	meta, ok := idx.Meta("1")
	require.True(t, ok)
	assert.Equal(t, "platinum", meta.ProtonSupportLevel)
	assert.Equal(t, []string{"/steam/steamapps/common/foo"}, meta.CommonInstallPaths)
}
