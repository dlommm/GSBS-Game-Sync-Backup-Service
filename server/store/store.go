package store

import (
	"context"
	"time"

	"github.com/gsbs/gsbs/pkg/types"
)

// WebSessionMaxAge is how long an inactive browser session is kept (matches WebUI cookie lifetime).
const WebSessionMaxAge = 7 * 24 * time.Hour

// Store is the persistence layer for users, clients, and saves.
type Store interface {
	// User
	CreateUser(ctx context.Context, username, passwordHash string) (userID string, err error)
	UserByUsername(ctx context.Context, username string) (userID, passwordHash string, err error)
	// UsernameByID returns the username for a user ID (e.g. for dashboard header and admin check).
	UsernameByID(ctx context.Context, userID string) (username string, err error)
	// UserRole returns the user's role ("user" or "admin"). Returns "user" if not set.
	UserRole(ctx context.Context, userID string) (role string, err error)
	// SetUserRole sets the role for a user (e.g. "admin"). Used for role-based access.
	SetUserRole(ctx context.Context, userID string, role string) error
	// EnsureAdminByUsername sets role to "admin" for the given username (for migration from GSBS_ADMIN_USERNAME).
	EnsureAdminByUsername(ctx context.Context, username string) error
	// IsUserDisabled returns true if the user is disabled (cannot log in).
	IsUserDisabled(ctx context.Context, userID string) (bool, error)
	// DisableUser sets the user as disabled (cannot log in).
	DisableUser(ctx context.Context, userID string) error
	// EnableUser clears the disabled flag.
	EnableUser(ctx context.Context, userID string) error
	// DeleteUser removes the user and all their clients, saves, and save_versions. Use with care.
	DeleteUser(ctx context.Context, userID string) error
	// UserQuotaBytes returns the storage quota in bytes for the user (0 = unlimited).
	UserQuotaBytes(ctx context.Context, userID string) (int64, error)
	// SetUserQuota sets the storage quota in bytes for the user (0 = unlimited).
	SetUserQuota(ctx context.Context, userID string, maxBytes int64) error
	// UpdateUserPassword sets the user's password hash (for password change).
	UpdateUserPassword(ctx context.Context, userID string, passwordHash string) error
	// UserPasswordHash returns the password hash for the user (for password verification). Returns empty if not found.
	UserPasswordHash(ctx context.Context, userID string) (string, error)
	// TOTP (optional 2FA): secret stored per user; empty = 2FA disabled.
	IsTOTPEnabled(ctx context.Context, userID string) (bool, error)
	GetTOTPSecret(ctx context.Context, userID string) (string, error)
	SetTOTPSecret(ctx context.Context, userID string, secret string) error
	SetTOTPEnabled(ctx context.Context, userID string, enabled bool) error
	// IsEncryptionEnabled reports whether the user has E2E encryption enabled.
	IsEncryptionEnabled(ctx context.Context, userID string) (bool, error)
	// SetEncryptionEnabled toggles E2E encryption for the user.
	SetEncryptionEnabled(ctx context.Context, userID string, enabled bool) error

	// Web sessions (cookie-backed; session ID stored in signed cookie, row in DB)
	CreateSession(ctx context.Context, userID, userAgent string) (sessionID string, err error)
	// GetSessionByID returns the userID for the session and updates last_seen. Returns empty if session not found or invalid.
	GetSessionByID(ctx context.Context, sessionID string) (userID string, err error)
	ListSessionsByUser(ctx context.Context, userID string) ([]SessionRow, error)
	DeleteSession(ctx context.Context, sessionID string) error
	DeleteSessionsByUser(ctx context.Context, userID string) error
	// DeleteExpiredSessions removes web sessions with last_seen before cutoff.
	DeleteExpiredSessions(ctx context.Context, cutoff time.Time) (int64, error)

	// Client
	RegisterClient(ctx context.Context, userID, name, os string) (clientID string, err error)
	ClientByToken(ctx context.Context, token string) (userID, clientID, name, os string, err error)
	ListClientsByUserID(ctx context.Context, userID string) ([]ClientInfo, error)
	// RegenerateClientToken issues a new token for the client; the old token is invalidated (client must re-login).
	RegenerateClientToken(ctx context.Context, clientID string) error
	// ClientUserID returns the owning user ID for a client, or error if not found.
	ClientUserID(ctx context.Context, clientID string) (string, error)
	// RefreshClientToken rotates token for the authenticated client; old token stops working.
	RefreshClientToken(ctx context.Context, currentToken string) (newToken string, err error)
	// RevokeAllClientTokens regenerates tokens for every client owned by the user,
	// invalidating all existing tokens (used after password or 2FA changes).
	RevokeAllClientTokens(ctx context.Context, userID string) error
	// UpdateClientLastSeen updates the last_seen timestamp for a client (called on push/pull).
	UpdateClientLastSeen(ctx context.Context, clientID string) error

	// Save
	UpsertSave(ctx context.Context, userID, gameID, pathKey string, content []byte) error
	// UpsertSaveWithMeta upserts with optional hash/size/client; skips write if hash matches existing.
	UpsertSaveWithMeta(ctx context.Context, userID, gameID, pathKey string, content []byte, meta *SaveMeta) (skipped bool, err error)
	GetSaveHash(ctx context.Context, userID, gameID, pathKey string) (hash string, err error)
	// GetSaveHashAndVersion returns the current content hash and latest version number for a save slot.
	// Returns ("", 0, nil) when no save exists yet.
	GetSaveHashAndVersion(ctx context.Context, userID, gameID, pathKey string) (hash string, version int, err error)
	ListSaves(ctx context.Context, userID string) ([]types.SaveBlob, error)
	// ListSavesPaginated returns a page of saves and total count. limit/offset 0 means no pagination (returns all).
	ListSavesPaginated(ctx context.Context, userID string, limit, offset int) ([]types.SaveBlob, int, error)
	GetSave(ctx context.Context, userID, gameID, pathKey string) (*types.SaveBlob, error)
	// GetSaveContentSize returns stored bytes for an existing save slot, or 0 if none.
	GetSaveContentSize(ctx context.Context, userID, gameID, pathKey string) (int64, error)
	DeleteSave(ctx context.Context, userID, gameID, pathKey string) error
	// Save versioning (last N versions per slot; retention policy applied on upsert)
	ListSaveVersions(ctx context.Context, userID, gameID, pathKey string, limit int) ([]SaveVersionInfo, error)
	GetSaveVersion(ctx context.Context, userID, gameID, pathKey string, version int) (*types.SaveBlob, error)
	RestoreSaveVersion(ctx context.Context, userID, gameID, pathKey string, version int) error

	// ListSaveSummaries returns lightweight save info (no content blob) with game title from manifest.
	ListSaveSummaries(ctx context.Context, userID string) ([]SaveSummary, error)
	// ListSaveSummariesFiltered returns saves matching query (game title, game_id, path_key).
	ListSaveSummariesFiltered(ctx context.Context, userID, query string) ([]SaveSummary, error)
	// ListSaveSummariesPaginated returns a page of summaries and total count. limit/offset 0 means no pagination.
	ListSaveSummariesPaginated(ctx context.Context, userID string, limit, offset int) ([]SaveSummary, int, error)
	// UserStorageBytes returns total bytes of save content for a user.
	UserStorageBytes(ctx context.Context, userID string) (int64, error)
	// DistinctGameCount returns number of unique games with saves for a user.
	DistinctGameCount(ctx context.Context, userID string) (int, error)

	// Game save locations (manifest from PCGW)
	UpsertGameSaveLocations(ctx context.Context, entries []types.GameSaveLocation) error
	ListGameSaveLocations(ctx context.Context) ([]types.GameSaveLocation, error)
	ListGameSaveLocationsPaginated(ctx context.Context, limit, offset int) ([]types.GameSaveLocation, error)
	// SearchGameSaveLocations searches manifest entries by game title, ID, platform, or path template.
	SearchGameSaveLocations(ctx context.Context, query string, limit, offset int) ([]types.GameSaveLocation, int, error)
	GetManifestSince(ctx context.Context, since string) ([]types.GameSaveLocation, error)

	// Admin / stats (used by WebUI admin page)
	CountUsers(ctx context.Context) (int, error)
	CountClients(ctx context.Context) (int, error)
	CountSaves(ctx context.Context) (int, error)
	CountGameSaveLocations(ctx context.Context) (int, error)
	TotalStorageBytes(ctx context.Context) (int64, error)
	ListUsers(ctx context.Context) ([]UserInfo, error)                // All users for admin listing
	ListUserStats(ctx context.Context) ([]UserStatRow, error)         // All users with per-user stats
	ListAllClients(ctx context.Context) ([]ClientInfoWithUser, error) // All clients with owner username

	// Job tracking (admin jobs dashboard)
	LogJobStart(ctx context.Context, jobName string) (runID string, err error)
	LogJobFinish(ctx context.Context, runID, status, errorMsg string, entriesCount int) error
	ListJobRuns(ctx context.Context, jobName string, limit int) ([]JobRun, error)
	GetLatestJobRun(ctx context.Context, jobName string) (*JobRun, error)
	GetLatestSuccessfulJobRun(ctx context.Context, jobName string) (*JobRun, error)
	ReconcileStaleJobRuns(ctx context.Context) error
	HasRunningJob(ctx context.Context, jobName string) bool

	// Manifest fetch tracking (admin manifest fetch log)
	LogManifestFetch(ctx context.Context, clientID, clientName, username string, entriesCount int) error
	ListManifestFetches(ctx context.Context, limit int) ([]ManifestFetchRow, error)

	// Audit log (admin actions and sensitive user actions)
	AppendAudit(ctx context.Context, actorUserID, actorUsername, action, targetID, details string) error
	ListAuditLog(ctx context.Context, limit int, sinceID string) ([]AuditRow, error)
	// ListAuditLogByUser returns recent audit entries for a specific actor username.
	ListAuditLogByUser(ctx context.Context, userID string, limit int) ([]AuditRow, error)

	// Stats snapshots (time-series for admin)
	AppendStatsSnapshot(ctx context.Context) error
	ListStatsSnapshots(ctx context.Context, limit int) ([]StatsSnapshotRow, error)

	// PCGW games (full mirror)
	UpsertPCGWGame(ctx context.Context, g *types.PCGWGame) error
	GetPCGWGame(ctx context.Context, pageID int64) (*types.PCGWGame, error)
	ListPCGWGames(ctx context.Context, filter PCGWGameListFilter) ([]types.PCGWGame, int, error)
	SearchPCGWGamesFTS(ctx context.Context, query string, limit, offset int) ([]types.PCGWGame, int, error)

	UpsertPCGWGameData(ctx context.Context, row *types.PCGWGameData) error
	DeletePCGWGameDataExcept(ctx context.Context, pageID int64, keepPlatformKeys []string) error
	ListPCGWGameData(ctx context.Context, pageID int64) ([]types.PCGWGameData, error)

	UpsertPCGWAvailability(ctx context.Context, row *types.PCGWSectionRow) error
	UpsertPCGWMonetization(ctx context.Context, row *types.PCGWSectionRow) error
	UpsertPCGWVideo(ctx context.Context, row *types.PCGWSectionRow) error
	UpsertPCGWInput(ctx context.Context, row *types.PCGWSectionRow) error
	UpsertPCGWAudio(ctx context.Context, row *types.PCGWSectionRow) error
	UpsertPCGWNetwork(ctx context.Context, row *types.PCGWSectionRow) error
	UpsertPCGWOther(ctx context.Context, row *types.PCGWSectionRow) error
	UpsertPCGWNotes(ctx context.Context, row *types.PCGWSectionRow) error
	UpsertPCGWReferences(ctx context.Context, row *types.PCGWSectionRow) error
	UpsertPCGWExternalLinks(ctx context.Context, row *types.PCGWSectionRow) error
	GetPCGWSection(ctx context.Context, pageID int64, section string) (*types.PCGWSectionRow, error)

	ReplacePCGWSystemRequirements(ctx context.Context, pageID int64, rows []types.PCGWSystemRequirement) error
	ListPCGWSystemRequirements(ctx context.Context, pageID int64) ([]types.PCGWSystemRequirement, error)

	UpsertPCGWMetadata(ctx context.Context, m *types.PCGWMetadata) error
	GetPCGWMetadata(ctx context.Context, pageID int64) (*types.PCGWMetadata, error)
	GetPCGWContentHash(ctx context.Context, pageID int64) (contentHash string, sectionHashes map[string]string, err error)
	PurgePCGWFullWikitext(ctx context.Context) (rowsAffected int64, err error)

	InsertPCGWParseFailure(ctx context.Context, f *types.PCGWParseFailure) error
	ListPCGWParseFailures(ctx context.Context, pageID int64, limit int) ([]types.PCGWParseFailure, error)
	// CountPCGWParseFailures returns total rows in pcgw_parse_failures (admin analytics).
	CountPCGWParseFailures(ctx context.Context) (int, error)

	StartPCGWSyncRun(ctx context.Context, mode string) (runID string, err error)
	StartPCGWSyncRunWithResume(ctx context.Context, mode, resumedFromRunID, notes string) (runID string, err error)
	UpdatePCGWSyncRunCheckpoint(ctx context.Context, runID string, offset int, stats PCGWSyncRunStats) error
	FinishPCGWSyncRun(ctx context.Context, runID, status, errMsg string, stats PCGWSyncRunStats) error
	GetLatestPCGWSyncRun(ctx context.Context) (*types.PCGWSyncRun, error)
	GetPCGWSyncRunByID(ctx context.Context, runID string) (*types.PCGWSyncRun, error)
	// ListPCGWSyncRuns returns recent sync runs, newest first.
	ListPCGWSyncRuns(ctx context.Context, limit int) ([]types.PCGWSyncRun, error)
	GetResumablePCGWSyncRun(ctx context.Context, mode string) (*types.PCGWSyncRun, error)
	ReconcileStalePCGWSyncRuns(ctx context.Context) error
	HasRunningPCGWSync(ctx context.Context) bool
	CancelRunningPCGWSyncRuns(ctx context.Context, errMsg string) error

	// PCGW catalog (two-phase sync)
	UpsertPCGWCatalogBatch(ctx context.Context, entries []types.PCGWCatalogEntry) error
	GetPCGWCatalogStats(ctx context.Context) (types.PCGWCatalogStats, error)
	ListPCGWCatalogMissing(ctx context.Context, limit, offset int) ([]int64, error)
	ListPCGWCatalogFailedPartial(ctx context.Context, limit, offset int) ([]int64, error)
	ListPCGWCatalogTitleBackfill(ctx context.Context, limit, offset int) ([]types.PCGWCatalogEntry, error)
	ListPCGWCatalogDeadLetter(ctx context.Context, limit int) ([]types.PCGWCatalogEntry, error)
	IncrementCatalogRetry(ctx context.Context, pageID int64, reason string) error
	ClearCatalogDeadLetter(ctx context.Context, pageID int64) error
	ResetPCGWDeadLetter(ctx context.Context) (int64, error)
	ComputeCatalogHash(ctx context.Context) (string, error)
	// UpdatePCGWSyncRunPhase1Stats persists Phase 1 stats. catalogScanMode: "full", "fast_probe", "tail", "skipped", "resumed".
	UpdatePCGWSyncRunPhase1Stats(ctx context.Context, runID string, stats types.Phase1Stats, catalogScanMode string) error
	UpdatePCGWSyncRunPhase2Progress(ctx context.Context, runID string, processed, cursor int) error
	// GetLastSuccessfulPhase1Stats returns Phase 1 stats from the most recent successful run. Returns nil if none.
	GetLastSuccessfulPhase1Stats(ctx context.Context) (*types.Phase1Stats, error)
	// SetLastRevCheckAt records when the rev-ID check (buildChangedQueue) last ran.
	SetLastRevCheckAt(ctx context.Context, t time.Time) error
	// UpdateLastFullSyncAt records the time of the most recent full catalog scan.
	UpdateLastFullSyncAt(ctx context.Context) error
	// Wipe operations
	WipePCGWMirrorOnly(ctx context.Context) error
	WipePCGWMirrorAndManifest(ctx context.Context) error
	GetPCGWWipePreflightCounts(ctx context.Context) (types.WipePreflightCounts, error)

	GetPCGWManifestMeta(ctx context.Context) (*types.PCGWManifestMeta, error)
	BumpManifestVersion(ctx context.Context, newETag string) (version int, err error)
	ReplaceGameSaveLocationsForGame(ctx context.Context, gameID string, entries []types.GameSaveLocation) error
	BuildManifestV2(ctx context.Context, since, platform string, limit, offset int) (*types.ManifestV2Response, error)

	GetPCGWStats(ctx context.Context) (PCGWStats, error)
	ExportPCGWGameJSON(ctx context.Context, pageID int64) ([]byte, error)
	ExportPCGWManifestBundle(ctx context.Context, gsbsVersion string) ([]byte, error)
	ImportPCGWManifestBundle(ctx context.Context, data []byte, mode string) (PCGWImportResult, error)
	ValidatePCGWImport(ctx context.Context) (PCGWImportValidation, error)

	// Admin settings (key/value, persisted cron and PCGW filters).
	GetAdminSetting(ctx context.Context, key string) (string, error)
	SetAdminSetting(ctx context.Context, key, value string) error
	ListAdminSettings(ctx context.Context) (map[string]string, error)

	// Analytics queries for admin dashboard.
	CountActiveClientsSince(ctx context.Context, since time.Time) (int, error)
	CountSyncVolume7d(ctx context.Context) (int, error)
	CountDistinctManifestGames(ctx context.Context) (int, error)
	CountDistinctSaveGames(ctx context.Context) (int, error)
	CountTotalSaves(ctx context.Context) (int, error)
	ListTopSaveGames(ctx context.Context, limit int) ([]SaveGameStatRow, error)
	ListRecentPCGWParseFailures(ctx context.Context, limit int) ([]PCGWParseFailureRow, error)

	// Close releases resources (e.g. DB connection).
	Close() error
}

