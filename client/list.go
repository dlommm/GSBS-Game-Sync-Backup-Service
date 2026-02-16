package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"

	"github.com/gsbs/gsbs/pkg/paths"
	"github.com/gsbs/gsbs/pkg/types"
)

// runList prints games that can be saved and synced on this machine.
// The server provides a manifest of known game save locations (from the PCGW sync job).
// The client resolves each path for the current OS and only lists games where the save directory exists locally.
func runList() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(1)
	}
	if cfg.ServerURL == "" {
		fmt.Fprintln(os.Stderr, "server_url not set. Run 'gsbs-client login' or set server_url in config.")
		os.Exit(1)
	}

	resolver := paths.NewResolver()
	if cfg.UbisoftConnectFolder != "" {
		resolver.UbisoftConnect = cfg.UbisoftConnectFolder
	}
	if cfg.LauncherUserID != "" {
		resolver.UserID = cfg.LauncherUserID
	}
	currentOS := paths.CurrentOS()

	// Load manifest (server or cache)
	ctx := context.Background()
	manifestEntries := LoadManifestFromDisk()
	if entries, err := FetchManifest(ctx, cfg.ServerURL, cfg.Token, ""); err == nil {
		manifestEntries = entries
		if err := SaveManifestToDisk(entries); err != nil {
			fmt.Fprintln(os.Stderr, "save manifest cache:", err)
		}
	} else {
		fmt.Fprintln(os.Stderr, "fetch manifest:", err)
		if len(manifestEntries) == 0 {
			fmt.Fprintln(os.Stderr, "No cached manifest. Ensure server is running and has run the PCGW sync job (game_save_locations).")
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "Using cached manifest.")
	}

	// Build list of games where the save path exists on this machine
	type row struct {
		GameID    string
		GameTitle string
		PathKey   string
		Resolved  string
		FromConfig bool
	}
	var rows []row
	seen := make(map[string]bool)

	// From manifest
	for _, e := range manifestEntries {
		if e.Platform != string(currentOS) {
			continue
		}
		resolved := resolver.Resolve(e.PathTemplate, currentOS)
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

	// Optional: which (game_id, path_key) have saves on server
	savesOnServer := make(map[string]bool)
	if cfg.Token != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.ServerURL+"/api/saves", nil)
		if err == nil {
			req.Header.Set("Authorization", "Bearer "+cfg.Token)
			resp, err := http.DefaultClient.Do(req)
			if err == nil && resp.StatusCode == http.StatusOK {
				var out struct {
					Saves []types.SaveEntry `json:"saves"`
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
	if len(rows) == 0 {
		fmt.Println("  (none — no manifest paths resolved to an existing directory on this OS)")
		fmt.Println("  Ensure the server has run the PCGW sync job and that game save folders exist locally.")
	}
}
