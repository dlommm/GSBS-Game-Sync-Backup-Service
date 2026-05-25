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
	if got != "{{p|future}}/save and {{p|game}}/data" {
		t.Fatalf("unknown placeholders should be preserved: got %q", got)
	}
}

func TestSplitNormalizePathTemplates_WitcherPipeGlobs(t *testing.T) {
	raw := `%USERPROFILE%/Documents/The Witcher 3/gamesaves/*.png|` +
		`%USERPROFILE%/Documents/The Witcher 3/gamesaves/*.sav`
	got := SplitNormalizePathTemplates(raw)
	want := `%USERPROFILE%/Documents/The Witcher 3/gamesaves`
	if len(got) != 1 || got[0] != want {
		t.Fatalf("SplitNormalizePathTemplates() = %v, want [%q]", got, want)
	}
}

func TestSplitNormalizePathTemplates_SingleGlob(t *testing.T) {
	got := SplitNormalizePathTemplates(`%APPDATA%/Game/saves/*.dat`)
	want := `%APPDATA%/Game/saves`
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