// SaveRecord is the internal save row.
type SaveRecord struct {
	UserID    string
	GameID    string
	PathKey   string
	Content   []byte
	UpdatedAt time.Time
}

// SaveMeta optional metadata for upsert (hash dedup, client tracking).
type SaveMeta struct {
	ContentHash  string
	ContentSize  int64
	ClientID     string
	Encrypted    bool
	RelativePath string // validated client-relative path when GSBS_SAVE_ROOT is set
}

type ClientInfo struct {
	ID       string
	Name     string
	OS       string
	LastSeen string
}

// SaveSummary is a lightweight save entry for dashboard display (no content blob).
type SaveSummary struct {
	GameID      string
	PathKey     string
	GameTitle   string // from game_save_locations join; falls back to game_id
	SizeBytes   int64
	UpdatedAt   string
	ContentHash string
	Encrypted   bool
}

// UserInfo is a user row for admin listing.
type UserInfo struct {
	ID        string
	Username  string
	CreatedAt string
}

// UserStatRow is a user row with aggregate stats for admin display.
type UserStatRow struct {
	ID           string
	Username     string
	CreatedAt    string
	ClientCount  int
	SaveCount    int
	StorageBytes int64
	QuotaBytes   int64 // 0 = unlimited
	Disabled     bool
}

