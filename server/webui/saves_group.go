package webui

import (
	"path"
	"sort"
	"strings"

	"github.com/gsbs/gsbs/server/store"
)

// saveFileRow is a single synced file within a category, enriched with a
// human-friendly name/folder derived from its relative path.
type saveFileRow struct {
	store.SaveSummary
	Name   string // display filename (basename of relative path) or short hash fallback
	Folder string // parent folder of the relative path; "" when at the save root
}

// saveCategory groups a game's files by kind (Saves / Config / Other).
type saveCategory struct {
	Name       string
	Files      []saveFileRow
	TotalBytes int64
}

// saveGameGroup is the top level of the dashboard saves tree: one entry per game.
type saveGameGroup struct {
	GameID     string
	Title      string
	FileCount  int
	TotalBytes int64
	LastSynced string // most recent updated_at across the game's files
	Categories []saveCategory
}

// smallGameThreshold: games with at most this many files render with their
// categories expanded by default (no extra click needed to see the files).
const smallGameThreshold = 12

// configExts are file extensions that almost always denote configuration
// rather than save data.
var configExts = map[string]bool{
	".ini": true, ".cfg": true, ".conf": true, ".config": true,
	".xml": true, ".json": true, ".yaml": true, ".yml": true,
	".toml": true, ".properties": true, ".props": true,
}

// categorize classifies a save by its relative path. It is a best-effort
// heuristic — the server stores a path hash plus the relative path, but no
// authoritative save/config flag per file.
func categorize(relPath string) string {
	if strings.TrimSpace(relPath) == "" {
		return "Other"
	}
	p := strings.ToLower(strings.ReplaceAll(relPath, "\\", "/"))
	if strings.Contains(p, "config") || strings.Contains(p, "settings") ||
		strings.Contains(p, "options") || strings.Contains(p, "/cfg/") || strings.HasPrefix(p, "cfg/") {
		return "Config"
	}
	if configExts[path.Ext(p)] {
		return "Config"
	}
	return "Saves"
}

// categoryOrder gives stable, intuitive ordering for category sections.
var categoryOrder = map[string]int{"Saves": 0, "Config": 1, "Other": 2}

// nameAndFolder derives a display filename and parent folder from a save's
// relative path, falling back to a short hash of the path key when no relative
// path was recorded (older clients / pre-relative-path data).
func nameAndFolder(s store.SaveSummary) (name, folder string) {
	rel := strings.TrimSpace(strings.ReplaceAll(s.RelativePath, "\\", "/"))
	rel = strings.Trim(rel, "/")
	if rel == "" {
		key := s.PathKey
		if len(key) > 12 {
			key = key[:12] + "…"
		}
		return key, ""
	}
	name = path.Base(rel)
	folder = path.Dir(rel)
	if folder == "." || folder == "/" {
		folder = ""
	}
	return name, folder
}

// groupSaves turns the flat, updated_at-DESC list of save summaries into a
// Game → Category → File tree. Game and file ordering follow the input
// (most-recently-synced first); categories follow categoryOrder.
func groupSaves(saves []store.SaveSummary) []saveGameGroup {
	type catAcc struct {
		files []saveFileRow
		bytes int64
	}
	type gameAcc struct {
		title      string
		lastSynced string
		fileCount  int
		totalBytes int64
		cats       map[string]*catAcc
	}

	order := make([]string, 0)
	games := make(map[string]*gameAcc)

	for _, s := range saves {
		g := games[s.GameID]
		if g == nil {
			g = &gameAcc{title: s.GameTitle, cats: map[string]*catAcc{}}
			if g.title == "" {
				g.title = s.GameID
			}
			games[s.GameID] = g
			order = append(order, s.GameID)
		}
		cat := categorize(s.RelativePath)
		c := g.cats[cat]
		if c == nil {
			c = &catAcc{}
			g.cats[cat] = c
		}
		name, folder := nameAndFolder(s)
		c.files = append(c.files, saveFileRow{SaveSummary: s, Name: name, Folder: folder})
		c.bytes += s.SizeBytes
		g.fileCount++
		g.totalBytes += s.SizeBytes
		if s.UpdatedAt > g.lastSynced { // RFC3339 strings sort chronologically
			g.lastSynced = s.UpdatedAt
		}
	}

	out := make([]saveGameGroup, 0, len(order))
	for _, gid := range order {
		g := games[gid]
		cats := make([]saveCategory, 0, len(g.cats))
		for name, c := range g.cats {
			cats = append(cats, saveCategory{Name: name, Files: c.files, TotalBytes: c.bytes})
		}
		sort.SliceStable(cats, func(i, j int) bool {
			return categoryOrder[cats[i].Name] < categoryOrder[cats[j].Name]
		})
		out = append(out, saveGameGroup{
			GameID:     gid,
			Title:      g.title,
			FileCount:  g.fileCount,
			TotalBytes: g.totalBytes,
			LastSynced: g.lastSynced,
			Categories: cats,
		})
	}
	return out
}
