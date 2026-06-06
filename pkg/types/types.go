package types

// SaveEntry identifies a single logical save (game + path key).
type SaveEntry struct {
	GameID      string `json:"game_id"`  // e.g. PCGW page name or Steam App ID
	PathKey     string `json:"path_key"` // save slot key: PCGW-sourced = OS-neutral (derived from game_id+slot_label+is_config); user-defined = per-OS hash
	UpdatedAt   string `json:"updated_at,omitempty"`
	Encrypted   bool   `json:"encrypted,omitempty"`
	ContentHash string `json:"content_hash,omitempty"` // SHA256 hex of content
	ContentSize int64  `json:"content_size,omitempty"`
}

// SaveBlob is a save file plus metadata for push/pull.
type SaveBlob struct {
	SaveEntry
	Content []byte `json:"-"` // raw file content
}

// PushRequest is the client request to upload a save.
type PushRequest struct {
	GameID   string `json:"game_id"`
	PathKey  string `json:"path_key"`
	FilePath string `json:"file_path"` // logical path for logging
	Content  []byte `json:"-"`
}

// PullResponse is the server response for "list my saves" / "pull all".
type PullResponse struct {
	Saves []SaveBlob `json:"saves"`
}

// SaveRule describes what to sync under a resolved directory.
type SaveRule struct {
	Directory       string   `json:"directory"`
	IncludePatterns []string `json:"include_patterns,omitempty"`
	Recursive       bool     `json:"recursive,omitempty"`
	Platform        string   `json:"platform,omitempty"`
	IsConfig        bool     `json:"is_config"`
	SyncAll         bool     `json:"sync_all,omitempty"` // explicit opt-in when IncludePatterns is empty
	// SlotLabel is an OS-neutral identifier for this save slot, assigned during PCGW ingest.
	// When non-empty, path_key is derived from (game_id, slot_label, is_config) instead of the
	// full rule — making the key identical across Windows and Linux for the same logical save.
	// Format: "<slot_index>" (e.g. "0", "1") — the 0-based index of the logical save slot
	// within the game, as assigned by the server PCGW ingest step.
	// Empty for user-defined rules (they keep the legacy hash-of-full-rule key).
	SlotLabel string `json:"slot_label,omitempty"`
}

// GameSaveLocation is a single manifest entry: where a game stores saves/config per platform.
type GameSaveLocation struct {
	GameID       string     `json:"game_id"`       // PCGW page ID (canonical save key)
	PCGWPageID   int64      `json:"pcgw_page_id"`  // PCGamingWiki page ID
	GameTitle    string     `json:"game_title"`    // human-readable title
	Platform     string     `json:"platform"`      // "windows" or "linux"
	PathTemplate string     `json:"path_template"` // placeholder path, e.g. %APPDATA%\EldenRing\<user-id>
	IsConfig     bool       `json:"is_config"`     // config file vs save file
	UpdatedAt    string     `json:"updated_at"`
	Source       string     `json:"source"`          // e.g. "pcgw"
	Notes        string     `json:"notes,omitempty"` // optional attribution or URL (e.g. PCGW page link)
	SteamAppIDs  []string   `json:"steam_app_ids,omitempty"`
	GOGID        string     `json:"gog_id,omitempty"`
	EpicID       string     `json:"epic_id,omitempty"`
	UbisoftID    string     `json:"ubisoft_id,omitempty"`
	SaveRules    []SaveRule `json:"save_rules,omitempty"`
}
