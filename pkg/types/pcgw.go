package types

// PCGWGame is the core PCGW index row.
type PCGWGame struct {
	PageID           int64                  `json:"page_id"`
	PageName         string                 `json:"page_name"`
	Title            string                 `json:"title"`
	IsDisambiguation bool                   `json:"is_disambiguation"`
	RedirectsTo      string                 `json:"redirects_to,omitempty"`
	SteamAppIDs      []string               `json:"steam_appids"`
	GOGID            string                 `json:"gog_id,omitempty"`
	EpicID           string                 `json:"epic_id,omitempty"`
	UbisoftID        string                 `json:"ubisoft_id,omitempty"`
	MicrosoftID      string                 `json:"microsoft_id,omitempty"`
	BattlenetID      string                 `json:"battlenet_id,omitempty"`
	ItchID           string                 `json:"itch_id,omitempty"`
	OtherIDs         map[string]string      `json:"other_ids,omitempty"`
	Developers       []string               `json:"developers,omitempty"`
	Publishers       []string               `json:"publishers,omitempty"`
	ReleaseDates     []string               `json:"release_dates,omitempty"`
	Engines          []string               `json:"engines,omitempty"`
	Taxonomy         map[string]interface{} `json:"taxonomy,omitempty"`
	Infobox          map[string]interface{} `json:"infobox,omitempty"`
	CoverURL         string                 `json:"cover_url,omitempty"`
	HLTBID           string                 `json:"hltb_id,omitempty"`
	IGDBID           string                 `json:"igdb_id,omitempty"`
	CargoLastUpdated string                 `json:"cargo_last_updated,omitempty"`
	PlatformsPresent []string               `json:"platforms_present,omitempty"`
	LastRevID        int64                  `json:"last_rev_id,omitempty"`
	LastRevTimestamp string                 `json:"last_rev_timestamp,omitempty"`
	LastFetchedAt    string                 `json:"last_fetched_at,omitempty"`
	ParseStatus      string                 `json:"parse_status"`
	ParseError       string                 `json:"parse_error,omitempty"`
	ParseDurationMs  int                    `json:"parse_duration_ms"`
	CreatedAt        string                 `json:"created_at,omitempty"`
	UpdatedAt        string                 `json:"updated_at,omitempty"`
}

// PCGWPathEntry is a save or config location group.
type PCGWPathEntry struct {
	PathTemplates []string `json:"path_templates"`
	IsConfig      bool     `json:"is_config"`
	Notes         string   `json:"notes,omitempty"`
}

// PCGWGameData is per-platform Game data section content.
type PCGWGameData struct {
	PageID             int64                  `json:"page_id"`
	PlatformKey        string                 `json:"platform_key"`
	PlatformRawLabel   string                 `json:"platform_raw_label,omitempty"`
	SaveLocations      []PCGWPathEntry        `json:"save_locations,omitempty"`
	ConfigLocations    []PCGWPathEntry        `json:"config_locations,omitempty"`
	SaveGameCloudSync  map[string]interface{} `json:"save_game_cloud_sync,omitempty"`
	InstallLocations   []interface{}          `json:"install_locations,omitempty"`
	RegistryKeys       []interface{}          `json:"registry_keys,omitempty"`
	SaveFileInfo       map[string]interface{} `json:"save_file_info,omitempty"`
	AllTemplates       []string               `json:"all_templates,omitempty"`
	SectionWikitext    string                 `json:"section_wikitext,omitempty"`
	Structured         map[string]interface{} `json:"structured,omitempty"`
	UpdatedAt          string                 `json:"updated_at,omitempty"`
}

// PCGWSectionRow is a generic PCGW page section (Availability, Video, etc.).
type PCGWSectionRow struct {
	PageID          int64                  `json:"page_id"`
	Data            map[string]interface{} `json:"data,omitempty"`
	AllTemplates    []string               `json:"all_templates,omitempty"`
	SectionWikitext string                 `json:"section_wikitext,omitempty"`
	UpdatedAt       string                 `json:"updated_at,omitempty"`
}

// PCGWSystemRequirement is structured min/rec specs per platform.
type PCGWSystemRequirement struct {
	PageID           int64                  `json:"page_id"`
	PlatformKey      string                 `json:"platform_key"`
	RequirementType  string                 `json:"requirement_type"`
	Specs            map[string]interface{} `json:"specs,omitempty"`
	SectionWikitext  string                 `json:"section_wikitext,omitempty"`
	UpdatedAt        string                 `json:"updated_at,omitempty"`
}

