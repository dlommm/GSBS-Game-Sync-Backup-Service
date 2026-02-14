package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gsbs/gsbs/pkg/paths"
	"github.com/gsbs/gsbs/pkg/types"
)

// manifestResponse is the API response for GET /api/manifest.
type manifestResponse struct {
	Entries []types.GameSaveLocation `json:"entries"`
}

// FetchManifest downloads the manifest from the server. If since is non-empty, requests delta.
func FetchManifest(ctx context.Context, baseURL, since string) ([]types.GameSaveLocation, error) {
	url := baseURL + "/api/manifest"
	if since != "" {
		url += "?since=" + since
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
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

// SaveManifestToDisk writes the full manifest to disk (merge with existing if doing delta is caller's job).
func SaveManifestToDisk(entries []types.GameSaveLocation) error {
	dir := filepath.Dir(manifestPath())
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(manifestFile{Entries: entries}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(manifestPath(), data, 0644)
}

// PathKeyForManifestEntry returns a stable path key for (gameID, pathTemplate).
func PathKeyForManifestEntry(gameID, pathTemplate string) string {
	h := sha256.Sum256([]byte(gameID + "\x00" + pathTemplate))
	return hex.EncodeToString(h[:])[:16]
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
