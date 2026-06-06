package saverule

import (
	"reflect"
	"strings"
	"testing"

	"github.com/gsbs/gsbs/pkg/types"
)

func identityNormalize(s string) string { return s }

func TestParseSaveRules_WitcherPipeGlobs(t *testing.T) {
	raw := `%USERPROFILE%/Documents/The Witcher 3/gamesaves/*.png|` +
		`%USERPROFILE%/Documents/The Witcher 3/gamesaves/*.sav`
	got := ParseSaveRules(raw, "windows", false, identityNormalize)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1: %+v", len(got), got)
	}
	wantDir := `%USERPROFILE%/Documents/The Witcher 3/gamesaves`
	if got[0].Directory != wantDir {
		t.Fatalf("Directory = %q, want %q", got[0].Directory, wantDir)
	}
	wantPatterns := []string{"*.png", "*.sav"}
	if !reflect.DeepEqual(got[0].IncludePatterns, wantPatterns) {
		t.Fatalf("IncludePatterns = %v, want %v", got[0].IncludePatterns, wantPatterns)
	}
	if got[0].SyncAll {
		t.Fatal("SyncAll should be false when patterns present")
	}
}

func TestParseSaveRules_MultiGlobSameDir(t *testing.T) {
	raw := `%APPDATA%/Game/saves/*.dat|%APPDATA%/Game/saves/*.bak`
	got := ParseSaveRules(raw, "windows", false, identityNormalize)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	wantPatterns := []string{"*.bak", "*.dat"}
	if !reflect.DeepEqual(got[0].IncludePatterns, wantPatterns) {
		t.Fatalf("IncludePatterns = %v, want %v", got[0].IncludePatterns, wantPatterns)
	}
}

func TestParseSaveRules_PipeSplit(t *testing.T) {
	raw := `%USERPROFILE%/save|%LOCALAPPDATA%/save`
	got := ParseSaveRules(raw, "windows", false, identityNormalize)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2: %+v", len(got), got)
	}
	if got[0].Directory != `%USERPROFILE%/save` || got[1].Directory != `%LOCALAPPDATA%/save` {
		t.Fatalf("unexpected directories: %+v", got)
	}
	for _, r := range got {
		if !r.SyncAll {
			t.Fatalf("directory-only rule should SyncAll: %+v", r)
		}
	}
}

func TestParseSaveRules_ExtensionlessDirectory(t *testing.T) {
	got := ParseSaveRules(`%APPDATA%/Game/saves`, "windows", false, identityNormalize)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Directory != `%APPDATA%/Game/saves` {
		t.Fatalf("Directory = %q", got[0].Directory)
	}
	if !got[0].SyncAll || len(got[0].IncludePatterns) != 0 {
		t.Fatalf("expected SyncAll directory-only rule: %+v", got[0])
	}
}

func TestParseSaveRules_SingleFile(t *testing.T) {
	got := ParseSaveRules(`%APPDATA%/Game/save.dat`, "windows", false, identityNormalize)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Directory != `%APPDATA%/Game` {
		t.Fatalf("Directory = %q", got[0].Directory)
	}
	if !reflect.DeepEqual(got[0].IncludePatterns, []string{"save.dat"}) {
		t.Fatalf("IncludePatterns = %v", got[0].IncludePatterns)
	}
}

func TestParseSaveRules_SaveStarPattern(t *testing.T) {
	got := ParseSaveRules(`%USERPROFILE%/Documents/Game/Save*`, "windows", false, identityNormalize)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Directory != `%USERPROFILE%/Documents/Game` {
		t.Fatalf("Directory = %q", got[0].Directory)
	}
	if !reflect.DeepEqual(got[0].IncludePatterns, []string{"Save*"}) {
		t.Fatalf("IncludePatterns = %v", got[0].IncludePatterns)
	}
	if !got[0].Recursive {
		t.Fatal("Save* pattern should set Recursive")
	}
}

func TestParseSaveRules_Dedupe(t *testing.T) {
	raw := `%USERPROFILE%/save|%USERPROFILE%/save`
	got := ParseSaveRules(raw, "windows", false, identityNormalize)
	if len(got) != 1 {
		t.Fatalf("dedup failed: %+v", got)
	}
}

