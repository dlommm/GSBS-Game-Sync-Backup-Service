package discovery

import (
	"strings"

	"github.com/gsbs/gsbs/pkg/types"
)

// GameMeta holds v2-only metadata used for discovery and path hints.
type GameMeta struct {
	GameID             string
	Title              string
	ProtonSupportLevel string
	Engines            []string
	CommonInstallPaths []string
	PlatformsPresent   []string
}

// ManifestV2Index extends ManifestIndex with v2 rich metadata and other_ids lookup.
type ManifestV2Index struct {
	*ManifestIndex
	byOtherID map[string]string // "store:id" -> game_id
	gameMeta  map[string]GameMeta
}

// BuildManifestV2Index builds an index from v2 games and flat entries.
func BuildManifestV2Index(games []types.ManifestV2Game, entries []types.GameSaveLocation) *ManifestV2Index {
	base := BuildManifestIndex(entries)
	idx := &ManifestV2Index{
		ManifestIndex: base,
		byOtherID:     make(map[string]string),
		gameMeta:      make(map[string]GameMeta),
	}
	for _, g := range games {
		gid := g.GameID
		idx.gameMeta[gid] = GameMeta{
			GameID:             gid,
			Title:              g.Title,
			ProtonSupportLevel: g.ProtonSupportLevel,
			Engines:            g.Engines,
			CommonInstallPaths: g.CommonInstallPaths,
			PlatformsPresent:   g.PlatformsPresent,
		}
		for store, id := range g.OtherIDs {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			key := normalizeOtherIDKey(store, id)
			idx.byOtherID[key] = gid
		}
	}
	return idx
}

func normalizeOtherIDKey(store, id string) string {
	return strings.ToLower(strings.TrimSpace(store)) + ":" + strings.TrimSpace(id)
}

// Meta returns v2 metadata for a manifest game_id.
func (idx *ManifestV2Index) Meta(gameID string) (GameMeta, bool) {
	if idx == nil {
		return GameMeta{}, false
	}
	m, ok := idx.gameMeta[gameID]
	return m, ok
}

// ResolveManifestGameID resolves an installed game using v2 other_ids in addition to base index.
func (idx *ManifestV2Index) ResolveManifestGameID(g InstalledGame, idMap map[string]string) string {
	if idx == nil || idx.ManifestIndex == nil {
		return ""
	}
	if gid := idx.ManifestIndex.ResolveManifestGameID(g, idMap); gid != "" {
		return gid
	}
	if idx.byOtherID == nil {
		return ""
	}
	key := normalizeOtherIDKey(g.Launcher, g.GameID)
	if gid := idx.byOtherID[key]; gid != "" {
		return gid
	}
	return idx.byOtherID[normalizeOtherIDKey(g.Launcher, g.Title)]
}

// MatchReason describes how an install was matched to the manifest.
func (idx *ManifestV2Index) MatchReason(g InstalledGame, manifestGameID string, idMap map[string]string) string {
	if idx == nil {
		return ""
	}
	if idMap != nil {
		if gid, ok := idMap[g.Launcher+":"+g.GameID]; ok && gid == manifestGameID {
			return "cached"
		}
	}
	switch g.Launcher {
	case "steam":
		if idx.bySteam[g.GameID] == manifestGameID {
			return "steam:" + g.GameID
		}
	case "gog":
		if idx.byGOG[g.GameID] == manifestGameID {
			return "gog:" + g.GameID
		}
	case "epic", "heroic":
		if idx.byEpic[normalizeEpicID(g.GameID)] == manifestGameID {
			return g.Launcher + ":" + g.GameID
		}
	case "ubisoft":
		if idx.byUbisoft[g.GameID] == manifestGameID {
			return "ubisoft:" + g.GameID
		}
	}
	if key := normalizeOtherIDKey(g.Launcher, g.GameID); idx.byOtherID[key] == manifestGameID {
		return "other_id:" + key
	}
	if g.Title != "" && idx.byTitle[normalizeTitle(g.Title)] == manifestGameID {
		return "title"
	}
	return "matched"
}

// MatchManifestWithV2Index matches installed games using the v2-aware index.
func MatchManifestWithV2Index(installed []InstalledGame, idx *ManifestV2Index, idMap map[string]string) []MatchedGame {
	var matched []MatchedGame
	seen := make(map[string]bool)
	for _, g := range installed {
		mgid := idx.ResolveManifestGameID(g, idMap)
		if mgid == "" || seen[mgid] {
			continue
		}
		seen[mgid] = true
		matched = append(matched, MatchedGame{
			ManifestGameID: mgid,
			GameID:         g.GameID,
			Title:          g.Title,
			Launcher:       g.Launcher,
			MatchReason:    idx.MatchReason(g, mgid, idMap),
		})
	}
	return matched
}
