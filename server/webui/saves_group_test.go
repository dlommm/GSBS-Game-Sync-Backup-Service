package webui

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/gsbs/gsbs/server/store"
)

func sampleSaves() []store.SaveSummary {
	var out []store.SaveSummary
	// A large game with many save files plus a couple of config files.
	for i := 0; i < 600; i++ {
		out = append(out, store.SaveSummary{
			GameID: "tess", GameTitle: "Tesseract",
			PathKey: fmt.Sprintf("%064x", i), RelativePath: fmt.Sprintf("saves/slot_%03d.sav", i),
			SizeBytes: int64(1024 * (i%50 + 1)), UpdatedAt: "2026-06-24T03:50:00Z",
		})
	}
	out = append(out,
		store.SaveSummary{GameID: "tess", GameTitle: "Tesseract", PathKey: "cfgaaa", RelativePath: "config/settings.ini", SizeBytes: 400, UpdatedAt: "2026-06-24T03:50:00Z"},
		store.SaveSummary{GameID: "tess", GameTitle: "Tesseract", PathKey: "cfgbbb", RelativePath: "options.cfg", SizeBytes: 200, UpdatedAt: "2026-06-24T03:50:00Z"},
	)
	// A small game.
	out = append(out,
		store.SaveSummary{GameID: "w3", GameTitle: "The Witcher 3: Wild Hunt", PathKey: "w3a", RelativePath: "gamesaves/quicksave.sav", SizeBytes: 71 * 1024, UpdatedAt: "2026-06-19T10:00:00Z", Encrypted: true},
		store.SaveSummary{GameID: "w3", GameTitle: "The Witcher 3: Wild Hunt", PathKey: "w3b", RelativePath: "user.settings", SizeBytes: 950 * 1024, UpdatedAt: "2026-06-19T10:00:00Z"},
	)
	return out
}

func TestGroupSavesStructure(t *testing.T) {
	groups := groupSaves(sampleSaves())
	if len(groups) != 2 {
		t.Fatalf("want 2 game groups, got %d", len(groups))
	}
	tess := groups[0]
	if tess.Title != "Tesseract" || tess.FileCount != 602 {
		t.Fatalf("Tesseract group wrong: title=%q files=%d", tess.Title, tess.FileCount)
	}
	// Categories: Saves first, then Config.
	if len(tess.Categories) != 2 || tess.Categories[0].Name != "Saves" || tess.Categories[1].Name != "Config" {
		t.Fatalf("unexpected categories: %+v", tess.Categories)
	}
	if got := len(tess.Categories[0].Files); got != 600 {
		t.Fatalf("want 600 saves, got %d", got)
	}
	if got := len(tess.Categories[1].Files); got != 2 {
		t.Fatalf("want 2 config files, got %d", got)
	}
	// Filenames derive from relative path; folder is captured separately.
	first := tess.Categories[0].Files[0]
	if first.Name != "slot_000.sav" || first.Folder != "saves" {
		t.Fatalf("name/folder derivation wrong: name=%q folder=%q", first.Name, first.Folder)
	}
}

func TestCategorize(t *testing.T) {
	cases := map[string]string{
		"saves/slot1.sav":     "Saves",
		"config/settings.ini": "Config",
		"options.cfg":         "Config",
		"user.json":           "Config",
		"profile.dat":         "Saves",
		"":                    "Other",
	}
	for in, want := range cases {
		if got := categorize(in); got != want {
			t.Errorf("categorize(%q)=%q want %q", in, got, want)
		}
	}
}

func TestDashboardSavesPartialRenders(t *testing.T) {
	tmpl := parseTemplates()
	var cards []gameCard
	for _, g := range groupSaves(sampleSaves()) {
		cards = append(cards, gameCard{
			GameID: g.GameID, Title: g.Title, FileCount: g.FileCount,
			TotalBytes: g.TotalBytes, LastSynced: g.LastSynced,
			Status: gameSyncStatus(g.LastSynced),
		})
	}
	data := map[string]interface{}{"Games": cards, "TotalGames": len(cards) + 3}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "partials/dashboard_saves.html", data); err != nil {
		t.Fatalf("render: %v", err)
	}
	html := buf.String()
	for _, want := range []string{"Tesseract", "The Witcher 3", "game-row-card", "View Versions", "View all"} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered HTML missing %q", want)
		}
	}
}