func TestParseSaveRules_Malformed(t *testing.T) {
	if got := ParseSaveRules("", "windows", false, identityNormalize); got != nil {
		t.Fatalf("empty raw: %+v", got)
	}
	if got := ParseSaveRules("|  |", "windows", false, identityNormalize); got != nil {
		t.Fatalf("pipe-only raw: %+v", got)
	}
	if got := ParseSaveRules("   ", "windows", false, identityNormalize); got != nil {
		t.Fatalf("whitespace raw: %+v", got)
	}
}

func TestParseSaveRules_WitcherSettingsSeparateRule(t *testing.T) {
	raw := `%USERPROFILE%/Documents/The Witcher 3/settings|` +
		`%USERPROFILE%/Documents/The Witcher 3/gamesaves/*.png|` +
		`%USERPROFILE%/Documents/The Witcher 3/gamesaves/*.sav`
	got := ParseSaveRules(raw, "windows", false, identityNormalize)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2: %+v", len(got), got)
	}

	var settings, saves types.SaveRule
	for _, r := range got {
		switch r.Directory {
		case `%USERPROFILE%/Documents/The Witcher 3/settings`:
			settings = r
		case `%USERPROFILE%/Documents/The Witcher 3/gamesaves`:
			saves = r
		}
	}
	if settings.Directory == "" {
		t.Fatal("missing settings rule")
	}
	if !settings.SyncAll {
		t.Fatalf("settings should SyncAll: %+v", settings)
	}
	if saves.Directory == "" {
		t.Fatal("missing gamesaves rule")
	}
	if !reflect.DeepEqual(saves.IncludePatterns, []string{"*.png", "*.sav"}) {
		t.Fatalf("gamesaves patterns = %v", saves.IncludePatterns)
	}
}

func TestParseSaveRules_PCGWTemplatePipesIgnored(t *testing.T) {
	normalize := func(s string) string {
		return strings.ReplaceAll(s, "{{p|steam}}", "<steam>")
	}
	raw := `{{p|steam}}/userdata/{{p|uid}}/save`
	got := ParseSaveRules(raw, "windows", false, normalize)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1: %+v", len(got), got)
	}
	if got[0].Directory != `<steam>/userdata/{{p|uid}}/save` {
		t.Fatalf("Directory = %q", got[0].Directory)
	}
}

func TestSplitOutsideTemplates(t *testing.T) {
	parts := splitOutsideTemplates(`a|{{p|b}}|c`, '|')
	if len(parts) != 3 || parts[0] != "a" || parts[1] != "{{p|b}}" || parts[2] != "c" {
		t.Fatalf("got %#v", parts)
	}
}

func TestParseSaveRules_IsConfigAndPlatform(t *testing.T) {
	got := ParseSaveRules(`%APPDATA%/cfg`, "linux", true, identityNormalize)
	if len(got) != 1 {
		t.Fatalf("len = %d", len(got))
	}
	if got[0].Platform != "linux" || !got[0].IsConfig {
		t.Fatalf("metadata not set: %+v", got[0])
	}
}

func TestParseSaveRules_RegistryPathsExcluded(t *testing.T) {
	registryCases := []string{
		// PCGW {{p|hkcu}} placeholder (preserved by NormalizePathTemplate since unknown)
		`{{p|hkcu}}\Software\GameCo\MyGame`,
		`{{p|hklm}}\Software\GameCo`,
		`{{p|hkcr}}\GameCo.SaveFile`,
		`{{p|hku}}\S-1-5-21\Software`,
		// Literal registry paths
		`HKEY_CURRENT_USER\Software\GameCo`,
		`HKEY_LOCAL_MACHINE\SOFTWARE\GameCo`,
	}
	for _, raw := range registryCases {
		got := ParseSaveRules(raw, "windows", false, identityNormalize)
		if len(got) != 0 {
			t.Errorf("registry path %q should be excluded, got %+v", raw, got)
		}
	}
}

func TestParseSaveRules_RegistryMixedWithValid(t *testing.T) {
	// Valid path mixed with a registry path — only valid path should survive.
	raw := `%APPDATA%/Game/saves|HKEY_CURRENT_USER\Software\Game`
	got := ParseSaveRules(raw, "windows", false, identityNormalize)
	if len(got) != 1 {
		t.Fatalf("expected 1 rule, got %d: %+v", len(got), got)
	}
	if got[0].Directory != `%APPDATA%/Game/saves` {
		t.Errorf("Directory = %q", got[0].Directory)
	}
}
