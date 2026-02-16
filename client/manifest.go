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
	"strings"
	"time"

	"github.com/gsbs/gsbs/pkg/paths"
	"github.com/gsbs/gsbs/pkg/types"
)

// manifestResponse is the API response for GET /api/manifest.
type manifestResponse struct {
	Entries []types.GameSaveLocation `json:"entries"`
}

const manifestFetchTimeout = 60 * time.Second

// FetchManifest downloads the manifest from the server. If since is non-empty, requests delta.
// include is "saves", "config", or "both" (default) to filter manifest content.
// If token is non-empty, it is sent as a Bearer token for server-side fetch tracking.
func FetchManifest(ctx context.Context, baseURL, token, since, include string) ([]types.GameSaveLocation, error) {
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

func manifestPath() string {
	dir, _ := os.UserConfigDir()
	return filepath.Join(dir, "gsbs", "manifest.json")
}

// manifestFile is the on-disk shape (entries + last fetch time for optional ?since=).
type manifestFile struct {
	Entries []types.GameSaveLocation `json:"entries"`
}

// LoadManifestFromDisk returns cached manifest entries. Returns nil if file missing or invalid.
func LoadManifestFromDisk() []types.GameSaveLocation {
	data, err := os.ReadFile(manifestPath())
	if err != nil {
		return nil
	}
	var f manifestFile
	if json.Unmarshal(data, &f) != nil {
		return nil
	}
	return f.Entries
}

// SaveManifestToDisk writes the full manifest to disk atomically (write to temp file, then rename).
// Merge with existing if doing delta is caller's job.
func SaveManifestToDisk(entries []types.GameSaveLocation) error {
	target := manifestPath()
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(manifestFile{Entries: entries}, "", "  ")
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
	const (
		minBackoff = 2 * time.Second
		maxBackoff = 60 * time.Second
		// If connection lasted longer than this, reset backoff (was a healthy connection).
		healthyThreshold = 30 * time.Second
	)
	backoff := minBackoff

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
		// Reset backoff if the connection was healthy (lasted a while before dropping).
		if time.Since(start) >= healthyThreshold {
			backoff = minBackoff
		}
		if err != nil {
			log.Printf("sse: connection error: %v (retrying in %s)", err, backoff)
		} else {
			log.Printf("sse: disconnected (retrying in %s)", backoff)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
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
func ManifestToWatchPaths(entries []types.GameSaveLocation, resolver *paths.Resolver, currentOS paths.OS, includeConfig bool) []watchPath {
	var out []watchPath
	seen := make(map[string]bool)
	for _, e := range entries {
		platform := e.Platform
		if platform != string(currentOS) {
			continue
		}
		if e.IsConfig && !includeConfig {
			continue
		}
		resolved := resolver.Resolve(e.PathTemplate, currentOS)
		for _, abs := range resolved {
			if abs == "" {
				continue
			}
			// Resolver may return a file path; we need the directory to exist for watching
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
			out = append(out, watchPath{
				GameID:        e.GameID,
				PathKey:       pathKey,
				PathTemplates: []string{e.PathTemplate},
			})
		}
	}
	return out
}
