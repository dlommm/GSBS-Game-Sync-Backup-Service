package paths

import (
	"os"
	"path/filepath"
	"testing"
)

// makeCompatDataDir creates <lib>/steamapps/compatdata/<appID>/ in the test temp tree
// and returns the path to that directory.
func makeCompatDataDir(t *testing.T, lib, appID string) string {
	t.Helper()
	dir := filepath.Join(lib, "steamapps", "compatdata", appID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("makeCompatDataDir: %v", err)
	}
	return dir
}

func TestResolveWindowsTemplateAsProton_AppData(t *testing.T) {
	root := t.TempDir()
	lib := filepath.Join(root, "steam")
	makeCompatDataDir(t, lib, "12345")

	got, err := ResolveWindowsTemplateAsProton(`%APPDATA%\Game\saves`, "12345", []string{lib})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(lib, "steamapps", "compatdata", "12345",
		"pfx", "drive_c", "users", "steamuser", "AppData", "Roaming", "Game", "saves")
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %v, want [%s]", got, want)
	}
}

func TestResolveWindowsTemplateAsProton_LocalAppData(t *testing.T) {
	root := t.TempDir()
	lib := filepath.Join(root, "steam")
	makeCompatDataDir(t, lib, "12345")

	got, err := ResolveWindowsTemplateAsProton(`%LOCALAPPDATA%\Game\cache`, "12345", []string{lib})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(lib, "steamapps", "compatdata", "12345",
		"pfx", "drive_c", "users", "steamuser", "AppData", "Local", "Game", "cache")
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %v, want [%s]", got, want)
	}
}

func TestResolveWindowsTemplateAsProton_UserProfile(t *testing.T) {
	root := t.TempDir()
	lib := filepath.Join(root, "steam")
	makeCompatDataDir(t, lib, "54321")

	got, err := ResolveWindowsTemplateAsProton(`%USERPROFILE%\Documents\Game`, "54321", []string{lib})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(lib, "steamapps", "compatdata", "54321",
		"pfx", "drive_c", "users", "steamuser", "Documents", "Game")
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %v, want [%s]", got, want)
	}
}

func TestResolveWindowsTemplateAsProton_Public(t *testing.T) {
	root := t.TempDir()
	lib := filepath.Join(root, "steam")
	makeCompatDataDir(t, lib, "99")

	got, err := ResolveWindowsTemplateAsProton(`%PUBLIC%\Documents\Game`, "99", []string{lib})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(lib, "steamapps", "compatdata", "99",
		"pfx", "drive_c", "users", "Public", "Documents", "Game")
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %v, want [%s]", got, want)
	}
}

func TestResolveWindowsTemplateAsProton_ProgramData(t *testing.T) {
	root := t.TempDir()
	lib := filepath.Join(root, "steam")
	makeCompatDataDir(t, lib, "77")

	got, err := ResolveWindowsTemplateAsProton(`%PROGRAMDATA%\Game\saves`, "77", []string{lib})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(lib, "steamapps", "compatdata", "77",
		"pfx", "drive_c", "ProgramData", "Game", "saves")
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %v, want [%s]", got, want)
	}
}

func TestResolveWindowsTemplateAsProton_NotInstalled(t *testing.T) {
	root := t.TempDir()
	lib := filepath.Join(root, "steam")
	// Intentionally do not create compatdata dir — game not installed.

	got, err := ResolveWindowsTemplateAsProton(`%APPDATA%\Game\saves`, "12345", []string{lib})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty result for uninstalled game, got %v", got)
	}
}