// ClientInfoWithUser is a client with owning username for admin listing.
type ClientInfoWithUser struct {
	ID       string
	UserID   string
	Username string
	Name     string
	OS       string
	LastSeen string
}

// JobRun represents one execution of a background job (e.g. PCGW sync).
type JobRun struct {
	ID           string
	JobName      string
	StartedAt    string
	FinishedAt   string // empty while running
	Status       string // "running", "success", "failed"
	ErrorMessage string
	EntriesCount int
}

// SaveVersionInfo is a versioned save entry (no content).
type SaveVersionInfo struct {
	Version   int    `json:"version"`
	UpdatedAt string `json:"updated_at"`
	SizeBytes int64  `json:"size_bytes"`
}

// ManifestFetchRow records a single manifest download by a client.
type ManifestFetchRow struct {
	ID           string
	ClientID     string // empty for unauthenticated
	ClientName   string
	Username     string
	EntriesCount int
	FetchedAt    string
}

// AuditRow is one audit log entry.
type AuditRow struct {
	ID            string
	At            string
	ActorUserID   string
	ActorUsername string
	Action        string
	TargetID      string
	Details       string
}

// StatsSnapshotRow is one point-in-time stats row for the admin "Stats over time" panel.
type StatsSnapshotRow struct {
	ID           string
	At           string
	UserCount    int
	ClientCount  int
	SaveCount    int
	StorageBytes int64
}

