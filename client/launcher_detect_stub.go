//go:build !windows

package main

import (
	"github.com/gsbs/gsbs/pkg/launchers"
)

// DetectLauncherPaths returns suggested launcher install paths for the current machine.
func DetectLauncherPaths() DetectedLauncherPaths {
	d := launchers.DetectPaths()
	return DetectedLauncherPaths{
		GOGGalaxy:          d.GOGGalaxy,
		EpicGames:          d.EpicGames,
		HeroicFolder:       d.Heroic,
		LutrisFolder:       d.Lutris,
		EAAppFolder:        d.EAApp,
		BottlesFolder:      d.Bottles,
		PrismFolder:        d.Prism,
		FlatpakSteamFolder: d.FlatpakSteam,
	}
}
