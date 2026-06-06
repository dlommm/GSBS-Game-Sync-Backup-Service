package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	clientsync "github.com/gsbs/gsbs/client/sync"
	"github.com/gsbs/gsbs/pkg/paths"
	"github.com/gsbs/gsbs/pkg/saverule"
)

// runDebugSync prints resolved watch paths and optionally pushes save files for one game.
func runDebugSync(gameID string, dryRun bool) {
	gameID = strings.TrimSpace(gameID)
	if gameID == "" {
		fmt.Fprintln(os.Stderr, "usage: gsbs-client debug-sync <game_id> [--dry-run]")
		os.Exit(1)
	}

	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(1)
	}
	if cfg.ServerURL == "" {
		fmt.Fprintln(os.Stderr, "server_url not set. Run 'gsbs-client login' or set server_url in config.")
		os.Exit(1)
	}

	resolver := configureResolverFromConfig(cfg)
	currentOS := paths.CurrentOS()
	discCache := loadDiscoveryCache()
	installRoots := BuildInstallRootsByGame(cfg, discCache)

	ctx := context.Background()
	manifestEntries := LoadManifestFromDisk()
	manifestInclude := cfg.ManifestInclude
	if manifestInclude == "" {
		manifestInclude = "both"
	}
	includeConfig := manifestInclude == "both" || manifestInclude == "config"
	if entries, err := FetchManifest(ctx, cfg.ServerURL, cfg.Token, "", manifestInclude); err == nil {
		manifestEntries = entries
	} else if len(manifestEntries) == 0 {
		fmt.Fprintln(os.Stderr, "fetch manifest:", err)
		os.Exit(1)
	}

	activeIDs := activeGameIDSet()
	watchMode := cfg.effectiveAutoWatchMode()
	manifestWP, stats := ManifestToWatchPaths(manifestEntries, resolver, currentOS, includeConfig, activeIDs, watchMode, installRoots)
	allWP := mergeWatchPaths(manifestWP, cfg.WatchPaths)

	var gameWP []watchPath
	for _, w := range allWP {
		if w.GameID == gameID {
			gameWP = append(gameWP, w)
		}
	}

	readiness := DiagnoseGameSync(gameID, manifestEntries, resolver, currentOS, includeConfig, installRoots)
	if isGameDisabled(gameID) {
		readiness.Reason = SyncReasonDisabled
	}

	fmt.Printf("Debug sync for game_id=%s (dry_run=%v)\n", gameID, dryRun)
	fmt.Printf("Sync readiness: %s (%s)\n", readiness.Reason, readiness.Reason.Friendly())
	fmt.Printf("  resolved dirs=%d, existing dirs=%d\n", len(readiness.ResolvedDirs), len(readiness.ExistingDirs))
	for _, d := range readiness.ResolvedDirs {
		exists := paths.WatchDirExists(d)
		fmt.Printf("  - %s (exists=%v)\n", d, exists)
	}
	fmt.Printf("Watch path stats: discovered_skip=%d platform_skip=%d missing_dir=%d malformed=%d\n",
		stats.SkippedDiscovered, stats.SkippedPlatform, stats.SkippedMissingDir, stats.SkippedMalformed)
	if len(cfg.WatchExclude) > 0 {
		fmt.Printf("Exclude patterns: %s\n", strings.Join(cfg.WatchExclude, ", "))
	}

	if len(gameWP) == 0 {
		fmt.Printf("No watch paths resolved for this game (reason: %s).\n", readiness.Reason.Friendly())
		switch readiness.Reason {
		case SyncReasonNoManifest:
			fmt.Println("Hint: this game's save locations are not in the server manifest. Add it via the tray 'Add a game…' page, or run the PCGW sync on the server.")
		case SyncReasonWrongPlatform:
			fmt.Println("Hint: the manifest only has save locations for a different OS.")
		case SyncReasonSaveDirMissing:
			fmt.Println("Hint: the save folder does not exist yet. Launch the game once, or set the install path in config (game_install_paths).")
		case SyncReasonDisabled:
			fmt.Println("Hint: sync is disabled for this game. Re-enable it under tray 'Discovered games'.")
		}
		os.Exit(1)
	}

	type pushTarget struct {
		GameID       string
		PathKey      string
		RuleKey      string
		RelativePath string
		AbsPath      string
		Bytes        int64
	}
	var targets []pushTarget

	for _, wp := range gameWP {
		ruleKey := wp.RuleKey
		if ruleKey == "" {
			ruleKey = wp.PathKey
		}
		fmt.Printf("\nWatch rule path_key=%s directory=%q patterns=%v recursive=%v sync_all=%v\n",
			wp.PathKey, wp.Directory, wp.IncludePatterns, wp.Recursive, wp.SyncAll)
		for _, dirTemplate := range watchRootDirs(wp) {
			for _, root := range resolveManifestTemplate(resolver, dirTemplate, currentOS, gameID, installRoots) {
				if root == "" || !paths.WatchDirExists(root) {
					fmt.Printf("  (skip unresolved/missing: %q)\n", dirTemplate)
					continue
				}
				root = filepath.Clean(root)
				fmt.Printf("  watching: %s\n", root)
				syncAll := syncAllForWatchPath(wp)
				_ = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
					if walkErr != nil || d.IsDir() {
						return nil
					}
					rel, err := filepath.Rel(root, path)
					if err != nil {
						return nil
					}
					rel = filepath.ToSlash(rel)
					if !saverule.MatchInclude(rel, wp.IncludePatterns, syncAll) {
						return nil
					}
					if debugExcludeMatch(cfg.WatchExclude, path, rel) {
						return nil
					}
					info, err := os.Stat(path)
					if err != nil || info.Size() == 0 {
						return nil
					}
					pk := pathKeyForRelative(ruleKey, rel, wp.IncludePatterns, syncAll)
					targets = append(targets, pushTarget{
						GameID:       gameID,
						PathKey:      pk,
						RuleKey:      ruleKey,
						RelativePath: rel,
						AbsPath:      path,
						Bytes:        info.Size(),
					})
					if !wp.Recursive {
						return filepath.SkipAll
					}
					return nil
				})
			}
		}
	}

	if len(targets) == 0 {
		fmt.Println("No matching save files found under resolved watch directories.")
		os.Exit(1)
	}

	fmt.Printf("\nResolved %d file(s) to push:\n", len(targets))
	for _, t := range targets {
		fmt.Printf("  path_key=%s relative_path=%s bytes=%d\n    %s\n", t.PathKey, t.RelativePath, t.Bytes, t.AbsPath)
	}

	if dryRun {
		fmt.Println("\nDry-run: no uploads performed.")
		if cfg.Token == "" {
			fmt.Println("(token not set — login required for live push)")
		}
		return
	}
	if strings.TrimSpace(cfg.Token) == "" {
		fmt.Fprintln(os.Stderr, "token not set — run 'gsbs-client login' first")
		os.Exit(1)
	}

	client, err := clientsync.NewClient(cfg.ServerURL, cfg.Token, resolver, currentOS, cfg.MaxSyncKbps, cfg.UseCompression, true)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sync client:", err)
		os.Exit(1)
	}
	if enc, err := client.FetchAccountSettings(ctx); err == nil {
		client.SetEncryption(enc, cfg.EncryptionPassphrase)
	}

	fmt.Println("\nPushing...")
	var ok, fail int
	for _, t := range targets {
		content, err := os.ReadFile(t.AbsPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  read %s: %v\n", t.AbsPath, err)
			fail++
			continue
		}
		if err := client.Push(ctx, t.GameID, t.PathKey, t.AbsPath, t.RelativePath, content); err != nil {
			fmt.Fprintf(os.Stderr, "  push %s: %v\n", t.RelativePath, err)
			fail++
			continue
		}
		fmt.Printf("  uploaded %s (%d bytes)\n", t.RelativePath, len(content))
		ok++
	}
	fmt.Printf("\nDone: %d uploaded, %d failed\n", ok, fail)
	if fail > 0 {
		os.Exit(1)
	}
}

func debugExcludeMatch(patterns []string, filePath, relPath string) bool {
	if len(patterns) == 0 {
		return false
	}
	base := filepath.Base(filePath)
	rel := filepath.ToSlash(relPath)
	for _, pat := range patterns {
		pat = strings.TrimSpace(pat)
		if pat == "" {
			continue
		}
		if strings.Contains(pat, "/") || strings.Contains(pat, `\`) {
			if ok, err := filepath.Match(pat, rel); err == nil && ok {
				return true
			}
			continue
		}
		if ok, err := filepath.Match(pat, base); err == nil && ok {
			return true
		}
	}
	return false
}