// PCGWMetadata holds full-page wikitext cache and section hashes.
type PCGWMetadata struct {
	PageID           int64                  `json:"page_id"`
	FullWikitextZstd []byte                 `json:"-"`
	ContentHash      string                 `json:"content_hash,omitempty"`
	SectionHashes    map[string]string      `json:"section_hashes,omitempty"`
	ParsedSections   map[string]interface{} `json:"parsed_sections,omitempty"`
	UncompressedSize int                    `json:"uncompressed_size"`
	LastFetchedAt    string                 `json:"last_fetched_at,omitempty"`
}

// PCGWParseFailure records a section parse error for debugging.
type PCGWParseFailure struct {
	ID              string `json:"id"`
	PageID          int64  `json:"page_id"`
	SyncRunID       string `json:"sync_run_id,omitempty"`
	Section         string `json:"section"`
	ErrorMessage    string `json:"error_message"`
	WikitextSnippet string `json:"wikitext_snippet,omitempty"`
	CreatedAt       string `json:"created_at"`
}

// PCGWSyncRun tracks one sync execution (resumable).
type PCGWSyncRun struct {
	ID                string `json:"id"`
	Mode              string `json:"mode"`
	Status            string `json:"status"`
	StartedAt         string `json:"started_at"`
	FinishedAt        string `json:"finished_at,omitempty"`
	CheckpointOffset  int    `json:"checkpoint_offset"`
	GamesTotal        int    `json:"games_total"`
	GamesOK           int    `json:"games_ok"`
	GamesPartial      int    `json:"games_partial"`
	GamesFailed       int    `json:"games_failed"`
	GamesSkipped      int    `json:"games_skipped"`
	AvgParseMs        int    `json:"avg_parse_ms"`
	ErrorMessage      string `json:"error_message,omitempty"`
}

// PCGWManifestMeta is singleton manifest generation state.
type PCGWManifestMeta struct {
	ManifestVersion   int    `json:"manifest_version"`
	ManifestETag      string `json:"manifest_etag"`
	LastIncrementalAt string `json:"last_incremental_at,omitempty"`
	LastFullSyncAt    string `json:"last_full_sync_at,omitempty"`
	DBWikitextBytes   int64  `json:"db_wikitext_bytes"`
}

// ManifestV2Location is a save/config location in manifest v2.
type ManifestV2Location struct {
	Platform         string   `json:"platform"`
	PlatformRawLabel string   `json:"platform_raw_label,omitempty"`
	PathTemplates    []string `json:"path_templates"`
	IsConfig         bool     `json:"is_config"`
	Notes            string   `json:"notes,omitempty"`
}

// ManifestV2Game is one game in manifest v2.
type ManifestV2Game struct {
	GameID               string                 `json:"game_id"`
	PageName             string                 `json:"page_name,omitempty"`
	Title                string                 `json:"title"`
	SteamAppIDs          []string               `json:"steam_appids,omitempty"`
	OtherIDs             map[string]string      `json:"other_ids,omitempty"`
	Platforms            []string               `json:"platforms,omitempty"`
	PlatformsPresent     []string               `json:"platforms_present,omitempty"`
	Taxonomy             map[string]interface{} `json:"taxonomy,omitempty"`
	Engines              []string               `json:"engines,omitempty"`
	HasSaveData          bool                   `json:"has_save_data"`
	CommonInstallPaths   []string               `json:"common_install_paths,omitempty"`
	ProtonSupportLevel   string                 `json:"proton_support_level,omitempty"`
	SaveLocations        []ManifestV2Location   `json:"save_locations,omitempty"`
	ConfigLocations      []ManifestV2Location   `json:"config_locations,omitempty"`
	CloudSync            map[string]interface{} `json:"cloud_sync,omitempty"`
	AvailabilitySummary  map[string]interface{} `json:"availability_summary,omitempty"`
	CoverURL             string                 `json:"cover_url,omitempty"`
	HLTBID               string                 `json:"hltb_id,omitempty"`
	IGDBID               string                 `json:"igdb_id,omitempty"`
	ParseStatus          string                 `json:"parse_status,omitempty"`
	LastUpdated          string                 `json:"last_updated,omitempty"`
	GOGID                string                 `json:"gog_id,omitempty"`
	EpicID               string                 `json:"epic_id,omitempty"`
	UbisoftID            string                 `json:"ubisoft_id,omitempty"`
}

// ManifestV2Response is GET /api/manifest/v2 payload.
type ManifestV2Response struct {
	Version        int              `json:"version"`
	GeneratedAt    string           `json:"generated_at"`
	ETag           string           `json:"etag"`
	Games          []ManifestV2Game `json:"games"`
	DeletedGameIDs []string         `json:"deleted_game_ids,omitempty"`
}
