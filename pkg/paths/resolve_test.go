package paths

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func testResolver() *Resolver {
	return &Resolver{
		Home:           "/home/user",
		LocalAppData:   "/home/user/.local/share",
		AppData:        "/home/user/.config",
		ProgramData:    "C:\\ProgramData",
		ProgramFiles:   "C:\\Program Files",
		UserID:         "steam_12345",
		UbisoftConnect: "C:\\Program Files (x86)\\Ubisoft\\Ubisoft Game Launcher",
		GOGGalaxy:      "C:\\Program Files (x86)\\GOG Galaxy",
		EpicGames:      "C:\\Program Files\\Epic Games",
		XboxApp:        "C:\\Users\\user\\AppData\\Local\\Packages",
		Heroic:         "/home/user/.config/heroic",
		Lutris:         "/home/user/.config/lutris",
		EAApp:          "C:\\Program Files\\EA Games",
		Bottles:        "/home/user/.var/app/com.usebottles.bottles/data/bottles",
		Prism:          "/home/user/.local/share/PrismLauncher",
		FlatpakSteam:   "/home/user/.var/app/com.valvesoftware.Steam/data/Steam",
		SteamLibraries: []string{
			"/steam/main",
			"/steam/secondary",
		},
	}
}

func TestExpandOnePlaceholders(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Linux resolver path tests skipped on Windows")
	}
	r := testResolver()

	tests := []struct {
		name     string
		template string
		os       OS
		want     string
	}{
		{"USERPROFILE windows", "%USERPROFILE%\\Documents\\save", Windows, "/home/user\\Documents\\save"},
		{"LOCALAPPDATA windows", "%LOCALAPPDATA%\\Game\\save", Windows, "/home/user/.local/share\\Game\\save"},
		{"APPDATA windows", "%APPDATA%\\Game\\save", Windows, "/home/user/.config\\Game\\save"},
		{"PROGRAMDATA", "%PROGRAMDATA%\\Game\\save", Windows, "C:\\ProgramData\\Game\\save"},
		{"PROGRAMFILES", "%PROGRAMFILES%\\Game\\save", Windows, "C:\\Program Files\\Game\\save"},
		{"user-id", "<user-id>/saves", Linux, "steam_12345/saves"},
		{"Ubisoft", "<Ubisoft-Connect-folder>/savegames", Windows, "C:\\Program Files (x86)\\Ubisoft\\Ubisoft Game Launcher/savegames"},
		{"GOG", "<GOG-Galaxy-folder>/Games", Windows, "C:\\Program Files (x86)\\GOG Galaxy/Games"},
		{"Epic", "<Epic-Games-folder>/Game", Windows, "C:\\Program Files\\Epic Games/Game"},
		{"Xbox", "<Xbox-App-folder>/Game", Windows, "C:\\Users\\user\\AppData\\Local\\Packages/Game"},
		{"Heroic", "<Heroic-folder>/Games", Linux, "/home/user/.config/heroic/Games"},
		{"Lutris", "<Lutris-folder>/games", Linux, "/home/user/.config/lutris/games"},
		{"EA App", "<EA-App-folder>/Game", Windows, "C:\\Program Files\\EA Games/Game"},
		{"Bottles", "<Bottles-folder>/Game", Linux, "/home/user/.var/app/com.usebottles.bottles/data/bottles/Game"},
		{"Prism", "<Prism-folder>/instances", Linux, "/home/user/.local/share/PrismLauncher/instances"},
		{"Flatpak Steam", "<Flatpak-Steam-folder>/steamapps", Linux, "/home/user/.var/app/com.valvesoftware.Steam/data/Steam/steamapps"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.expandOne(tt.template, tt.os)
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExpandOne_EmptyPlaceholderSkipped(t *testing.T) {
	r := &Resolver{Home: "/home/user"}
	got := r.expandOne("%PROGRAMDATA%\\save", Windows)
	if got != "%PROGRAMDATA%\\save" {
		t.Fatalf("empty value should leave placeholder, got %q", got)
	}
}

func TestExpandOne_SeparatorNormalization(t *testing.T) {
	r := testResolver()

	t.Run("windows converts slashes", func(t *testing.T) {
		got := r.expandOne("%USERPROFILE%/Documents/save", Windows)
		want := filepath.FromSlash("/home/user/Documents/save")
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("linux keeps forward slashes", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Linux path test skipped on Windows")
		}
		got := r.expandOne("%USERPROFILE%/Documents/save", Linux)
		if got != "/home/user/Documents/save" {
			t.Fatalf("got %q", got)
		}
	})
}

func TestResolveAll_SteamMultiLibrary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Linux Steam path tests skipped on Windows")
	}
	r := testResolver()
	template := "<SteamLibrary-folder>/steamapps/common/Game/saves"

	got := r.ResolveAll(template, Linux)
	want := []string{
		"/steam/main/steamapps/common/Game/saves",
		"/steam/secondary/steamapps/common/Game/saves",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestResolveAll_SteamDedup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Linux Steam path tests skipped on Windows")
	}
	r := testResolver()
	r.SteamLibraries = []string{"/steam/main", "/steam/main", "/steam/other"}
	template := "<SteamLibrary-folder>/saves"

	got := r.ResolveAll(template, Linux)
	want := []string{
		"/steam/main/saves",
		"/steam/other/saves",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestResolveAll_EmptyTemplate(t *testing.T) {
	r := testResolver()
	if got := r.ResolveAll("  ", Linux); got != nil {
		t.Fatalf("got %v, want nil", got)
	}
	if got := r.ResolveAll("", Linux); got != nil {
		t.Fatalf("got %v, want nil", got)
	}
}

func TestResolveAll_NoSteamPlaceholder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Linux path tests skipped on Windows")
	}
	r := testResolver()
	got := r.ResolveAll("%USERPROFILE%/saves", Linux)
	want := []string{"/home/user/saves"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestResolveAll_SkipsEmptySteamRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Linux path tests skipped on Windows")
	}
	r := testResolver()
	r.SteamLibraries = []string{"", "/steam/valid"}
	got := r.ResolveAll("<SteamLibrary-folder>/saves", Linux)
	want := []string{"/steam/valid/saves"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestResolve_DelegatesToResolveAll(t *testing.T) {
	r := testResolver()
	got := r.Resolve("%USERPROFILE%/saves", Linux)
	want := r.ResolveAll("%USERPROFILE%/saves", Linux)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Resolve %v != ResolveAll %v", got, want)
	}
}

func TestNewResolver(t *testing.T) {
	r := NewResolver()
	if r.Home == "" {
		t.Fatal("Home should be set")
	}
	if r.LocalAppData == "" || r.AppData == "" {
		t.Fatal("LocalAppData and AppData should be set")
	}
	if len(r.SteamLibraries) == 0 {
		t.Fatal("expected default Steam library roots")
	}
	if r.Heroic != filepath.Join(r.AppData, "heroic") {
		t.Fatalf("Heroic %q", r.Heroic)
	}
	if r.Lutris != filepath.Join(r.AppData, "lutris") {
		t.Fatalf("Lutris %q", r.Lutris)
	}
}

func TestGetSteamLibraryRoots(t *testing.T) {
	home := t.TempDir()
	got := GetSteamLibraryRoots(home)
	if len(got) == 0 {
		t.Fatal("expected at least one default root")
	}
	found := false
	for _, root := range got {
		if root == home || strings.HasPrefix(root, home+string(filepath.Separator)) {
			found = true
			break
		}
	}
	if !found && runtime.GOOS != "windows" {
		want := filepath.Join(home, ".steam", "steam")
		for _, root := range got {
			if root == want {
				found = true
				break
			}
		}
	}
	if !found && runtime.GOOS != "windows" {
		t.Fatalf("expected linux default under home, got %v", got)
	}
}

func TestGetSteamLibraryRoots_WithVDF(t *testing.T) {
	root := t.TempDir()
	extraLib := filepath.Join(root, "ExtraLibrary")
	if err := os.MkdirAll(extraLib, 0755); err != nil {
		t.Fatal(err)
	}
	vdfContent := `"libraryfolders"
{
	"1"
	{
		"path"		"` + filepath.ToSlash(extraLib) + `"
	}
}
`
	steamapps := filepath.Join(root, "steamapps")
	if err := os.MkdirAll(steamapps, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(steamapps, libraryfoldersVDFName), []byte(vdfContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Inject our temp root as the only seed library.
	got := appendSteamLibrariesFromVDF([]string{root})
	foundExtra := false
	for _, lib := range got {
		if filepath.FromSlash(lib) == extraLib {
			foundExtra = true
		}
	}
	if !foundExtra {
		t.Fatalf("expected VDF extra library in %v", got)
	}
}

func TestExpandOne_SteamLibraryExistingRoot(t *testing.T) {
	root := t.TempDir()
	r := &Resolver{SteamLibraries: []string{root}}
	got := r.expandOne("<SteamLibrary-folder>/steamapps/saves", Linux)
	want := filepath.Join(root, "steamapps", "saves")
	// Normalize both to forward slashes for a platform-independent comparison.
	if filepath.ToSlash(got) != filepath.ToSlash(want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestPathExists(t *testing.T) {
	dir := t.TempDir()
	savePath := filepath.Join(dir, "save.sav")
	if !PathExists(savePath) {
		t.Fatal("parent dir exists")
	}
	if PathExists(filepath.Join(dir, "missing", "save.sav")) {
		t.Fatal("missing parent should be false")
	}
}

func TestEnsureDir(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "nested", "dir", "save.sav")
	if err := EnsureDir(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Dir(path))
	if err != nil || !info.IsDir() {
		t.Fatal("expected parent dir created")
	}
}

func TestCurrentOS(t *testing.T) {
	got := CurrentOS()
	if runtime.GOOS == "windows" && got != Windows {
		t.Fatalf("got %q, want windows", got)
	}
	if runtime.GOOS != "windows" && got != Linux {
		t.Fatalf("got %q, want linux", got)
	}
}

func TestExpandProgramData(t *testing.T) {
	r := &Resolver{
		Home:         "/home/user",
		LocalAppData: "/home/user/.local/share",
		AppData:      "/home/user/.config",
		ProgramData:  "C:\\ProgramData",
		ProgramFiles: "C:\\Program Files",
	}
	got := r.expandOne("%PROGRAMDATA%\\Game\\save", Windows)
	if got != "C:\\ProgramData\\Game\\save" {
		t.Fatalf("got %q", got)
	}
}

func TestExpandHeroic(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Linux path tests skipped on Windows")
	}
	r := &Resolver{
		Heroic: "/home/user/.config/heroic",
	}
	got := r.expandOne("<Heroic-folder>/Games", Linux)
	if got != "/home/user/.config/heroic/Games" {
		t.Fatalf("got %q", got)
	}
}
