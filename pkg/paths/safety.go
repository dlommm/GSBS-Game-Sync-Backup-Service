package paths

import (
	"path/filepath"
	"strings"
)

// UnsafeWatchDir reports whether absDir is too broad to safely watch and sync.
//
// A save folder must be game-specific. If a manifest template (or a manual
// entry) resolves to the home directory or a top-level XDG/system root, watching
// it recursively would sweep up unrelated files — dotfiles (.bashrc, shell
// history), caches, and every other application's data. GSBS must refuse those.
//
// It returns true when absDir is empty/relative, a filesystem or home root, an
// ancestor of the home directory, or one of the well-known top-level roots
// (~/.config, ~/.local/share, ~/.cache, Documents, %APPDATA%, …). A directory
// strictly *below* one of those roots (e.g. ~/.local/share/MyGame) is safe.
func (r *Resolver) UnsafeWatchDir(absDir string) bool {
	absDir = strings.TrimSpace(absDir)
	if absDir == "" || !filepath.IsAbs(absDir) {
		return true
	}
	clean := filepath.Clean(absDir)
	if clean == "." || clean == string(filepath.Separator) {
		return true
	}
	if r.unsafeRoots()[clean] {
		return true
	}
	// Reject any ancestor of (or equal to) the home directory, e.g. "/", "/home".
	if home := filepath.Clean(r.Home); home != "" && home != "." {
		if clean == home || isAncestorDir(clean, home) {
			return true
		}
	}
	return false
}

// unsafeRoots is the set of cleaned directories that are too broad to watch.
// It is derived from the resolver's own roots so it adapts to custom $HOME /
// $XDG_* values (including the Flatpak sandbox, where XDG dirs are redirected).
func (r *Resolver) unsafeRoots() map[string]bool {
	set := make(map[string]bool)
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" || !filepath.IsAbs(p) {
			return
		}
		set[filepath.Clean(p)] = true
	}
	add(string(filepath.Separator))
	add(r.LocalAppData) // ~/.local/share  or  %LOCALAPPDATA%
	add(r.AppData)      // ~/.config       or  %APPDATA%
	add(r.XDGCacheHome) // ~/.cache        or  %LOCALAPPDATA%\cache
	add(r.ProgramData)
	add(r.ProgramFiles)
	if home := r.Home; home != "" {
		add(home)
		add(filepath.Dir(home)) // /home, /Users, C:\Users
		for _, sub := range []string{
			".config", ".local", ".local/share", ".local/state", ".cache",
			".var", ".var/app", ".steam", ".steam/steam",
			"Documents", "Downloads", "Desktop", "Music", "Pictures", "Videos",
			"Documents/My Games", "Saved Games",
			"AppData", "AppData/Local", "AppData/LocalLow", "AppData/Roaming",
		} {
			add(filepath.Join(home, filepath.FromSlash(sub)))
		}
	}
	return set
}

// isAncestorDir reports whether anc is a strict ancestor directory of p.
func isAncestorDir(anc, p string) bool {
	if anc == p {
		return false
	}
	rel, err := filepath.Rel(anc, p)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
