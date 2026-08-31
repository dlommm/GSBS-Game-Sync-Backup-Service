package store

import (
	"context"
	"os"
	"strings"
)

// PCGW sync source values.
const (
	// PCGWSyncSourceS3 fetches the prebuilt manifest bundle from the public
	// CDN/object-store URL (Cloudflare R2 by default). This is the default.
	PCGWSyncSourceS3 = "s3"
	// PCGWSyncSourceGitHub is the legacy name for the bundle source, kept as a
	// backward-compat alias for existing installs (normalized to s3 on read).
	PCGWSyncSourceGitHub = "github"
	// PCGWSyncSourceAPI crawled the PCGW API directly (manual mode). RETIRED:
	// PCGamingWiki removed the runcargoqueries right from anonymous users, so
	// the catalog scan the mode depends on returns permissiondenied for every
	// install. The constant is kept so stored and env values can still be
	// recognized and normalized to the bundle source.
	PCGWSyncSourceAPI = "api"
)

// Admin setting keys for GitHub manifest bundle sync.
const (
	AdminSettingPCGWSyncSource           = "pcgw_sync_source"
	AdminSettingPCGWBundleURL            = "pcgw_bundle_url"
	AdminSettingPCGWBundleCron           = "pcgw_bundle_cron"
	AdminSettingPCGWBundleETag           = "pcgw_bundle_etag"
	AdminSettingPCGWBundleLastFetchedAt  = "pcgw_bundle_last_fetched_at"
	AdminSettingPCGWBundleLastExportedAt = "pcgw_bundle_last_exported_at"
	AdminSettingPCGWBundleFullExportedAt = "pcgw_bundle_full_exported_at"
	AdminSettingPCGWBundleIncrementalFB  = "pcgw_bundle_incremental_fallback"
	AdminSettingPCGWBundleLastFetchError = "pcgw_bundle_last_fetch_error"
	// Versioned-index sync (index.json): single atomic pointer + monotonic version.
	AdminSettingPCGWBundleIndexURL      = "pcgw_bundle_index_url"
	AdminSettingPCGWBundleIndexETag     = "pcgw_bundle_index_etag"
	AdminSettingPCGWBundleMergedVersion = "pcgw_bundle_merged_version"
	AdminSettingPCGWBundleLatestVersion = "pcgw_bundle_latest_version" // last seen index manifest_version (for "N behind" display)
)

const (
	// Default artifact URLs point at the official public CDN (Cloudflare R2 via a
	// custom domain). These are READ-ONLY, public URLs — they carry no
	// credentials and cannot be written to. Operators can override any of them
	// via the GSBS_PCGW_BUNDLE_*_URL env vars or Admin → Settings (e.g. to self-host).
	DefaultPCGWBundleURL      = "https://gsbs.ohhcloud.com/manifest/manifest.json.gz"
	DefaultPCGWBundleIndexURL = "https://gsbs.ohhcloud.com/manifest/index.json"
	// Monday 03:00 — the public bundle is published by the VPS job Sunday
	// 03:00 UTC, so fetching Monday morning always picks up a finished run.
	DefaultPCGWBundleCron = "0 3 * * 1"
)

// Env var names (override admin_settings when set).
const (
	EnvPCGWSyncSource     = "GSBS_PCGW_SYNC_SOURCE"
	EnvPCGWBundleURL      = "GSBS_PCGW_BUNDLE_URL"
	EnvPCGWBundleIndexURL = "GSBS_PCGW_BUNDLE_INDEX_URL"
	EnvPCGWBundleCron     = "GSBS_PCGW_BUNDLE_CRON"
)

// PCGWBundleExportOpts controls manifest bundle export.
type PCGWBundleExportOpts struct {
	Lite               bool
	Since              string // RFC3339; non-empty = delta export
	PreviousExportedAt string
	FullExportedAt     string // anchor full bundle timestamp (delta exports)
}

// PCGWImportResult summarizes a manifest bundle import.
type PCGWImportResult struct {
	Mode              string
	GameSaveLocations int
	PCGWGames         int
	PCGWGameData      int
	PCGWMetadata      int
	PCGWSections      int
	PCGWSystemReqs    int
	PCGWCatalog       int
	RowsChanged       int
	ExportedAt        string
	FullExportedAt    string
	SkippedUnchanged  int
	SkippedOrphans    int // child rows skipped because their parent game was absent
	Deleted           int // local games removed because they were absent from the bundle catalog
	NoOp              bool
}

// PCGWSyncSourceFromSettings returns the canonical sync source, which is always
// "s3" now that the direct-API mode is retired. The legacy values "github" and
// "api" are accepted from the env var or admin_settings and normalized to "s3".
func PCGWSyncSourceFromSettings(settings map[string]string) string {
	if v, ok := os.LookupEnv(EnvPCGWSyncSource); ok {
		if src, ok := normalizePCGWSyncSource(v); ok {
			return src
		}
	}
	if v, ok := settings[AdminSettingPCGWSyncSource]; ok && v != "" {
		if src, ok := normalizePCGWSyncSource(v); ok {
			return src
		}
	}
	return PCGWSyncSourceS3
}

// normalizePCGWSyncSource maps a raw source value to its canonical form. It
// accepts "s3" and the retired "github" and "api" values, all of which resolve
// to the bundle source. ok is false for unrecognized values so callers fall
// through to the default.
func normalizePCGWSyncSource(v string) (string, bool) {
	switch strings.TrimSpace(strings.ToLower(v)) {
	case PCGWSyncSourceS3, PCGWSyncSourceGitHub, PCGWSyncSourceAPI:
		return PCGWSyncSourceS3, true
	default:
		return "", false
	}
}

func PCGWBundleURLFromSettings(settings map[string]string) string {
	if v := strings.TrimSpace(os.Getenv(EnvPCGWBundleURL)); v != "" {
		return v
	}
	if v := strings.TrimSpace(settings[AdminSettingPCGWBundleURL]); v != "" {
		return v
	}
	return DefaultPCGWBundleURL
}

// PCGWBundleIndexURLFromSettings resolves the index.json URL, or "" when the
// indexed (versioned) sync path should not be attempted.
//
// It returns a URL only when the operator opted in explicitly (env or setting)
// or is using the default official manifest. A custom GSBS_PCGW_BUNDLE_URL with
// no index URL stays on the legacy timestamp flow until the operator sets an
// index URL — this avoids firing requests at a derived URL that may not exist.
func PCGWBundleIndexURLFromSettings(settings map[string]string) string {
	if v := strings.TrimSpace(os.Getenv(EnvPCGWBundleIndexURL)); v != "" {
		return v
	}
	if v := strings.TrimSpace(settings[AdminSettingPCGWBundleIndexURL]); v != "" {
		return v
	}
	if PCGWBundleURLFromSettings(settings) == DefaultPCGWBundleURL {
		return DefaultPCGWBundleIndexURL
	}
	return ""
}

func PCGWBundleCronFromSettings(settings map[string]string) string {
	if v, ok := os.LookupEnv(EnvPCGWBundleCron); ok {
		return v
	}
	if v, ok := settings[AdminSettingPCGWBundleCron]; ok {
		return v
	}
	return DefaultPCGWBundleCron
}

func PCGWBundleIncrementalFallbackFromSettings(settings map[string]string) bool {
	v := settings[AdminSettingPCGWBundleIncrementalFB]
	return v == "true" || v == "1"
}

// IsPCGWBundleSeeded reports whether local SQL already has PCGW mirror data.
func (s *sqliteStore) IsPCGWBundleSeeded(ctx context.Context) (bool, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pcgw_games`).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}
