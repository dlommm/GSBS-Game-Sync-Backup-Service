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
	"runtime"
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

// manifestFetchResult holds the outcome of a manifest download (v2 or v1).
type manifestFetchResult struct {
	Entries     []types.GameSaveLocation
	Games       []types.ManifestV2Game
	DeletedIDs  []string
	ETag        string
	Source      string // "v2" or "v1"
	NotModified bool
}

// FetchManifest downloads the manifest from the server. Tries v2 first, falls back to v1 on error, 404, or empty entries.
func FetchManifest(ctx context.Context, baseURL, token, since, include string) ([]types.GameSaveLocation, error) {
	res, err := FetchManifestFull(ctx, baseURL, token, since, include)
	if err != nil {
		return nil, err
	}
	return res.Entries, nil
}

// FetchManifestFull downloads manifest with v2 metadata; updates on-disk cache when not a 304.
func FetchManifestFull(ctx context.Context, baseURL, token, since, include string) (manifestFetchResult, error) {
	cached := LoadManifestFile()

	if res, err := fetchManifestV2(ctx, baseURL, token, since, include, cached); err == nil {
		if len(res.Entries) > 0 || res.NotModified {
			if !res.NotModified {
				merged := mergeManifestFetch(cached, res)
				if err := SaveManifestFile(merged); err != nil {
					log.Println("save manifest cache:", err)
				}
				return manifestFetchResult{
					Entries:     merged.Entries,
					Games:       merged.Games,
					DeletedIDs:  res.DeletedIDs,
					ETag:        merged.ETag,
					Source:      "v2",
					NotModified: false,
				}, nil
			}
			return manifestFetchResult{
				Entries:     cached.Entries,
				Games:       cached.Games,
				ETag:        cached.ETag,
				Source:      "v2",
				NotModified: true,
			}, nil
		}
	}

	entries, err := fetchManifestV1(ctx, baseURL, token, since, include)
	if err != nil {
		return manifestFetchResult{}, err
	}
	f := manifestFile{
		Entries:       entries,
		LastFetchedAt: time.Now().UTC().Format(time.RFC3339),
		Source:        "v1",
	}
	if err := SaveManifestFile(f); err != nil {
		log.Println("save manifest cache:", err)
	}
	return manifestFetchResult{Entries: entries, Source: "v1"}, nil
}

