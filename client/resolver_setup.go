package main

import (
	"github.com/gsbs/gsbs/pkg/launchers"
	"github.com/gsbs/gsbs/pkg/paths"
)

// applyLauncherDetection merges auto-detected launcher paths into resolver and config (empty fields only).
func applyLauncherDetection(cfg *config, resolver *paths.Resolver) {
	detected := launchers.DetectPaths()
	detected.ApplyToResolver(resolver)
	if cfg == nil {
		return
	}
	merged := false
	if detected.UbisoftConnect != "" && cfg.UbisoftConnectFolder == "" {
		cfg.UbisoftConnectFolder = detected.UbisoftConnect
		merged = true
	}
	if detected.GOGGalaxy != "" && cfg.GOGGalaxyFolder == "" {
		cfg.GOGGalaxyFolder = detected.GOGGalaxy
		merged = true
	}
	if detected.EpicGames != "" && cfg.EpicGamesFolder == "" {
		cfg.EpicGamesFolder = detected.EpicGames
		merged = true
	}
	if detected.XboxApp != "" && cfg.XboxAppFolder == "" {
		cfg.XboxAppFolder = detected.XboxApp
		merged = true
	}
	if detected.Heroic != "" && cfg.HeroicFolder == "" {
		cfg.HeroicFolder = detected.Heroic
		merged = true
	}
	if detected.Lutris != "" && cfg.LutrisFolder == "" {
		cfg.LutrisFolder = detected.Lutris
		merged = true
	}
	if detected.EAApp != "" && cfg.EAAppFolder == "" {
		cfg.EAAppFolder = detected.EAApp
		merged = true
	}
	if merged {
		_ = saveConfig(cfg)
	}
}

func configureResolverFromConfig(cfg *config) *paths.Resolver {
	resolver := paths.NewResolver()
	if cfg.UbisoftConnectFolder != "" {
		resolver.UbisoftConnect = cfg.UbisoftConnectFolder
	}
	if cfg.GOGGalaxyFolder != "" {
		resolver.GOGGalaxy = cfg.GOGGalaxyFolder
	}
	if cfg.EpicGamesFolder != "" {
		resolver.EpicGames = cfg.EpicGamesFolder
	}
	if cfg.XboxAppFolder != "" {
		resolver.XboxApp = cfg.XboxAppFolder
	}
	if cfg.LauncherUserID != "" {
		resolver.UserID = cfg.LauncherUserID
	}
	if cfg.HeroicFolder != "" {
		resolver.Heroic = cfg.HeroicFolder
	}
	if cfg.LutrisFolder != "" {
		resolver.Lutris = cfg.LutrisFolder
	}
	if cfg.EAAppFolder != "" {
		resolver.EAApp = cfg.EAAppFolder
	}
	applyLauncherDetection(cfg, resolver)
	resolver.InstalledSteam = discoveryState.InstalledSteam
	return resolver
}

func refreshResolver(cfg *config, r *paths.Resolver) {
	if r == nil {
		return
	}
	updated := configureResolverFromConfig(cfg)
	*r = *updated
}

func buildPullContext(cfg *config) paths.PullContext {
	legacy := cfg.effectiveAutoWatchMode() == "legacy"
	active := activeGameIDSet()
	if legacy {
		active = nil
	}
	return paths.PullContext{
		LegacyMode:         legacy,
		InstalledGameIDs:   active,
		InstalledSteamApps: discoveryState.InstalledSteam,
	}
}
