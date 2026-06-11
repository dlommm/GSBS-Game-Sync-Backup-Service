package store

import (
	"context"
	"os"
	"strings"
)

// PCGW sync source values.
const (
	PCGWSyncSourceGitHub = "github"
	PCGWSyncSourceAPI    = "api"
)

// Admin setting keys for GitHub manifest bundle sync.
const (
	AdminSettingPCGWSyncSource              = "pcgw_sync_source"
	AdminSettingPCGWBundleURL               = "pcgw_bundle_url"
	AdminSettingPCGWBundleDeltaURL          = "pcgw_bundle_delta_url"
	AdminSettingPCGWBundleCron              = "pcgw_bundle_cron"
	AdminSettingPCGWBundleETag              = "pcgw_bundle_etag"
	AdminSettingPCGWBundleDeltaETag         = "pcgw_bundle_delta_etag"
	AdminSettingPCGWBundleLastFetchedAt     = "pcgw_bundle_last_fetched_at"
	AdminSettingPCGWBundleLastExportedAt    = "pcgw_bundle_last_exported_at"
	AdminSettingPCGWBundleFullExportedAt    = "pcgw_bundle_full_exported_at"
	AdminSettingPCGWBundleIncrementalFB     = "pcgw_bundle_incremental_fallback"
	AdminSettingPCGWBundleLastFetchError    = "pcgw_bundle_last_fetch_error"
)

const (
	DefaultPCGWBundleURL      = "https://raw.githubusercontent.com/dlommm/gsbs-manifest/main/manifest.json.gz"
	DefaultPCGWBundleDeltaURL = "https://raw.githubusercontent.com/dlommm/gsbs-manifest/main/manifest.delta.json.gz"
	DefaultPCGWBundleCron     = "0 4 * * *"
)

// Env var names (override admin_settings when set).
const (
	EnvPCGWSyncSource     = "GSBS_PCGW_SYNC_SOURCE"
	EnvPCGWBundleURL      = "GSBS_PCGW_BUNDLE_URL"
	EnvPCGWBundleDeltaURL = "GSBS_PCGW_BUNDLE_DELTA_URL"
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
	NoOp              bool
}

// PCGWSyncSourceFromSettings returns github or api (default github for unset fresh installs).
func PCGWSyncSourceFromSettings(settings map[string]string) string {
	if v, ok := os.LookupEnv(EnvPCGWSyncSource); ok {
		v = strings.TrimSpace(strings.ToLower(v))
		if v == PCGWSyncSourceAPI || v == PCGWSyncSourceGitHub {
			return v
		}
	}
	if v, ok := settings[AdminSettingPCGWSyncSource]; ok && v != "" {
		v = strings.TrimSpace(strings.ToLower(v))
		if v == PCGWSyncSourceAPI || v == PCGWSyncSourceGitHub {
			return v
		}
	}
	return PCGWSyncSourceGitHub
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

func PCGWBundleDeltaURLFromSettings(settings map[string]string) string {
	if v := strings.TrimSpace(os.Getenv(EnvPCGWBundleDeltaURL)); v != "" {
		return v
	}
	if v := strings.TrimSpace(settings[AdminSettingPCGWBundleDeltaURL]); v != "" {
		return v
	}
	return DefaultPCGWBundleDeltaURL
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
