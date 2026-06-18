package store

import (
	"encoding/json"
	"fmt"
	"strings"
)

// PCGWBundleIndex is the small, atomically-fetched pointer published alongside
// the manifest bundle (index.json). It is the single source of truth for which
// full bundle is current and what version it represents, so a server can decide
// whether to download in one cheap round-trip.
//
// Versioning model (full bundle only):
//   - ManifestVersion is the current version; it increments by 1 on every publish
//     and always equals Full.Version.
//   - Full describes the one downloadable artifact (the complete manifest). Each
//     publish overwrites it; a server merges the full bundle to catch up from any
//     prior version (the import upserts with skip-unchanged semantics).
//
// A "delta" field may still appear in older index.json files; it is ignored.
type PCGWBundleIndex struct {
	ManifestVersion int                  `json:"manifest_version"`
	GeneratedAt     string               `json:"generated_at,omitempty"`
	Full            PCGWBundleIndexEntry `json:"full"`
}

// PCGWBundleIndexEntry describes one downloadable artifact.
type PCGWBundleIndexEntry struct {
	Version int    `json:"version"`
	URL     string `json:"url"`
	SHA256  string `json:"sha256,omitempty"`
	Bytes   int    `json:"bytes,omitempty"`
}

// ParsePCGWBundleIndex unmarshals and validates index.json bytes.
func ParsePCGWBundleIndex(data []byte) (PCGWBundleIndex, error) {
	var idx PCGWBundleIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return PCGWBundleIndex{}, fmt.Errorf("invalid index.json: %w", err)
	}
	if idx.ManifestVersion < 1 {
		return PCGWBundleIndex{}, fmt.Errorf("index.json: manifest_version must be >= 1, got %d", idx.ManifestVersion)
	}
	if idx.Full.Version < 1 || strings.TrimSpace(idx.Full.URL) == "" {
		return PCGWBundleIndex{}, fmt.Errorf("index.json: full bundle version/url required")
	}
	return idx, nil
}

// AdvanceBundleIndex computes the next index.json for a full-bundle publish,
// given the previous index (zero value for the very first publish) and the new
// artifact's checksum/size. baseURL is the directory URL where the artifact is
// hosted (a trailing slash is added if missing). Every publish increments
// manifest_version by 1 and overwrites the full entry.
//
// The URL carries a CONTENT-ADDRESSED cache key (?h=<sha-prefix>) so each
// distinct bundle has a distinct, unguessable URL. The object keeps a fixed
// filename (overwritten in place), but the CDN caches the content-hash URL
// separately, so a freshly published bundle can never be masked by a stale
// cached copy — and because the key is derived from the content, nothing can
// pollute it before the content exists. No cache purging required.
func AdvanceBundleIndex(prev PCGWBundleIndex, sha256 string, bytes int, baseURL, generatedAt string) (PCGWBundleIndex, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return PCGWBundleIndex{}, fmt.Errorf("base URL required")
	}
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}
	next := prev.ManifestVersion + 1
	return PCGWBundleIndex{
		ManifestVersion: next,
		GeneratedAt:     generatedAt,
		Full: PCGWBundleIndexEntry{
			Version: next,
			URL:     fmt.Sprintf("%smanifest.json.gz?h=%s", baseURL, cacheKey(sha256)),
			SHA256:  sha256,
			Bytes:   bytes,
		},
	}, nil
}

// cacheKey returns a short content-hash fragment for the CDN cache-busting query.
func cacheKey(sha256 string) string {
	if len(sha256) >= 16 {
		return sha256[:16]
	}
	return sha256
}

// BundleStep is one fetch-and-import action in a catch-up plan.
type BundleStep struct {
	Kind    string // always "full"
	URL     string
	SHA256  string
	Mode    string // import mode passed to ImportPCGWManifestBundle
	Version int    // merged version after this step applies
}

// PlanBundleCatchup returns the steps to bring a server currently at merged up
// to the latest version named by the index. It returns nil when the server is
// already current.
//
// With full-bundle-only publishing the plan is at most one step: fetch and merge
// the current full bundle. Merging is safe from any prior version because the
// import upserts with skip-unchanged semantics and reconciles deletions against
// the bundle's complete catalog.
func PlanBundleCatchup(merged int, idx PCGWBundleIndex) []BundleStep {
	if merged >= idx.ManifestVersion {
		return nil
	}
	return []BundleStep{{
		Kind: "full", URL: idx.Full.URL, SHA256: idx.Full.SHA256,
		Mode: "merge_skip_unchanged", Version: idx.Full.Version,
	}}
}
