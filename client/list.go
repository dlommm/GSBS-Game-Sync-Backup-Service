package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/gsbs/gsbs/pkg/paths"
)

// runList prints games that can be saved and synced on this machine.
// The server provides a manifest of known game save locations (from the PCGW sync job).
// The client resolves each path for the current OS and only lists games where the save directory exists locally.
// If dryRunPull is true and token is set, also reports what would be downloaded and written by a pull (without performing it).
func runList(dryRunPull bool) {
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
	installRoots := BuildInstallRootsByGame(cfg, loadDiscoveryCache())

	// Load manifest (server or cache)
	ctx := context.Background()
	manifestEntries := LoadManifestFromDisk()
	manifestInclude := cfg.ManifestInclude
	if manifestInclude == "" {
		manifestInclude = "both"
	}
	includeConfig := manifestInclude == "both" || manifestInclude == "config"
	if entries, err := FetchManifest(ctx, cfg.ServerURL, cfg.Token, "", manifestInclude); err == nil {
		manifestEntries = entries
		log.Printf("client list: manifest fetched %d entries", len(entries))
		if err := SaveManifestToDisk(entries); err != nil {
			log.Printf("client list: save manifest cache: %v", err)
			fmt.Fprintln(os.Stderr, "save manifest cache:", err)
		}
	} else {
		log.Printf("client list: fetch manifest: %v", err)
		fmt.Fprintln(os.Stderr, "fetch manifest:", err)
		if len(manifestEntries) == 0 {
			fmt.Fprintln(os.Stderr, "No cached manifest. Ensure server is running and has run the PCGW sync job (game_save_locations).")
			os.Exit(1)
		}
		log.Printf("client list: using cached manifest (%d entries)", len(manifestEntries))
		fmt.Fprintln(os.Stderr, "Using cached manifest.")
	}

	// Build list of games where the save path exists on this machine
	type row struct {
		GameID     string
		GameTitle  string
		PathKey    string
		Resolved   string
		FromConfig bool
	}
	var rows []row
	seen := make(map[string]bool)

	// From manifest
	for _, e := range manifestEntries {
		if e.Platform != string(currentOS) {
			continue
		}
		roots := installRoots[e.GameID]
		resolved := resolver.ResolveAllForGame(e.PathTemplate, currentOS, roots)
		for _, abs := range resolved {
			if abs == "" {
				continue
			}
			dir := abs
			if info, err := os.Stat(abs); err == nil && !info.IsDir() {
				dir = filepath.Dir(abs)
			}
			if _, err := os.Stat(dir); err != nil {
				continue
			}
			pathKey := PathKeyForManifestEntry(e.GameID, e.PathTemplate)
			key := e.GameID + "\x00" + pathKey
			if seen[key] {
				continue
			}
			seen[key] = true
			rows = append(rows, row{
				GameID:     e.GameID,
				GameTitle:  e.GameTitle,
				PathKey:    pathKey,
				Resolved:   dir,
				FromConfig: false,
			})
		}
	}

	// From config watch_paths (may add games not in manifest)
	for _, wp := range cfg.WatchPaths {
		for _, t := range wp.PathTemplates {
			resolved := resolver.Resolve(t, currentOS)
			for _, abs := range resolved {
				if abs == "" {
					continue
				}
				dir := abs
				if info, err := os.Stat(abs); err == nil && !info.IsDir() {
					dir = filepath.Dir(abs)
				}
				if _, err := os.Stat(dir); err != nil {
					continue
				}
				key := wp.GameID + "\x00" + wp.PathKey
				if seen[key] {
					continue
				}
				seen[key] = true
				rows = append(rows, row{
					GameID:     wp.GameID,
					GameTitle:  "",
					PathKey:    wp.PathKey,
					Resolved:   dir,
					FromConfig: true,
				})
			}
		}
	}

	sort.Slice(rows, func(i, j int) bool {
		ti, tj := rows[i].GameTitle, rows[j].GameTitle
		if ti == "" {
			ti = rows[i].GameID
		}
		if tj == "" {
			tj = rows[j].GameID
		}
		return ti < tj
	})

	// Optional: which (game_id, path_key) have saves on server (use summaries to avoid downloading content)
	savesOnServer := make(map[string]bool)
	if cfg.Token != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.ServerURL+"/api/saves?summaries=1", nil)
		if err == nil {
			req.Header.Set("Authorization", "Bearer "+cfg.Token)
			listHTTP := &http.Client{Timeout: 30 * time.Second}
			resp, err := listHTTP.Do(req)
			if err == nil && resp.StatusCode == http.StatusOK {
				var out struct {
					Saves []struct {
						GameID  string `json:"game_id"`
						PathKey string `json:"path_key"`
					} `json:"saves"`
				}
				if json.NewDecoder(resp.Body).Decode(&out) == nil {
					for _, s := range out.Saves {
						savesOnServer[s.GameID+"\x00"+s.PathKey] = true
					}
				}
				resp.Body.Close()
			}
		}
	}

	fmt.Println("Games that can be saved and synced on this machine:")
	fmt.Println("(Save directory exists locally; client will watch and push changes, and pull server saves here.)")
	fmt.Println()
	for _, r := range rows {
		title := r.GameTitle
		if title == "" {
			title = r.GameID
		}
		syncStatus := ""
		if savesOnServer[r.GameID+"\x00"+r.PathKey] {
			syncStatus = " [synced on server]"
		} else if cfg.Token != "" {
			syncStatus = " [not on server]"
		}
		configNote := ""
		if r.FromConfig {
			configNote = " (from config)"
		}
		fmt.Printf("  %s  game_id=%s path_key=%s%s%s\n", title, r.GameID, r.PathKey, syncStatus, configNote)
		fmt.Printf("    %s\n", r.Resolved)
	}
	log.Printf("client list: listed %d games", len(rows))
	if len(rows) == 0 {
		fmt.Println("  (none — no manifest paths resolved to an existing directory on this OS)")
		fmt.Println("  Ensure the server has run the PCGW sync job and that game save folders exist locally.")
	}

	if dryRunPull && cfg.Token != "" {
		// Report what would be written by a pull (same resolution as sync).
		effectiveWatchPaths, _ := ManifestToWatchPaths(manifestEntries, resolver, currentOS, includeConfig, nil, "legacy", nil)
		effectiveWatchPaths = mergeWatchPaths(effectiveWatchPaths, cfg.WatchPaths)
		resolvePath := func(gameID, pathKey string) string {
			for _, w := range effectiveWatchPaths {
				if w.GameID != gameID || w.PathKey != pathKey {
					continue
				}
				for _, t := range w.PathTemplates {
					resolved := resolver.Resolve(t, currentOS)
					for _, abs := range resolved {
						if abs != "" && paths.PathExists(abs) {
							return abs
						}
					}
				}
			}
			return ""
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.ServerURL+"/api/saves?summaries=1", nil)
		if err != nil {
			fmt.Fprintln(os.Stderr, "dry-run-pull: request:", err)
			return
		}
		req.Header.Set("Authorization", "Bearer "+cfg.Token)
		listHTTP := &http.Client{Timeout: 30 * time.Second}
		resp, err := listHTTP.Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			fmt.Fprintln(os.Stderr, "dry-run-pull: fetch failed")
			if resp != nil {
				resp.Body.Close()
			}
			return
		}
		var out struct {
			Saves []struct {
				GameID    string `json:"game_id"`
				PathKey   string `json:"path_key"`
				UpdatedAt string `json:"updated_at"`
			} `json:"saves"`
		}
		if json.NewDecoder(resp.Body).Decode(&out) != nil {
			resp.Body.Close()
			return
		}
		resp.Body.Close()
		fmt.Println()
		fmt.Println("Dry-run pull (what would be written):")
		for _, s := range out.Saves {
			absPath := resolvePath(s.GameID, s.PathKey)
			if absPath == "" {
				continue
			}
			if !paths.PathExists(absPath) {
				// Dir might exist but path might be a file that doesn't exist yet
				dir := filepath.Dir(absPath)
				if !paths.PathExists(dir) {
					continue
				}
			}
			fmt.Printf("  %s  %s  -> %s\n", s.GameID, s.PathKey, absPath)
		}
	}
}