func manifestPlatformParam() string {
	switch runtime.GOOS {
	case "windows":
		return "windows"
	case "darwin":
		return "macos"
	default:
		return "linux"
	}
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

func fetchManifestV2(ctx context.Context, baseURL, token, since, include string, cached manifestFile) (manifestFetchResult, error) {
	url := baseURL + "/api/manifest/v2"
	params := []string{"platform=" + manifestPlatformParam()}
	if since != "" {
		params = append(params, "since="+since)
	}
	url += "?" + strings.Join(params, "&")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return manifestFetchResult{}, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if cached.ETag != "" {
		req.Header.Set("If-None-Match", cached.ETag)
	}

	client := &http.Client{Timeout: manifestFetchTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return manifestFetchResult{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return manifestFetchResult{NotModified: true}, nil
	}
	if resp.StatusCode == http.StatusNotFound {
		return manifestFetchResult{}, fmt.Errorf("manifest v2: not found")
	}
	if resp.StatusCode != http.StatusOK {
		return manifestFetchResult{}, fmt.Errorf("manifest v2: %s", resp.Status)
	}

	var out types.ManifestV2Response
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return manifestFetchResult{}, err
	}
	etag := out.ETag
	if etag == "" {
		etag = resp.Header.Get("ETag")
	}
	entries := manifestV2ToEntries(out, include)
	return manifestFetchResult{
		Entries:    entries,
		Games:      out.Games,
		DeletedIDs: out.DeletedGameIDs,
		ETag:       etag,
	}, nil
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
						Notes: loc.Notes, SteamAppIDs: g.SteamAppIDs,
						GOGID: g.GOGID, EpicID: g.EpicID, UbisoftID: g.UbisoftID,
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

var manifestPathOverride string

func manifestPath() string {
	if manifestPathOverride != "" {
		return manifestPathOverride
	}
	dir, _ := os.UserConfigDir()
	return filepath.Join(dir, "gsbs", "manifest.json")
}

// SetManifestPathForTest overrides the on-disk manifest cache path (tests only).
func SetManifestPathForTest(path string) {
	manifestPathOverride = path
}

// manifestFile is the on-disk manifest cache (flat entries + optional v2 game metadata).
type manifestFile struct {
	Entries       []types.GameSaveLocation `json:"entries"`
	Games         []types.ManifestV2Game   `json:"games,omitempty"`
	ETag          string                   `json:"etag,omitempty"`
	Source        string                   `json:"source,omitempty"` // "v2" or "v1"
	LastFetchedAt string                   `json:"last_fetched_at,omitempty"`
}

// LoadManifestFromDisk returns cached manifest entries. Returns nil if file missing or invalid.
func LoadManifestFromDisk() []types.GameSaveLocation {
	f := LoadManifestFile()
	return f.Entries
}

// LoadManifestCache returns cached entries and the last fetch timestamp.
func LoadManifestCache() ([]types.GameSaveLocation, time.Time) {
	f := LoadManifestFile()
	var lastFetched time.Time
	if f.LastFetchedAt != "" {
		lastFetched, _ = time.Parse(time.RFC3339, f.LastFetchedAt)
	}
	return f.Entries, lastFetched
}

// LoadManifestFile returns the full on-disk manifest cache.
func LoadManifestFile() manifestFile {
	data, err := os.ReadFile(manifestPath())
	if err != nil {
		return manifestFile{}
	}
	var f manifestFile
	if json.Unmarshal(data, &f) != nil {
		return manifestFile{}
	}
	return f
}

// mergeManifestFetch merges a v2 fetch delta into the cached manifest file.
func mergeManifestFetch(cached manifestFile, res manifestFetchResult) manifestFile {
	out := cached
	out.ETag = res.ETag
	out.Source = "v2"
	out.LastFetchedAt = time.Now().UTC().Format(time.RFC3339)

	if len(res.DeletedIDs) > 0 {
		out.Entries = ApplyManifestDeletions(out.Entries, res.DeletedIDs)
		out.Games = applyGameDeletions(out.Games, res.DeletedIDs)
	}

	if len(res.Entries) > 0 {
		if len(cached.Entries) > 0 {
			out.Entries = MergeManifestDelta(cached.Entries, res.Entries)
		} else {
			out.Entries = res.Entries
		}
	}
	if len(res.Games) > 0 {
		out.Games = mergeV2Games(cached.Games, res.Games, res.DeletedIDs)
	} else if len(cached.Games) == 0 && len(res.Entries) > 0 {
		out.Games = nil
	}
	return out
}

func mergeV2Games(existing, delta []types.ManifestV2Game, deleted []string) []types.ManifestV2Game {
	if len(deleted) > 0 {
		existing = applyGameDeletions(existing, deleted)
	}
	if len(existing) == 0 {
		return delta
	}
	if len(delta) == 0 {
		return existing
	}
	index := make(map[string]int, len(existing))
	for i, g := range existing {
		index[g.GameID] = i
	}
	out := make([]types.ManifestV2Game, len(existing))
	copy(out, existing)
	for _, g := range delta {
		if i, ok := index[g.GameID]; ok {
			out[i] = g
		} else {
			out = append(out, g)
		}
	}
	return out
}

func applyGameDeletions(games []types.ManifestV2Game, deleted []string) []types.ManifestV2Game {
	if len(deleted) == 0 {
		return games
	}
	del := make(map[string]bool, len(deleted))
	for _, id := range deleted {
		del[id] = true
	}
	var out []types.ManifestV2Game
	for _, g := range games {
		if !del[g.GameID] {
			out = append(out, g)
		}
	}
	return out
}

// ApplyManifestDeletions removes all entries for deleted game IDs.
func ApplyManifestDeletions(entries []types.GameSaveLocation, deleted []string) []types.GameSaveLocation {
	if len(deleted) == 0 {
		return entries
	}
	del := make(map[string]bool, len(deleted))
	for _, id := range deleted {
		del[id] = true
	}
	var out []types.GameSaveLocation
	for _, e := range entries {
		if !del[e.GameID] {
			out = append(out, e)
		}
	}
	return out
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

// SaveManifestToDisk writes flat entries to disk, preserving v2 metadata when present.
func SaveManifestToDisk(entries []types.GameSaveLocation) error {
	f := LoadManifestFile()
	f.Entries = entries
	f.LastFetchedAt = time.Now().UTC().Format(time.RFC3339)
	return SaveManifestFile(f)
}

// SaveManifestFile writes the full manifest cache atomically.
func SaveManifestFile(f manifestFile) error {
	target := manifestPath()
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	if f.LastFetchedAt == "" {
		f.LastFetchedAt = time.Now().UTC().Format(time.RFC3339)
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
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

// ManifestETagAge returns how long since the manifest was last fetched, or zero if unknown.
func ManifestETagAge() time.Duration {
	f := LoadManifestFile()
	if f.LastFetchedAt == "" {
		return 0
	}
	t, err := time.Parse(time.RFC3339, f.LastFetchedAt)
	if err != nil {
		return 0
	}
	return time.Since(t)
}

// pingManifestHealth checks server reachability via manifest v2 (fallback v1).
func pingManifestHealth(baseURL, token string) (*http.Response, error) {
	baseURL = strings.TrimSuffix(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("server URL is empty")
	}
	client := &http.Client{Timeout: 5 * time.Second}
	for _, path := range []string{"/api/manifest/v2", "/api/manifest"} {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, baseURL+path, nil)
		if err != nil {
			return nil, err
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusUnauthorized ||
			resp.StatusCode == http.StatusNotModified {
			return resp, nil
		}
		resp.Body.Close()
	}
	return nil, fmt.Errorf("manifest health check failed")
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
// When mode is "discovered", only includes entries for game IDs in activeGameIDs (or explicit config paths via mergeWatchPaths).
func ManifestToWatchPaths(entries []types.GameSaveLocation, resolver *paths.Resolver, currentOS paths.OS, includeConfig bool, activeGameIDs map[string]bool, mode string) []watchPath {
	var out []watchPath
	seen := make(map[string]bool)
	discoveredMode := mode == "discovered"
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
