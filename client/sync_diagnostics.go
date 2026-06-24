package main

import (
	"strings"

	clientlogx "github.com/gsbs/gsbs/client/logx"
	"github.com/gsbs/gsbs/pkg/paths"
	"github.com/gsbs/gsbs/pkg/types"
)

// SyncReason classifies why a game is or is not ready to sync its saves.
type SyncReason string

const (
	SyncReasonReady          SyncReason = "ready"
	SyncReasonNoManifest     SyncReason = "no_manifest_entry"
	SyncReasonWrongPlatform  SyncReason = "wrong_platform"
	SyncReasonSaveDirMissing SyncReason = "save_dir_missing"
	SyncReasonMalformedRules SyncReason = "malformed_rules"
	SyncReasonDisabled       SyncReason = "disabled"
)

// Friendly returns a short human-readable explanation for a sync reason.
func (r SyncReason) Friendly() string {
	switch r {
	case SyncReasonReady:
		return "ready to sync"
	case SyncReasonNoManifest:
		return "not in server manifest"
	case SyncReasonWrongPlatform:
		return "no saves for this OS"
	case SyncReasonSaveDirMissing:
		return "save folder not found"
	case SyncReasonMalformedRules:
		return "no valid save rule"
	case SyncReasonDisabled:
		return "sync disabled"
	default:
		return string(r)
	}
}

// SyncReadiness is the diagnostic result for one game.
type SyncReadiness struct {
	GameID       string
	Reason       SyncReason
	ResolvedDirs []string // directories the templates resolved to (whether or not present)
	ExistingDirs []string // resolved directories that exist on disk
}

// DiagnoseGameSync classifies a single game's sync readiness against the manifest
// and resolver. It mirrors the gating in ManifestToWatchPaths so the reason
// matches why a watch path would (or would not) be produced.
func DiagnoseGameSync(gameID string, entries []types.GameSaveLocation, resolver *paths.Resolver, currentOS paths.OS, includeConfig bool, installRootsByGame map[string][]string) SyncReadiness {
	res := SyncReadiness{GameID: gameID, Reason: SyncReasonNoManifest}
	if strings.TrimSpace(gameID) == "" {
		return res
	}
	var hasAny, hasPlatform, hasValidRules bool
	for _, e := range entries {
		if e.GameID != gameID {
			continue
		}
		hasAny = true
		// Mirror ManifestToWatchPaths: on Linux a Windows-platform rule for a
		// Steam game is a Proton candidate (resolved via compatdata).
		protonCandidate := currentOS == paths.Linux && e.Platform == string(paths.Windows) && len(e.SteamAppIDs) > 0
		if e.Platform != string(currentOS) && !protonCandidate {
			continue
		}
		hasPlatform = true
		if e.IsConfig && !includeConfig {
			continue
		}
		rules := saveRulesForEntry(e)
		if len(rules) == 0 {
			continue
		}
		hasValidRules = true
		for _, rule := range rules {
			var resolved []string
			if protonCandidate {
				resolved = resolveProtonPaths(resolver, rule, e.SteamAppIDs)
			} else {
				resolved = resolveManifestTemplate(resolver, rule.Directory, currentOS, gameID, installRootsByGame)
			}
			for _, abs := range resolved {
				if abs == "" || resolver.UnsafeWatchTarget(abs, rule.SyncAll, rule.Recursive, rule.IncludePatterns) {
					continue
				}
				res.ResolvedDirs = append(res.ResolvedDirs, abs)
				if paths.WatchDirExists(abs) {
					res.ExistingDirs = append(res.ExistingDirs, abs)
				}
			}
		}
	}
	switch {
	case !hasAny:
		res.Reason = SyncReasonNoManifest
	case !hasPlatform:
		res.Reason = SyncReasonWrongPlatform
	case !hasValidRules:
		res.Reason = SyncReasonMalformedRules
	case len(res.ExistingDirs) > 0:
		res.Reason = SyncReasonReady
	default:
		res.Reason = SyncReasonSaveDirMissing
	}
	return res
}

// diagnoseGamesReadiness computes sync readiness for the given manifest game IDs,
// loading config, resolver, manifest, and install roots once. Disabled games are
// reported as SyncReasonDisabled regardless of path state.
func diagnoseGamesReadiness(ids []string) map[string]SyncReadiness {
	out := make(map[string]SyncReadiness, len(ids))
	if len(ids) == 0 {
		return out
	}
	cfg, _ := loadConfig()
	if cfg == nil {
		cfg = blankConfig()
	}
	resolver := configureResolverFromConfig(cfg)
	currentOS := paths.CurrentOS()
	entries := LoadManifestFromDisk()
	installRoots := BuildInstallRootsByGame(cfg, loadDiscoveryCache())
	includeConfig := manifestIncludesConfig(cfg)
	for _, id := range ids {
		r := DiagnoseGameSync(id, entries, resolver, currentOS, includeConfig, installRoots)
		if isGameDisabled(id) {
			r.Reason = SyncReasonDisabled
		}
		out[id] = r
	}
	return out
}

// logActiveGamesReadiness logs a per-game sync-readiness reason for each active
// game. Useful when zero watch paths were built so the user can see exactly why
// each discovered game is not syncing (e.g. save folder missing).
func logActiveGamesReadiness(activeIDs map[string]bool) {
	if len(activeIDs) == 0 {
		return
	}
	ids := make([]string, 0, len(activeIDs))
	for id := range activeIDs {
		ids = append(ids, id)
	}
	for id, rd := range diagnoseGamesReadiness(ids) {
		clientlogx.Event("game_sync_readiness",
			"game_id", id,
			"reason", string(rd.Reason),
			"resolved_dirs", len(rd.ResolvedDirs),
			"existing_dirs", len(rd.ExistingDirs))
	}
}

// manifestIncludesConfig reports whether config-type entries should be considered.
func manifestIncludesConfig(cfg *config) bool {
	include := cfg.ManifestInclude
	if include == "" {
		include = "both"
	}
	return include == "both" || include == "config"
}
