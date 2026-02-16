//go:build windows

package main

import (
	"os"
	"path/filepath"

	"github.com/gsbs/gsbs/pkg/paths"
)

// DetectLauncherPaths returns suggested launcher install paths for the current machine.
// Only includes paths that exist. Windows-only; other platforms return empty struct.
func DetectLauncherPaths() DetectedLauncherPaths {
	var out DetectedLauncherPaths
	r := paths.NewResolver()
	// Ubisoft: common install path (NewResolver does not set it).
	pf86 := os.Getenv("ProgramFiles(x86)")
	if pf86 == "" {
		pf86 = filepath.Join(os.Getenv("ProgramFiles"), "..", "Program Files (x86)")
	}
	ubisoft := filepath.Join(pf86, "Ubisoft", "Ubisoft Game Launcher")
	if _, err := os.Stat(ubisoft); err == nil {
		out.UbisoftConnect = ubisoft
	}
	if r.GOGGalaxy != "" {
		if _, err := os.Stat(r.GOGGalaxy); err == nil {
			out.GOGGalaxy = r.GOGGalaxy
		}
	}
	if r.EpicGames != "" {
		if _, err := os.Stat(r.EpicGames); err == nil {
			out.EpicGames = r.EpicGames
		}
	}
	if r.XboxApp != "" {
		if _, err := os.Stat(r.XboxApp); err == nil {
			out.XboxApp = r.XboxApp
		}
	}
	return out
}