// SessionRow is one Web session (browser) for the user.
type SessionRow struct {
	ID        string
	CreatedAt string
	LastSeen  string
	UserAgent string
}

// SaveGameStatRow is aggregate save stats for one game (admin analytics).
type SaveGameStatRow struct {
	GameID       string
	GameTitle    string
	SaveCount    int
	StorageBytes int64
}

// PCGWParseFailureRow is a parse failure with optional game title for admin lists.
type PCGWParseFailureRow struct {
	types.PCGWParseFailure
	GameTitle string
}

// PCGWGameListFilter filters ListPCGWGames.
type PCGWGameListFilter struct {
	ParseStatus  string
	Platform     string
	SteamAppID   string
	UpdatedAfter string
	Limit        int
	Offset       int
}

// PCGWSyncRunStats aggregates sync progress counters.
type PCGWSyncRunStats struct {
	GamesTotal   int
	GamesOK      int
	GamesPartial int
	GamesFailed  int
	GamesSkipped int
	AvgParseMs   int
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
}

// PCGWImportValidation is post-import row counts and sample checks.
type PCGWImportValidation struct {
	GameSaveLocations int
	PCGWGames         int
	PCGWGameData      int
	SampleOK          bool
	Errors            []string
}

// PCGWStats is admin dashboard summary for PCGW data.
type PCGWStats struct {
	TotalGames      int
	OK              int
	Partial         int
	Failed          int
	Pending         int
	LastSyncAt      string
	AvgParseMs      int
	DBWikitextBytes int64
	ManifestVersion int
}
