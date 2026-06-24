package main

import (
	"sync"

	"github.com/gsbs/gsbs/pkg/launchers"
	"github.com/gsbs/gsbs/pkg/paths"
)

var (
	baseResolverOnce sync.Once
	baseResolver     *paths.Resolver
)

// unsafeWatchTargetAbs reports whether an absolute, already-saved watch path is
// too broad to sync given its rule shape (a home/XDG/system root watched
// recursively or with sync-all). Uses an env-based resolver, so it does not
// depend on per-config launcher overrides.
func unsafeWatchTargetAbs(dir string, syncAll, recursive bool, patterns []string) bool {
	baseResolverOnce.Do(func() { baseResolver = paths.NewResolver() })
	return baseResolver.UnsafeWatchTarget(dir, syncAll, recursive, patterns)
}

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
	if detected.Bottles != "" && cfg.BottlesFolder == "" {
		cfg.BottlesFolder = detected.Bottles
		merged = true
	}
	if detected.Prism != "" && cfg.PrismFolder == "" {
		cfg.PrismFolder = detected.Prism
		merged = true
	}
	if detected.FlatpakSteam != "" && cfg.FlatpakSteamFolder == "" {
		cfg.FlatpakSteamFolder = detected.FlatpakSteam
		merged = true
	}
	if detected.SteamUserID != "" && cfg.LauncherUserID == "" {
		cfg.LauncherUserID = detected.SteamUserID
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
	if cfg.BottlesFolder != "" {
		resolver.Bottles = cfg.BottlesFolder
	}
	if cfg.PrismFolder != "" {
		resolver.Prism = cfg.PrismFolder
	}
	if cfg.FlatpakSteamFolder != "" {
		resolver.FlatpakSteam = cfg.FlatpakSteamFolder
	}
	if len(cfg.SteamLibraryFolders) > 0 {
		resolver.SteamLibraries = paths.MergeSteamLibraries(resolver.SteamLibraries, cfg.SteamLibraryFolders)
	}
	applyLauncherDetection(cfg, resolver)
	discoveryMu.RLock()
	resolver.InstalledSteam = discoveryState.InstalledSteam
	discoveryMu.RUnlock()
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
	discoveryMu.RLock()
	installedSteam := discoveryState.InstalledSteam
	discoveryMu.RUnlock()
	return paths.PullContext{
		LegacyMode:         legacy,
		InstalledGameIDs:   active,
		InstalledSteamApps: installedSteam,
	}
}
