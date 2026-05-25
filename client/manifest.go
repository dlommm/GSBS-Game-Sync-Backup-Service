package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gsbs/gsbs/pkg/paths"
	"github.com/gsbs/gsbs/pkg/retry"
	"github.com/gsbs/gsbs/pkg/types"
)

// manifestResponse is the API response for GET /api/manifest.
type manifestResponse struct {
	Entries []types.GameSaveLocation `json:"entries"`
}

const manifestFetchTimeout = 60 * time.Second

// FetchManifest downloads the manifest from the server. Tries v2 first for richer metadata, falls back to v1.
func FetchManifest(ctx context.Context, baseURL, token, since, include string) ([]types.GameSaveLocation, error) {
	if entries, err := fetchManifestV2(ctx, baseURL, token, since, include); err == nil && len(entries) > 0 {
		return entries, nil
	}
	return fetchManifestV1(ctx, baseURL, token, since, include)
}

func fetchManifestV1(ctx context.Context, baseURL, token, since, include string) ([]types.GameSaveLocation, error) {
	url := baseURL + "/api/manifest"
	params := []string{}
	if since != "" {
		params = append(params, "since="+since)
	}
	if include != "" && include != "both" {
		params = append(params, "include="+include)
	}
	if len(params) > 0 {
		url += "?" + strings.Join(params, "&")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: manifestFetchTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("manifest: %s", resp.Status)
	}
	var out manifestResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Entries, nil
}

