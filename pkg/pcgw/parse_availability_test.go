package pcgw

import (
	"strings"
	"testing"
)

// Real-shape wikitext: PCGamingWiki writes infobox parameters in lowercase and
// keeps Epic/Ubisoft IDs in {{Availability/row}}, not in the infobox.
const acValhallaWikitext = `{{Infobox game
|cover        = Assassin's Creed Valhalla cover.png
|steam appid  = 2208920
|gogcom id    =
|hltb         = 77729
|igdb         = assassins-creed-valhalla
|wikipedia    = Assassin's Creed Valhalla
}}

==Availability==
{{Availability|
{{Availability/row| Epic Games Store | assassins-creed-valhalla | Ubisoft Connect | | | Windows }}
{{Availability/row| Gamesplanet | 5389-1 | Ubisoft Connect | | | Windows }}
{{Availability/row| Ubisoft Store | 5e849c6c5cdf9a21c0b4e731 | Ubisoft Connect | | | Windows }}
{{Availability/row| Steam | 2208920 | Steam, Ubisoft Connect | | | Windows }}
}}
`

func TestInfoboxValueIsCaseAndSpacingInsensitive(t *testing.T) {
	box := ParseInfoboxGame(acValhallaWikitext)

	// The old code indexed by the Cargo column names and always got "".
	if got := box["HLTB"]; got != "" {
		t.Fatalf("precondition: infobox keys are lowercase, got %q for HLTB", got)
	}

	if got := InfoboxValue(box, "hltb", "howlongtobeat"); got != "77729" {
		t.Errorf("hltb: got %q, want 77729", got)
	}
	if got := InfoboxValue(box, "IGDB"); got != "assassins-creed-valhalla" {
		t.Errorf("igdb: got %q, want assassins-creed-valhalla", got)
	}
	if got := InfoboxValue(box, "Steam_AppID"); got != "2208920" {
		t.Errorf("steam appid via Cargo-style key: got %q, want 2208920", got)
	}
	if got := InfoboxValue(box, "gogcom id"); got != "" {
		t.Errorf("empty parameter must read as empty, got %q", got)
	}
	if got := InfoboxValue(box, "nope"); got != "" {
		t.Errorf("missing parameter: got %q", got)
	}
	if got := InfoboxValue(nil, "hltb"); got != "" {
		t.Errorf("nil infobox: got %q", got)
	}
}

func TestAvailabilityStoreIDs(t *testing.T) {
	epicID, ubisoftID := AvailabilityStoreIDs(acValhallaWikitext)
	if epicID != "assassins-creed-valhalla" {
		t.Errorf("epic: got %q, want assassins-creed-valhalla", epicID)
	}
	if ubisoftID != "5e849c6c5cdf9a21c0b4e731" {
		t.Errorf("ubisoft: got %q, want 5e849c6c5cdf9a21c0b4e731", ubisoftID)
	}
}

// A page with no availability rows must yield empty IDs, not garbage.
func TestAvailabilityStoreIDsAbsent(t *testing.T) {
	epicID, ubisoftID := AvailabilityStoreIDs("{{Infobox game\n|steam appid = 620\n}}")
	if epicID != "" || ubisoftID != "" {
		t.Errorf("got %q/%q, want empty", epicID, ubisoftID)
	}
}

// The first row for a store wins; later rows are alternate editions.
func TestAvailabilityStoreIDsFirstRowWins(t *testing.T) {
	wt := `{{Availability/row| Epic Games Store | control | DRM-free | | | Windows }}
{{Availability/row| Epic Games Store | control-ultimate-edition | DRM-free | | | Windows }}`
	epicID, _ := AvailabilityStoreIDs(wt)
	if epicID != "control" {
		t.Errorf("got %q, want control", epicID)
	}
}

// Ingest must lift both the infobox IDs and the availability IDs onto PageInfo.
func TestIngestPopulatesLauncherIDs(t *testing.T) {
	box := ParseInfoboxGame(acValhallaWikitext)
	if got := InfoboxValue(box, "hltb"); got == "" {
		t.Fatal("hltb missing from parsed infobox")
	}
	epicID, ubisoftID := AvailabilityStoreIDs(acValhallaWikitext)
	if epicID == "" || ubisoftID == "" {
		t.Fatal("availability IDs missing")
	}
}

func TestCargoQuoteEscapesQuotes(t *testing.T) {
	if got := cargoQuote("620"); got != `"620"` {
		t.Errorf("got %s", got)
	}
	// A quote in the value must not be able to close the literal early.
	if got := cargoQuote(`620" OR "1`); got != `"620\" OR \"1"` {
		t.Errorf("got %s", got)
	}
	if got := cargoQuote(`a\b`); got != `"a\\b"` {
		t.Errorf("got %s", got)
	}
}

func TestIsNumericID(t *testing.T) {
	for _, ok := range []string{"0", "620", "2208920"} {
		if !isNumericID(ok) {
			t.Errorf("%q should be numeric", ok)
		}
	}
	for _, bad := range []string{"", "62a", `620" OR "1`, " 620", "-1"} {
		if isNumericID(bad) {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

// Two titles that normalize to the same key must both survive: keeping only the
// last one silently dropped the other section's wikitext, including legacy
// table-format save paths.
func TestSplitWikiSectionsKeepsCollidingSections(t *testing.T) {
	wt := `==Game data==
{{Game data|
{{Game data/saves|Windows|{{p|appdata}}\Alpha}}
}}

==Save game data location==
{{Game data/saves|Windows|{{p|appdata}}\Beta}}

==Some unknown thing==
first-other

==Another unknown thing==
second-other
`
	secs := SplitWikiSections(wt)

	gd, ok := secs["game_data"]
	if !ok {
		t.Fatal("game_data section missing")
	}
	for _, want := range []string{"Alpha", "Beta"} {
		if !strings.Contains(gd.body, want) {
			t.Errorf("game_data lost %q: %s", want, gd.body)
		}
	}

	other, ok := secs["other"]
	if !ok {
		t.Fatal("other section missing")
	}
	for _, want := range []string{"first-other", "second-other"} {
		if !strings.Contains(other.body, want) {
			t.Errorf("other lost %q", want)
		}
	}
}
