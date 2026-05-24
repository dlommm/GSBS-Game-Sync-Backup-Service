package store

import (
	"context"
	"time"

	"github.com/gsbs/gsbs/pkg/types"
)

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

	// Web sessions (cookie-backed; session ID stored in signed cookie, row in DB)
	CreateSession(ctx context.Context, userID, userAgent string) (sessionID string, err error)
	// GetSessionByID returns the userID for the session and updates last_seen. Returns empty if session not found or invalid.
	GetSessionByID(ctx context.Context, sessionID string) (userID string, err error)
	ListSessionsByUser(ctx context.Context, userID string) ([]SessionRow, error)
	DeleteSession(ctx context.Context, sessionID string) error
	DeleteSessionsByUser(ctx context.Context, userID string) error

	// Client
	RegisterClient(ctx context.Context, userID, name, os string) (clientID string, err error)
	ClientByToken(ctx context.Context, token string) (userID, clientID, name, os string, err error)
	ListClientsByUserID(ctx context.Context, userID string) ([]ClientInfo, error)
	// RegenerateClientToken issues a new token for the client; the old token is invalidated (client must re-login).
	RegenerateClientToken(ctx context.Context, clientID string) error
	// RefreshClientToken rotates token for the authenticated client; old token stops working.
	RefreshClientToken(ctx context.Context, currentToken string) (newToken string, err error)
	// UpdateClientLastSeen updates the last_seen timestamp for a client (called on push/pull).
	UpdateClientLastSeen(ctx context.Context, clientID string) error

	// Save
	UpsertSave(ctx context.Context, userID, gameID, pathKey string, content []byte) error
	// UpsertSaveWithMeta upserts with optional hash/size/client; skips write if hash matches existing.
	UpsertSaveWithMeta(ctx context.Context, userID, gameID, pathKey string, content []byte, meta *SaveMeta) (skipped bool, err error)
	GetSaveHash(ctx context.Context, userID, gameID, pathKey string) (hash string, err error)
	ListSaves(ctx context.Context, userID string) ([]types.SaveBlob, error)
	// ListSavesPaginated returns a page of saves and total count. limit/offset 0 means no pagination (returns all).
	ListSavesPaginated(ctx context.Context, userID string, limit, offset int) ([]types.SaveBlob, int, error)
	GetSave(ctx context.Context, userID, gameID, pathKey string) (*types.SaveBlob, error)
	DeleteSave(ctx context.Context, userID, gameID, pathKey string) error
	// Save versioning (last N versions per slot; retention policy applied on upsert)
	ListSaveVersions(ctx context.Context, userID, gameID, pathKey string, limit int) ([]SaveVersionInfo, error)
	GetSaveVersion(ctx context.Context, userID, gameID, pathKey string, version int) (*types.SaveBlob, error)
	RestoreSaveVersion(ctx context.Context, userID, gameID, pathKey string, version int) error

	// ListSaveSummaries returns lightweight save info (no content blob) with game title from manifest.
	ListSaveSummaries(ctx context.Context, userID string) ([]SaveSummary, error)
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
	GetManifestSince(ctx context.Context, since string) ([]types.GameSaveLocation, error)

	// Admin / stats (used by WebUI admin page)
	CountUsers(ctx context.Context) (int, error)
	CountClients(ctx context.Context) (int, error)
	CountSaves(ctx context.Context) (int, error)
	CountGameSaveLocations(ctx context.Context) (int, error)
	TotalStorageBytes(ctx context.Context) (int64, error)
	ListUsers(ctx context.Context) ([]UserInfo, error)                 // All users for admin listing
	ListUserStats(ctx context.Context) ([]UserStatRow, error)          // All users with per-user stats
	ListAllClients(ctx context.Context) ([]ClientInfoWithUser, error)  // All clients with owner username

	// Job tracking (admin jobs dashboard)
	LogJobStart(ctx context.Context, jobName string) (runID string, err error)
	LogJobFinish(ctx context.Context, runID, status, errorMsg string, entriesCount int) error
	ListJobRuns(ctx context.Context, jobName string, limit int) ([]JobRun, error)
	GetLatestJobRun(ctx context.Context, jobName string) (*JobRun, error)

	// Manifest fetch tracking (admin manifest fetch log)
	LogManifestFetch(ctx context.Context, clientID, clientName, username string, entriesCount int) error
	ListManifestFetches(ctx context.Context, limit int) ([]ManifestFetchRow, error)

	// Audit log (admin actions and sensitive user actions)
	AppendAudit(ctx context.Context, actorUserID, actorUsername, action, targetID, details string) error
	ListAuditLog(ctx context.Context, limit int, sinceID string) ([]AuditRow, error)

	// Stats snapshots (time-series for admin)
	AppendStatsSnapshot(ctx context.Context) error
	ListStatsSnapshots(ctx context.Context, limit int) ([]StatsSnapshotRow, error)

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
	ContentHash string
	ContentSize int64
	ClientID    string
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
