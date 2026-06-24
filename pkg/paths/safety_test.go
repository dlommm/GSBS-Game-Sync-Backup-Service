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
	}
	for _, d := range safe {
		if r.UnsafeWatchDir(d) {
			t.Errorf("expected SAFE, got unsafe: %q", d)
		}
	}
}
