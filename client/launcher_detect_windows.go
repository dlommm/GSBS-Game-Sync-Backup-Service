//go:build windows

package main

import (
	"github.com/gsbs/gsbs/pkg/launchers"
)

// DetectLauncherPaths returns suggested launcher install paths for the current machine.
func DetectLauncherPaths() DetectedLauncherPaths {
	d := launchers.DetectPaths()
	return DetectedLauncherPaths{
		UbisoftConnect: d.UbisoftConnect,
		GOGGalaxy:      d.GOGGalaxy,
		EpicGames:      d.EpicGames,
		XboxApp:        d.XboxApp,
		EAAppFolder:    d.EAApp,
	}
}
