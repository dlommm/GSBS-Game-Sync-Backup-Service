package pcgw

import "testing"

func TestNormalizePathTemplate_KnownMappings(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"{{p|appdata}}\\Game", "%APPDATA%/Game"},
		{"{{P|localappdata}}\\save", "%LOCALAPPDATA%/save"},
		{"{{p|userprofile}}\\docs", "%USERPROFILE%/docs"},
		{"{{p|userprofile\\documents}}\\save", "%USERPROFILE%/Documents/save"},
		{"{{p|public}}\\shared", "%PUBLIC%/shared"},
		{"{{p|programdata}}\\cfg", "%PROGRAMDATA%/cfg"},
		{"{{p|programfiles}}\\Game", "%PROGRAMFILES%/Game"},
		{"{{p|uid}}\\save", "<user-id>/save"},
		{"{{p|steam}}\\steamapps", "<SteamLibrary-folder>/steamapps"},
		{"{{p|uplay}}\\savegames", "<Ubisoft-Connect-folder>/savegames"},
		{"{{p|gog}}\\Games", "<GOG-Galaxy-folder>/Games"},
		{"{{p|epic}}\\Game", "<Epic-Games-folder>/Game"},
		{"{{p|ea}}\\save", "<EA-App-folder>/save"},
		{"{{p|origin}}\\save", "<EA-App-folder>/save"},
		{"{{p|xbox}}\\package", "<Xbox-App-folder>/package"},
		{"{{p|heroic}}\\Games", "<Heroic-folder>/Games"},
		{"{{p|lutris}}\\games", "<Lutris-folder>/games"},
		{"{{p|bottles}}\\data", "<Bottles-folder>/data"},
		{"{{p|prism}}\\instances", "<Prism-folder>/instances"},
		{"{{p|flatpak}}\\steamapps", "<Flatpak-Steam-folder>/steamapps"},
		{"{{p|flatpak-steam}}\\steamapps", "<Flatpak-Steam-folder>/steamapps"},
		{"{{p|linuxhome}}/.config/game", "%USERPROFILE%/.config/game"},
		{"{{p|osxhome}}/Library", "%USERPROFILE%/Library"},
		{"{{p|xdgdatahome}}/game", "%LOCALAPPDATA%/game"},
		{"{{p|xdgconfighome}}/game", "%APPDATA%/game"},
	}
	for _, tt := range tests {
		got := NormalizePathTemplate(tt.in)
		if got != tt.want {
			t.Errorf("NormalizePathTemplate(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestNormalizePathTemplate_UnknownPreserved(t *testing.T) {
	in := "{{p|future}}\\save and {{p|game}}\\data"
	got := NormalizePathTemplate(in)
	if got != "{{p|future}}/save and <game-install-folder>/data" {
		t.Fatalf("unknown preserved, game mapped: got %q", got)
	}
}

func TestSplitNormalizePathTemplates_PCGWNestedPlaceholders(t *testing.T) {
	raw := `{{p|steam}}/userdata/{{p|uid}}/222940/remote/kofxiii/`
	got := SplitNormalizePathTemplates(raw)
	want := `<SteamLibrary-folder>/userdata/<user-id>/222940/remote/kofxiii/`
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %v, want [%q]", got, want)
	}
}

func TestSplitNormalizePathTemplates_GameProfile(t *testing.T) {
	raw := `{{p|game}}/Profiles`
	got := SplitNormalizePathTemplates(raw)
	want := `<game-install-folder>/Profiles`
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %v, want [%q]", got, want)
	}
}

func TestSplitNormalizePathTemplates_UserDocuments(t *testing.T) {
	raw := `{{p|userprofile\Documents}}\PlanetExplorers\`
	got := SplitNormalizePathTemplates(raw)
	want := `%USERPROFILE%/Documents/PlanetExplorers/`
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %v, want [%q]", got, want)
	}
}

func TestSplitNormalizePathTemplates_PipeSeparatedRealPaths(t *testing.T) {
	raw := `{{p|appdata}}\GameA\|{{p|localappdata}}\GameB\`
	got := SplitNormalizePathTemplates(raw)
	want := []string{`%APPDATA%/GameA/`, `%LOCALAPPDATA%/GameB/`}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestSplitNormalizePathTemplates_WitcherPipeGlobs(t *testing.T) {
	// Glob tails must survive normalization: re-parsing the stored template
	// must yield directory + include pattern, never a sync-all directory rule.
	raw := `%USERPROFILE%/Documents/The Witcher 3/gamesaves/*.png|` +
		`%USERPROFILE%/Documents/The Witcher 3/gamesaves/*.sav`
	got := SplitNormalizePathTemplates(raw)
	want := []string{
		`%USERPROFILE%/Documents/The Witcher 3/gamesaves/*.png`,
		`%USERPROFILE%/Documents/The Witcher 3/gamesaves/*.sav`,
	}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("SplitNormalizePathTemplates() = %v, want %v", got, want)
	}
}

func TestSplitNormalizePathTemplates_SingleGlob(t *testing.T) {
	got := SplitNormalizePathTemplates(`%APPDATA%/Game/saves/*.dat`)
	want := `%APPDATA%/Game/saves/*.dat`
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %v, want [%q]", got, want)
	}
}

func TestSplitNormalizePathTemplates_SingleFileKeepsName(t *testing.T) {
	// Regression: 12 Orbits (PCGW 51667) lists a single plist file; the stored
	// template must keep the filename so the rebuilt rule never becomes
	// sync-all on ~/Library/Preferences.
	got := SplitNormalizePathTemplates(`{{p|linuxhome}}/Library/Preferences/unity.Roman Uhlig.12 orbits.plist`)
	want := `%USERPROFILE%/Library/Preferences/unity.Roman Uhlig.12 orbits.plist`
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %v, want [%q]", got, want)
	}
}

func TestSplitNormalizePathTemplates_Dedupes(t *testing.T) {
	raw := `%USERPROFILE%/save|%USERPROFILE%/save`
	got := SplitNormalizePathTemplates(raw)
	if len(got) != 1 || got[0] != `%USERPROFILE%/save` {
		t.Fatalf("dedup failed: %v", got)
	}
}

func TestNormalizePathTemplate_NonPathTemplatesPreserved(t *testing.T) {
	in := "prefix {{Game data/saves|Windows|path}} suffix"
	got := NormalizePathTemplate(in)
	if got != in {
		t.Fatalf("non-p templates must not be stripped: got %q", got)
	}
}

func TestSplitNormalizePathTemplates_BrSeparated(t *testing.T) {
	// <br>-separated paths inside one template argument must produce two separate entries.
	tests := []struct {
		name string
		raw  string
	}{
		{"br", `%APPDATA%/Game/saves<br>%LOCALAPPDATA%/Game/saves`},
		{"br-self-close", `%APPDATA%/Game/saves<br/>%LOCALAPPDATA%/Game/saves`},
		{"br-xhtml", `%APPDATA%/Game/saves<br />%LOCALAPPDATA%/Game/saves`},
		{"br-upper", `%APPDATA%/Game/saves<BR>%LOCALAPPDATA%/Game/saves`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SplitNormalizePathTemplates(tt.raw)
			if len(got) != 2 {
				t.Fatalf("expected 2 paths, got %d: %v", len(got), got)
			}
			if got[0] != `%APPDATA%/Game/saves` {
				t.Errorf("path[0] = %q, want %%APPDATA%%/Game/saves", got[0])
			}
			if got[1] != `%LOCALAPPDATA%/Game/saves` {
				t.Errorf("path[1] = %q, want %%LOCALAPPDATA%%/Game/saves", got[1])
			}
		})
	}
}