func TestResolveWindowsTemplateAsProton_MultipleLibraries_OnlyInstalled(t *testing.T) {
	root := t.TempDir()
	lib1 := filepath.Join(root, "steam1")
	lib2 := filepath.Join(root, "steam2") // no compatdata
	lib3 := filepath.Join(root, "steam3")

	makeCompatDataDir(t, lib1, "99999")
	makeCompatDataDir(t, lib3, "99999")

	got, err := ResolveWindowsTemplateAsProton(`%APPDATA%\Game\saves`, "99999", []string{lib1, lib2, lib3})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 results (lib1 + lib3), got %v", got)
	}
	want1 := filepath.Join(lib1, "steamapps", "compatdata", "99999",
		"pfx", "drive_c", "users", "steamuser", "AppData", "Roaming", "Game", "saves")
	want3 := filepath.Join(lib3, "steamapps", "compatdata", "99999",
		"pfx", "drive_c", "users", "steamuser", "AppData", "Roaming", "Game", "saves")
	if got[0] != want1 || got[1] != want3 {
		t.Fatalf("got %v, want [%s, %s]", got, want1, want3)
	}
}

func TestResolveWindowsTemplateAsProton_ForwardSlashTemplate(t *testing.T) {
	root := t.TempDir()
	lib := filepath.Join(root, "steam")
	makeCompatDataDir(t, lib, "12345")

	// Forward slashes should work the same as backslashes.
	got, err := ResolveWindowsTemplateAsProton(`%APPDATA%/Game/saves`, "12345", []string{lib})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(lib, "steamapps", "compatdata", "12345",
		"pfx", "drive_c", "users", "steamuser", "AppData", "Roaming", "Game", "saves")
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %v, want [%s]", got, want)
	}
}

func TestResolveWindowsTemplateAsProton_UnrecognisedPlaceholder(t *testing.T) {
	root := t.TempDir()
	lib := filepath.Join(root, "steam")
	makeCompatDataDir(t, lib, "12345")

	// A template with no supported Windows placeholder should return nil, nil.
	got, err := ResolveWindowsTemplateAsProton(`<SteamLibrary-folder>/steamapps/common/Game`, "12345", []string{lib})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for unrecognised placeholder, got %v", got)
	}
}

func TestResolveWindowsTemplateAsProton_EmptyInputs(t *testing.T) {
	got, err := ResolveWindowsTemplateAsProton("", "12345", []string{"/steam"})
	if err != nil || got != nil {
		t.Fatalf("empty template: got %v %v", got, err)
	}

	got, err = ResolveWindowsTemplateAsProton(`%APPDATA%\Game`, "", []string{"/steam"})
	if err != nil || got != nil {
		t.Fatalf("empty appID: got %v %v", got, err)
	}

	got, err = ResolveWindowsTemplateAsProton(`%APPDATA%\Game`, "123", []string{})
	if err != nil || got != nil {
		t.Fatalf("empty libraries: got %v %v", got, err)
	}
}

func TestResolveWindowsTemplateAsProton_CaseInsensitivePrefix(t *testing.T) {
	root := t.TempDir()
	lib := filepath.Join(root, "steam")
	makeCompatDataDir(t, lib, "12345")

	// Lowercase placeholder — should still resolve.
	got, err := ResolveWindowsTemplateAsProton(`%appdata%\Game\saves`, "12345", []string{lib})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(lib, "steamapps", "compatdata", "12345",
		"pfx", "drive_c", "users", "steamuser", "AppData", "Roaming", "Game", "saves")
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %v, want [%s]", got, want)
	}
}

func TestResolveWindowsTemplateAsProton_LocalAppDataVsAppData(t *testing.T) {
	// %LOCALAPPDATA% must not accidentally match %APPDATA%
	root := t.TempDir()
	lib := filepath.Join(root, "steam")
	makeCompatDataDir(t, lib, "42")

	local, err := ResolveWindowsTemplateAsProton(`%LOCALAPPDATA%\Game\saves`, "42", []string{lib})
	if err != nil {
		t.Fatal(err)
	}
	roaming, err := ResolveWindowsTemplateAsProton(`%APPDATA%\Game\saves`, "42", []string{lib})
	if err != nil {
		t.Fatal(err)
	}
	if len(local) != 1 || len(roaming) != 1 {
		t.Fatalf("expected 1 result each, got local=%v roaming=%v", local, roaming)
	}
	if local[0] == roaming[0] {
		t.Fatalf("%%LOCALAPPDATA%% and %%APPDATA%% should resolve to different paths:\nlocal=%s\nroaming=%s", local[0], roaming[0])
	}
}