func fetchManifestV2(ctx context.Context, baseURL, token, since, include string) ([]types.GameSaveLocation, error) {
	url := baseURL + "/api/manifest/v2"
	params := []string{}
	if since != "" {
		params = append(params, "since="+since)
	}
	if len(params) > 0 {
		url += "?" + strings.Join(params, "&")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: manifestFetchTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified || resp.StatusCode == http.StatusOK {
		if resp.StatusCode == http.StatusNotModified {
			return LoadManifestFromDisk(), nil
		}
	} else {
		return nil, fmt.Errorf("manifest v2: %s", resp.Status)
	}
	var out types.ManifestV2Response
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return manifestV2ToEntries(out, include), nil
}

func manifestV2ToEntries(v2 types.ManifestV2Response, include string) []types.GameSaveLocation {
	var entries []types.GameSaveLocation
	for _, g := range v2.Games {
		addLocs := func(locs []types.ManifestV2Location, isConfig bool) {
			for _, loc := range locs {
				if include == "saves" && isConfig {
					continue
				}
				if include == "config" && !isConfig {
					continue
				}
				for _, pt := range loc.PathTemplates {
					if pt == "" {
						continue
					}
					entries = append(entries, types.GameSaveLocation{
						GameID: g.GameID, PCGWPageID: parsePageID(g.GameID),
						GameTitle: g.Title, Platform: loc.Platform, PathTemplate: pt,
						IsConfig: isConfig, UpdatedAt: g.LastUpdated, Source: "pcgw",
						SteamAppIDs: g.SteamAppIDs, GOGID: g.GOGID, EpicID: g.EpicID, UbisoftID: g.UbisoftID,
					})
				}
			}
		}
		addLocs(g.SaveLocations, false)
		addLocs(g.ConfigLocations, true)
	}
	return entries
}

func parsePageID(gameID string) int64 {
	n, _ := strconv.ParseInt(gameID, 10, 64)
	return n
}

func manifestPath() string {
	dir, _ := os.UserConfigDir()
	return filepath.Join(dir, "gsbs", "manifest.json")
}

// manifestFile is the on-disk shape (entries + last fetch time for ?since=).
type manifestFile struct {
	Entries       []types.GameSaveLocation `json:"entries"`
	LastFetchedAt string                   `json:"last_fetched_at,omitempty"` // RFC3339
}

// LoadManifestFromDisk returns cached manifest entries. Returns nil if file missing or invalid.
func LoadManifestFromDisk() []types.GameSaveLocation {
	entries, _ := LoadManifestCache()
	return entries
}

// LoadManifestCache returns cached entries and the last fetch timestamp.
func LoadManifestCache() ([]types.GameSaveLocation, time.Time) {
	data, err := os.ReadFile(manifestPath())
	if err != nil {
		return nil, time.Time{}
	}
	var f manifestFile
	if json.Unmarshal(data, &f) != nil {
		return nil, time.Time{}
	}
	var lastFetched time.Time
	if f.LastFetchedAt != "" {
		lastFetched, _ = time.Parse(time.RFC3339, f.LastFetchedAt)
	}
	return f.Entries, lastFetched
}

// MergeManifestDelta merges delta entries into existing by (game_id, platform, path_template).
func MergeManifestDelta(existing, delta []types.GameSaveLocation) []types.GameSaveLocation {
	if len(delta) == 0 {
		return existing
	}
	if len(existing) == 0 {
		return delta
	}
	key := func(e types.GameSaveLocation) string {
		return e.GameID + "\x00" + e.Platform + "\x00" + e.PathTemplate
	}
	index := make(map[string]int, len(existing))
	for i, e := range existing {
		index[key(e)] = i
	}
	out := make([]types.GameSaveLocation, len(existing))
	copy(out, existing)
	for _, e := range delta {
		k := key(e)
		if i, ok := index[k]; ok {
			out[i] = e
		} else {
			out = append(out, e)
		}
	}
	return out
}

// SaveManifestToDisk writes the full manifest to disk atomically (write to temp file, then rename).
// Merge with existing if doing delta is caller's job.
func SaveManifestToDisk(entries []types.GameSaveLocation) error {
	target := manifestPath()
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(manifestFile{
		Entries:       entries,
		LastFetchedAt: time.Now().UTC().Format(time.RFC3339),
	}, "", "  ")
	if err != nil {
		return err
	}
	// Write to a temp file in the same directory then rename for atomicity.
	tmp, err := os.CreateTemp(dir, ".manifest-*.json.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, target); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

// PathKeyForManifestEntry returns a stable path key for (gameID, pathTemplate).
func PathKeyForManifestEntry(gameID, pathTemplate string) string {
	h := sha256.Sum256([]byte(gameID + "\x00" + pathTemplate))
	return hex.EncodeToString(h[:])[:16]
}

// ListenSSE connects to the server SSE endpoint and calls onEvent for each received event type.
// It auto-reconnects with exponential backoff on disconnect. Blocks until ctx is cancelled.
func ListenSSE(ctx context.Context, baseURL, token string, onEvent func(eventType string)) {
	const healthyThreshold = 30 * time.Second
	bo := retry.SSEBackoff()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		start := time.Now()
		err := connectSSE(ctx, baseURL, token, onEvent)
		if ctx.Err() != nil {
			return
		}
		if time.Since(start) >= healthyThreshold {
			bo.Reset()
		}
		delay := bo.Next()
		if err != nil {
			log.Printf("sse: connection error: %v (retrying in %s)", err, delay)
		} else {
			log.Printf("sse: disconnected (retrying in %s)", delay)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

func connectSSE(ctx context.Context, baseURL, token string, onEvent func(eventType string)) error {
	url := baseURL + "/api/events"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusUnauthorized {
			return fmt.Errorf("sse: 401 Unauthorized — token may be invalid or expired; try logging in again from the tray")
		}
		return fmt.Errorf("sse: %s", resp.Status)
	}

	scanner := bufio.NewScanner(resp.Body)
	var eventType string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			if eventType != "" {
				log.Printf("sse: received event: %s", eventType)
				onEvent(eventType)
			}
			eventType = ""
		} else if line == "" {
			eventType = ""
		}
	}
	return scanner.Err()
}

// ManifestToWatchPaths converts manifest entries to watch paths for the current OS.
// Only includes entries where the resolved path exists. Skips config-only if we only want saves.
// When mode is "discovered", only includes entries for game IDs in activeGameIDs.
func ManifestToWatchPaths(entries []types.GameSaveLocation, resolver *paths.Resolver, currentOS paths.OS, includeConfig bool, activeGameIDs map[string]bool, mode string) []watchPath {
	var out []watchPath
	seen := make(map[string]bool)
	discoveredMode := mode == "discovered" && len(activeGameIDs) > 0
	for _, e := range entries {
		if discoveredMode && !activeGameIDs[e.GameID] {
			continue
		}
		platform := e.Platform
		if platform != string(currentOS) {
			continue
		}
		if e.IsConfig && !includeConfig {
			continue
		}
		resolved := resolver.ResolveAll(e.PathTemplate, currentOS)
		for _, abs := range resolved {
			if abs == "" {
				continue
			}
			if !paths.WatchDirExists(abs) {
				continue
			}
			pathKey := PathKeyForManifestEntry(e.GameID, e.PathTemplate)
			key := e.GameID + "\x00" + pathKey
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, watchPath{
				GameID:        e.GameID,
				PathKey:       pathKey,
				PathTemplates: []string{e.PathTemplate},
			})
		}
	}
	return out
}

// resolveSavePath finds the local absolute path for a save slot.
func resolveSavePath(gameID, pathKey string, manifestEntries []types.GameSaveLocation, watchPaths []watchPath, resolver *paths.Resolver, currentOS paths.OS, pullCtx paths.PullContext) string {
	for _, w := range watchPaths {
		if w.GameID != gameID || w.PathKey != pathKey {
			continue
		}
		for _, t := range w.PathTemplates {
			for _, abs := range resolver.ResolveAll(t, currentOS) {
				if abs == "" {
					continue
				}
				if paths.WatchDirExists(abs) {
					return abs
				}
				elig := paths.EvaluatePullEligibility(abs, gameID, pullCtx)
				if elig == paths.ApplyReady || elig == paths.ApplyCreateDir {
					return abs
				}
			}
		}
	}
	for _, e := range manifestEntries {
		if e.GameID != gameID {
			continue
		}
		if PathKeyForManifestEntry(e.GameID, e.PathTemplate) != pathKey {
			continue
		}
		for _, abs := range resolver.ResolveAll(e.PathTemplate, currentOS) {
			if abs == "" {
				continue
			}
			elig := paths.EvaluatePullEligibility(abs, gameID, pullCtx)
			if elig == paths.ApplyReady || elig == paths.ApplyCreateDir {
				return abs
			}
		}
	}
	return ""
}
