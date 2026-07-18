package paths

import (
	"path/filepath"
	"testing"
)

func TestUnsafeWatchDir(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home", "user")
	r := &Resolver{
		Home:         home,
		LocalAppData: filepath.Join(home, ".local", "share"),
		AppData:      filepath.Join(home, ".config"),
		XDGCacheHome: filepath.Join(home, ".cache"),
	}

	unsafe := []string{
		"",                                  // empty
		"relative/path",                     // not absolute
		home,                                // the home dir itself
		filepath.Dir(home),                  // ancestor of home (/.../home)
		filepath.Join(home, ".config"),      // XDG config root
		filepath.Join(home, ".local"),       // ~/.local
		filepath.Join(home, ".local/share"), // XDG data root
		filepath.Join(home, ".cache"),       // XDG cache root
		filepath.Join(home, ".var", "app"),  // Flatpak per-app root
		filepath.Join(home, ".steam"),       // all of Steam
		filepath.Join(home, "Documents"),
		filepath.Join(home, "Documents", "My Games"),
		filepath.Join(home, "Library"),                // macOS
		filepath.Join(home, "Library", "Preferences"), // macOS: system+app plists
		filepath.Join(home, "Library", "Application Support"), // macOS app data root
		filepath.Join(home, "Library", "Containers"),
		filepath.Join(home, "Library", "Caches"),
	}
	for _, d := range unsafe {
		if !r.UnsafeWatchDir(d) {
			t.Errorf("expected UNSAFE, got safe: %q", d)
		}
	}

	safe := []string{
		filepath.Join(home, ".local", "share", "MyGame"),
		filepath.Join(home, ".config", "MyGame"),
		filepath.Join(home, ".tesseract"), // a hidden game dir directly in home is fine
		filepath.Join(home, "Documents", "My Games", "SomeGame"),
		filepath.Join(home, ".steam", "steam", "steamapps", "compatdata", "1057090", "pfx", "drive_c"),
		filepath.Join(home, "Library", "Application Support", "MyGame"),
		filepath.Join(home, "Library", "Preferences", "unity3d", "MyGame"),
	}
	for _, d := range safe {
		if r.UnsafeWatchDir(d) {
			t.Errorf("expected SAFE, got unsafe: %q", d)
		}
	}
}

func TestUnsafeWatchTarget(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home", "user")
	r := &Resolver{
		Home:         home,
		LocalAppData: filepath.Join(home, ".local", "share"),
		AppData:      filepath.Join(home, ".config"),
		XDGCacheHome: filepath.Join(home, ".cache"),
	}
	gameDir := filepath.Join(home, ".local", "share", "MyGame")

	cases := []struct {
		name      string
		dir       string
		syncAll   bool
		recursive bool
		patterns  []string
		want      bool
	}{
		{"specific subfolder syncAll", gameDir, true, true, nil, false},
		{"home syncAll", home, true, false, nil, true},
		{"home recursive", home, false, true, nil, true},
		{"home no patterns", home, false, false, nil, true},
		{"home named file", home, false, false, []string{"savegame.dat"}, false},
		{"home two named files", home, false, false, []string{".gamerc", "profile.sav"}, false},
		{"home star", home, false, false, []string{"*"}, true},
		{"home star-dot-star", home, false, false, []string{"*.*"}, true},
		{"home recursive glob", home, false, false, []string{"**/*.sav"}, true},
		{"home path pattern", home, false, false, []string{"sub/file.sav"}, true},
	}
	for _, c := range cases {
		if got := r.UnsafeWatchTarget(c.dir, c.syncAll, c.recursive, c.patterns); got != c.want {
			t.Errorf("%s: UnsafeWatchTarget=%v want %v", c.name, got, c.want)
		}
	}
}
