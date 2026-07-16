//go:build darwin

package launchers

import (
	"os"
	"path/filepath"
	"testing"
)

// macOS previously ran the Linux detection branch and probed XDG/.var paths
// that never exist on a Mac — "Detect launcher paths" found only Steam.
func TestDetectPathsDarwinBranch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	appSupport := filepath.Join(home, "Library", "Application Support")
	for _, dir := range []string{
		filepath.Join(appSupport, "Epic"),
		filepath.Join(appSupport, "heroic"),
		filepath.Join(home, "GOG Games"),
	} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}

	d := DetectPaths()
	if d.EpicGames != filepath.Join(appSupport, "Epic") {
		t.Errorf("EpicGames = %q, want the darwin Application Support path", d.EpicGames)
	}
	if d.Heroic != filepath.Join(appSupport, "heroic") {
		t.Errorf("Heroic = %q, want the darwin Application Support path", d.Heroic)
	}
	if d.GOGGalaxy != filepath.Join(home, "GOG Games") {
		t.Errorf("GOGGalaxy = %q, want ~/GOG Games", d.GOGGalaxy)
	}
	// Linux-only launchers must stay empty on darwin.
	if d.Lutris != "" || d.Bottles != "" || d.Prism != "" || d.FlatpakSteam != "" {
		t.Errorf("linux-only launchers detected on darwin: lutris=%q bottles=%q prism=%q flatpakSteam=%q",
			d.Lutris, d.Bottles, d.Prism, d.FlatpakSteam)
	}
}
