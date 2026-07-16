package discovery

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

const heroicLibraryJSON = `{"game1": {"title": "Epic Game One", "app_name": "eg1"}}`

// Heroic must be found under every per-OS default root — the old scanner
// hardcoded ~/.config/heroic, so Windows (%APPDATA%\heroic) and Flatpak
// Heroic installs were never discovered.
func TestScanHeroicDefaultRootsPerOS(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	t.Setenv("USERPROFILE", home)

	var root string
	switch runtime.GOOS {
	case "windows":
		root = filepath.Join(home, "AppData", "Roaming", "heroic")
	case "darwin":
		root = filepath.Join(home, "Library", "Application Support", "heroic")
	default:
		// The Flatpak Heroic path — previously known to the launcher
		// detector but ignored by this scanner.
		root = filepath.Join(home, ".var", "app", "com.heroicgameslauncher.hgl", "config", "heroic")
	}
	writeFile(t, filepath.Join(root, "Games", "legendary", "library.json"), heroicLibraryJSON)

	games := scanHeroic(nil)
	if len(games) != 1 || games[0].GameID != "eg1" {
		t.Fatalf("scanHeroic() = %+v, want one game eg1", games)
	}
}

// A user-configured Heroic folder must be honored (it was previously ignored
// entirely by discovery) and must win duplicates against defaults.
func TestScanHeroicConfiguredRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))

	custom := filepath.Join(home, "SomewhereElse", "heroic")
	// The alternate store_cache layout must be read too.
	writeFile(t, filepath.Join(custom, "store_cache", "legendary", "library.json"), heroicLibraryJSON)

	games := scanHeroic([]string{custom})
	if len(games) != 1 || games[0].GameID != "eg1" {
		t.Fatalf("scanHeroic(configured) = %+v, want one game eg1", games)
	}
}

func TestScanLutrisNativeAndFlatpakRoots(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("lutris is linux-only")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeFile(t, filepath.Join(home, ".config", "lutris", "games", "native-game.yml"),
		"slug: native-game\nname: Native Game\n")
	writeFile(t, filepath.Join(home, ".var", "app", "net.lutris.Lutris", "config", "lutris", "games", "flatpak-game.yml"),
		"slug: flatpak-game\nname: Flatpak Game\n")

	games := scanLutris(nil)
	ids := map[string]bool{}
	for _, g := range games {
		ids[g.GameID] = true
	}
	if !ids["native-game"] || !ids["flatpak-game"] {
		t.Fatalf("scanLutris() = %+v, want both native and flatpak games", games)
	}
}

func TestScanBottlesNativeAndFlatpakRoots(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bottles is linux-only")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeFile(t, filepath.Join(home, ".var", "app", "com.usebottles.bottles", "data", "bottles", "bottles", "flatpak-bottle", "bottle.yml"),
		"Name: Flatpak Bottle\n")
	writeFile(t, filepath.Join(home, ".local", "share", "bottles", "bottles", "native-bottle", "bottle.yml"),
		"Name: Native Bottle\n")

	games := scanBottles(nil)
	ids := map[string]bool{}
	for _, g := range games {
		ids[g.GameID] = true
	}
	if !ids["flatpak-bottle"] || !ids["native-bottle"] {
		t.Fatalf("scanBottles() = %+v, want both bottles", games)
	}
}

func TestScanPrismNativeAndFlatpakRoots(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("prism scan roots are unix-shaped")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeFile(t, filepath.Join(home, ".local", "share", "PrismLauncher", "instances", "vanilla", "instance.cfg"),
		"name=Vanilla\n")
	writeFile(t, filepath.Join(home, ".var", "app", "org.prismlauncher.PrismLauncher", "data", "PrismLauncher", "instances", "modded", "instance.cfg"),
		"name=Modded\n")

	games := scanPrism(nil)
	ids := map[string]bool{}
	for _, g := range games {
		ids[g.GameID] = true
	}
	if !ids["vanilla"] || !ids["modded"] {
		t.Fatalf("scanPrism() = %+v, want both instances", games)
	}
}
