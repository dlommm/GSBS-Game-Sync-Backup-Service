package launchers

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/gsbs/gsbs/pkg/paths"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	}
	return home
}

func TestDetectPaths_UnixLaunchers(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix launcher paths")
	}
	home := setupHome(t)

	require.NoError(t, os.MkdirAll(filepath.Join(home, ".config", "heroic"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".config", "lutris"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".local", "share", "Epic"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".local", "share", "PrismLauncher"), 0755))
	require.NoError(t, os.Mkdir(filepath.Join(home, "GOG Games"), 0755))

	got := DetectPaths()
	assert.Equal(t, filepath.Join(home, ".config", "heroic"), got.Heroic)
	assert.Equal(t, filepath.Join(home, ".config", "lutris"), got.Lutris)
	assert.Equal(t, filepath.Join(home, ".local", "share", "Epic"), got.EpicGames)
	assert.Equal(t, filepath.Join(home, ".local", "share", "PrismLauncher"), got.Prism)
	assert.Equal(t, filepath.Join(home, "GOG Games"), got.GOGGalaxy)
}

func TestDetectPaths_FlatpakPaths(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("flatpak paths are linux-only")
	}
	home := setupHome(t)

	flatpakSteam := filepath.Join(home, ".var", "app", "com.valvesoftware.Steam", "data", "Steam")
	heroicFlatpak := filepath.Join(home, ".var", "app", "com.heroicgameslauncher.hgl", "config", "heroic")
	bottles := filepath.Join(home, ".var", "app", "com.usebottles.bottles", "data", "bottles")
	require.NoError(t, os.MkdirAll(flatpakSteam, 0755))
	require.NoError(t, os.MkdirAll(heroicFlatpak, 0755))
	require.NoError(t, os.MkdirAll(bottles, 0755))

	got := DetectPaths()
	assert.Equal(t, flatpakSteam, got.FlatpakSteam)
	assert.Contains(t, got.SteamLibraries, flatpakSteam)
	assert.Equal(t, heroicFlatpak, got.Heroic)
	assert.Equal(t, bottles, got.Bottles)
}

func TestDetectPaths_WindowsLaunchers(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows launcher paths")
	}
	home := setupHome(t)
	pf := filepath.Join(home, "Program Files")
	pf86 := filepath.Join(home, "Program Files (x86)")
	localAppData := filepath.Join(home, "AppData", "Local")

	t.Setenv("ProgramFiles", pf)
	t.Setenv("ProgramFiles(x86)", pf86)
	t.Setenv("LOCALAPPDATA", localAppData)

	require.NoError(t, os.MkdirAll(filepath.Join(pf86, "Ubisoft", "Ubisoft Game Launcher"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(pf86, "GOG Galaxy"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(pf, "Epic Games"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(pf, "Electronic Arts", "EA Desktop"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(localAppData, "Packages"), 0755))

	got := DetectPaths()
	assert.Equal(t, filepath.Join(pf86, "Ubisoft", "Ubisoft Game Launcher"), got.UbisoftConnect)
	assert.Equal(t, filepath.Join(pf86, "GOG Galaxy"), got.GOGGalaxy)
	assert.Equal(t, filepath.Join(pf, "Epic Games"), got.EpicGames)
	assert.Equal(t, filepath.Join(pf, "Electronic Arts", "EA Desktop"), got.EAApp)
	assert.Equal(t, filepath.Join(localAppData, "Packages"), got.XboxApp)
}

func TestDetectPaths_MissingPaths(t *testing.T) {
	home := setupHome(t)
	_ = home

	got := DetectPaths()
	assert.Empty(t, got.UbisoftConnect)
	assert.Empty(t, got.GOGGalaxy)
	assert.Empty(t, got.EpicGames)
	assert.Empty(t, got.Heroic)
	assert.Empty(t, got.Lutris)
	assert.Empty(t, got.Bottles)
	assert.Empty(t, got.Prism)
	assert.Empty(t, got.FlatpakSteam)
}

func TestApplyToResolver(t *testing.T) {
	d := DetectedPaths{
		SteamLibraries: []string{"/steam/a", "/steam/b"},
		UbisoftConnect: "/ubisoft",
		GOGGalaxy:      "/gog",
	}
	r := &paths.Resolver{}
	d.ApplyToResolver(r)
	assert.Equal(t, []string{"/steam/a", "/steam/b"}, r.SteamLibraries)
	assert.Equal(t, "/ubisoft", r.UbisoftConnect)
	assert.Equal(t, "/gog", r.GOGGalaxy)

	r.UbisoftConnect = "/existing"
	d.ApplyToResolver(r)
	assert.Equal(t, "/existing", r.UbisoftConnect)
}
