package discovery

import (
	"strings"

	"github.com/gsbs/gsbs/pkg/types"
)

// ManifestIndex maps launcher external IDs and titles to canonical manifest game_id (PCGW page ID).
type ManifestIndex struct {
	byGameID  map[string]string // game_id -> game_id (identity)
	bySteam   map[string]string // steam app id -> game_id
	byGOG     map[string]string
	byEpic    map[string]string
	byUbisoft map[string]string
	byTitle   map[string]string // normalized title -> game_id
}

// BuildManifestIndex constructs a lookup index from manifest entries.
func BuildManifestIndex(entries []types.GameSaveLocation) *ManifestIndex {
	idx := &ManifestIndex{
		byGameID:  make(map[string]string),
		bySteam:   make(map[string]string),
		byGOG:     make(map[string]string),
		byEpic:    make(map[string]string),
		byUbisoft: make(map[string]string),
		byTitle:   make(map[string]string),
	}
	seenTitle := make(map[string]string)
	for _, e := range entries {
		gid := e.GameID
		idx.byGameID[gid] = gid
		for _, sid := range e.SteamAppIDs {
			sid = strings.TrimSpace(sid)
			if sid != "" {
				idx.bySteam[sid] = gid
			}
		}
		if e.GOGID != "" {
			idx.byGOG[strings.TrimSpace(e.GOGID)] = gid
		}
		if e.EpicID != "" {
			idx.byEpic[normalizeEpicID(e.EpicID)] = gid
		}
		if e.UbisoftID != "" {
			idx.byUbisoft[strings.TrimSpace(e.UbisoftID)] = gid
		}
		if e.GameTitle != "" {
			key := normalizeTitle(e.GameTitle)
			if key != "" && seenTitle[key] == "" {
				seenTitle[key] = gid
				idx.byTitle[key] = gid
			}
		}
	}
	return idx
}

func normalizeTitle(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func normalizeEpicID(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// ResolveManifestGameID returns the canonical manifest game_id for an installed game, or "" if unknown.
func (idx *ManifestIndex) ResolveManifestGameID(g InstalledGame, idMap map[string]string) string {
	if idx == nil {
		return ""
	}
	if idMap != nil {
		if gid, ok := idMap[g.Launcher+":"+g.GameID]; ok && gid != "" {
			return gid
		}
	}
	switch g.Launcher {
	case "steam":
		if gid := idx.bySteam[g.GameID]; gid != "" {
			return gid
		}
	case "gog":
		if gid := idx.byGOG[g.GameID]; gid != "" {
			return gid
		}
		if gid := idx.byTitle[normalizeTitle(g.Title)]; gid != "" {
			return gid
		}
		if gid := idx.byTitle[normalizeTitle(g.GameID)]; gid != "" {
			return gid
		}
	case "epic", "heroic":
		if gid := idx.byEpic[normalizeEpicID(g.GameID)]; gid != "" {
			return gid
		}
	case "ubisoft":
		if gid := idx.byUbisoft[g.GameID]; gid != "" {
			return gid
		}
	case "lutris", "bottles", "prism", "ea":
		if gid := idx.byTitle[normalizeTitle(g.Title)]; gid != "" {
			return gid
		}
		if gid := idx.byTitle[normalizeTitle(g.GameID)]; gid != "" {
			return gid
		}
	}
	if gid := idx.byGameID[g.GameID]; gid != "" {
		return gid
	}
	if g.Title != "" {
		if gid := idx.byTitle[normalizeTitle(g.Title)]; gid != "" {
			return gid
		}
	}
	return ""
}

// MatchedGame links a detected install to its manifest game_id.
type MatchedGame struct {
	ManifestGameID string `json:"manifest_game_id"`
	GameID         string `json:"game_id"` // launcher-local id
	Title          string `json:"title,omitempty"`
	Launcher       string `json:"launcher"`
}

// MatchManifestWithIndex matches installed games to manifest entries using the index and optional idMap cache.
func MatchManifestWithIndex(installed []InstalledGame, idx *ManifestIndex, idMap map[string]string) []MatchedGame {
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
		})
	}
	return matched
}

// MatchManifest filters manifest game IDs against installed games (legacy API).
func MatchManifest(installed []InstalledGame, manifestGameIDs map[string]bool) []InstalledGame {
	var matched []InstalledGame
	for _, g := range installed {
		if manifestGameIDs[g.GameID] {
			matched = append(matched, g)
			continue
		}
		for id := range manifestGameIDs {
			if strings.EqualFold(g.Title, id) || strings.Contains(strings.ToLower(id), strings.ToLower(g.GameID)) {
				matched = append(matched, g)
				break
			}
		}
	}
	return matched
}
