package types

// SaveEntry identifies a single logical save (game + path key).
type SaveEntry struct {
	GameID      string `json:"game_id"`   // e.g. PCGW page name or Steam App ID
	PathKey     string `json:"path_key"`  // stable key for this path (same across OSes)
	UpdatedAt   string `json:"updated_at,omitempty"`
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

// GameSaveLocation is a single manifest entry: where a game stores saves/config per platform.
type GameSaveLocation struct {
	GameID       string `json:"game_id"`        // e.g. Steam App ID or PCGW page ID
	PCGWPageID   int64  `json:"pcgw_page_id"`   // PCGamingWiki page ID
	GameTitle    string `json:"game_title"`     // human-readable title
	Platform     string `json:"platform"`       // "windows" or "linux"
	PathTemplate string `json:"path_template"`  // placeholder path, e.g. %APPDATA%\EldenRing\<user-id>
	IsConfig     bool   `json:"is_config"`     // config file vs save file
	UpdatedAt    string `json:"updated_at"`
	Source       string `json:"source"`         // e.g. "pcgw"
	Notes        string `json:"notes,omitempty"` // optional attribution or URL (e.g. PCGW page link)
}
