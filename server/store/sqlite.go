package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gsbs/gsbs/pkg/types"
	_ "github.com/mattn/go-sqlite3"
)

type sqliteStore struct {
	db               *sql.DB
	versionRetention int
	saveRoot         string // non-empty enables filesystem storage (GSBS_SAVE_ROOT)
}

// NewSQLite creates a SQLite-backed store.
func NewSQLite(path string) (Store, error) {
	retention := 8
	if s := os.Getenv("GSBS_SAVE_VERSION_RETENTION"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n >= 5 && n <= 10 {
			retention = n
		}
	}
	// WAL mode improves concurrent read performance; single connection avoids "database is locked" under concurrent writes.
	db, err := sql.Open("sqlite3", path+"?_foreign_keys=on&_journal_mode=WAL")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &sqliteStore{db: db, versionRetention: retention, saveRoot: saveRootFromEnv()}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	if os.Getenv("GSBS_MIGRATE_BLOBS_TO_FS") == "1" {
		if err := s.migrateBlobsToFS(); err != nil {
			log.Printf("GSBS: blob-to-filesystem migration failed: %v", err)
		}
	}
	return s, nil
}

func (s *sqliteStore) Close() error {
	return s.db.Close()
}

func (s *sqliteStore) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			username TEXT UNIQUE NOT NULL,
			password_hash TEXT NOT NULL,
			created_at TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS clients (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES users(id),
			name TEXT NOT NULL,
			os TEXT NOT NULL,
			token TEXT UNIQUE,
			last_seen TEXT,
			created_at TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS saves (
			user_id TEXT NOT NULL,
			game_id TEXT NOT NULL,
			path_key TEXT NOT NULL,
			content BLOB NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (user_id, game_id, path_key),
			FOREIGN KEY (user_id) REFERENCES users(id)
		);
		CREATE TABLE IF NOT EXISTS game_save_locations (
			id TEXT PRIMARY KEY,
			game_id TEXT NOT NULL,
			pcgw_page_id INTEGER NOT NULL,
			game_title TEXT NOT NULL,
			platform TEXT NOT NULL,
			path_template TEXT NOT NULL,
			is_config INTEGER NOT NULL,
			updated_at TEXT NOT NULL,
			source TEXT NOT NULL,
			notes TEXT,
			UNIQUE(game_id, platform, path_template)
		);
		CREATE TABLE IF NOT EXISTS job_runs (
			id TEXT PRIMARY KEY,
			job_name TEXT NOT NULL,
			started_at TEXT NOT NULL,
			finished_at TEXT,
			status TEXT NOT NULL,
			error_message TEXT,
			entries_count INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE IF NOT EXISTS manifest_fetches (
			id TEXT PRIMARY KEY,
			client_id TEXT,
			client_name TEXT,
			username TEXT,
			entries_count INTEGER NOT NULL DEFAULT 0,
			fetched_at TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS save_versions (
			user_id TEXT NOT NULL,
			game_id TEXT NOT NULL,
			path_key TEXT NOT NULL,
			version INTEGER NOT NULL,
			content BLOB NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (user_id, game_id, path_key, version),
			FOREIGN KEY (user_id) REFERENCES users(id)
		);
		CREATE INDEX IF NOT EXISTS idx_save_versions_slot ON save_versions(user_id, game_id, path_key);
		CREATE INDEX IF NOT EXISTS idx_clients_token ON clients(token);
		CREATE INDEX IF NOT EXISTS idx_saves_user ON saves(user_id);
		CREATE INDEX IF NOT EXISTS idx_manifest_updated ON game_save_locations(updated_at);
		CREATE INDEX IF NOT EXISTS idx_job_runs_name ON job_runs(job_name, started_at);
		CREATE INDEX IF NOT EXISTS idx_manifest_fetches_at ON manifest_fetches(fetched_at);
	`)
	if err != nil {
		return err
	}
	// Optional column for manifest entries (backward compatible)
	_, err = s.db.Exec(`ALTER TABLE game_save_locations ADD COLUMN notes TEXT`)
	if err != nil && !strings.Contains(err.Error(), "duplicate") {
		return err
	}
	// Role-based admin: users.role 'user' | 'admin'
	_, err = s.db.Exec(`ALTER TABLE users ADD COLUMN role TEXT DEFAULT 'user'`)
	if err != nil && !strings.Contains(err.Error(), "duplicate") {
		return err
	}
	// User management: disabled flag and storage quota
	_, err = s.db.Exec(`ALTER TABLE users ADD COLUMN disabled INTEGER NOT NULL DEFAULT 0`)
	if err != nil && !strings.Contains(err.Error(), "duplicate") {
		return err
	}
	_, err = s.db.Exec(`ALTER TABLE users ADD COLUMN storage_quota_bytes INTEGER`)
	if err != nil && !strings.Contains(err.Error(), "duplicate") {
		return err
	}
	_, err = s.db.Exec(`
		CREATE TABLE IF NOT EXISTS audit_log (
			id TEXT PRIMARY KEY,
			at TEXT NOT NULL,
			actor_user_id TEXT NOT NULL,
			actor_username TEXT NOT NULL,
			action TEXT NOT NULL,
			target_id TEXT,
			details TEXT
		);
		CREATE INDEX IF NOT EXISTS idx_audit_log_at ON audit_log(at);
	`)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`
		CREATE TABLE IF NOT EXISTS stats_snapshots (
			id TEXT PRIMARY KEY,
			at TEXT NOT NULL,
			user_count INTEGER NOT NULL,
			client_count INTEGER NOT NULL,
			save_count INTEGER NOT NULL,
			storage_bytes INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_stats_snapshots_at ON stats_snapshots(at);
	`)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`
		CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES users(id),
			created_at TEXT NOT NULL,
			last_seen TEXT NOT NULL,
			user_agent TEXT
		);
		CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);
	`)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`ALTER TABLE users ADD COLUMN totp_secret TEXT`)
	if err != nil && !strings.Contains(err.Error(), "duplicate") {
		return err
	}
	_, err = s.db.Exec(`ALTER TABLE users ADD COLUMN totp_enabled INTEGER NOT NULL DEFAULT 0`)
	if err != nil && !strings.Contains(err.Error(), "duplicate") {
		return err
	}
	for _, stmt := range []string{
		`ALTER TABLE saves ADD COLUMN content_hash TEXT`,
		`ALTER TABLE saves ADD COLUMN content_size INTEGER`,
		`ALTER TABLE saves ADD COLUMN client_id TEXT`,
		`ALTER TABLE save_versions ADD COLUMN content_hash TEXT`,
		`ALTER TABLE game_save_locations ADD COLUMN steam_app_ids TEXT`,
		`ALTER TABLE game_save_locations ADD COLUMN gog_id TEXT`,
		`ALTER TABLE game_save_locations ADD COLUMN epic_id TEXT`,
		`ALTER TABLE game_save_locations ADD COLUMN ubisoft_id TEXT`,
		`ALTER TABLE game_save_locations ADD COLUMN save_rules_json TEXT`,
		`ALTER TABLE users ADD COLUMN encryption_enabled INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE saves ADD COLUMN encrypted INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE clients ADD COLUMN token_created_at TEXT`,
	} {
		_, err = s.db.Exec(stmt)
		if err != nil && !strings.Contains(err.Error(), "duplicate") {
			return err
		}
	}
	if err := s.migrateTokenHashes(); err != nil {
		return err
	}
	if err := s.migratePCGW(); err != nil {
		return err
	}
	if err := s.migrateSaveFilesystem(); err != nil {
		return err
	}
	_, err = s.db.Exec(`
		CREATE TABLE IF NOT EXISTS admin_settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`)
	if err != nil {
		return err
	}
	return s.seedAdminSettings()
}

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func isTokenHashed(token string) bool {
	if len(token) != 64 {
		return false
	}
	for _, c := range token {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

func tokenMaxAge() time.Duration {
	d := 90 * 24 * time.Hour
	if s := os.Getenv("GSBS_TOKEN_MAX_AGE"); s != "" {
		if parsed, err := time.ParseDuration(s); err == nil && parsed > 0 {
			d = parsed
		}
	}
	return d
}

func (s *sqliteStore) migrateTokenHashes() error {
	_, err := s.db.Exec(`UPDATE clients SET token_created_at = COALESCE(token_created_at, created_at) WHERE token_created_at IS NULL OR token_created_at = ''`)
	if err != nil {
		return err
	}
	rows, err := s.db.Query(`SELECT id, token FROM clients WHERE token IS NOT NULL AND token != ''`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, token string
		if err := rows.Scan(&id, &token); err != nil {
			return err
		}
		if isTokenHashed(token) {
			continue
		}
		if _, err := s.db.Exec(`UPDATE clients SET token = ? WHERE id = ?`, hashToken(token), id); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (s *sqliteStore) CreateUser(ctx context.Context, username, passwordHash string) (string, error) {
	id, err := genID()
	if err != nil {
		return "", err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO users (id, username, password_hash, created_at) VALUES (?, ?, ?, ?)`,
		id, username, passwordHash, time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return "", err
	}
	if err := s.EnsureUserStorage(ctx, id); err != nil {
		return "", err
	}
	return id, nil
}

func (s *sqliteStore) UserByUsername(ctx context.Context, username string) (userID, passwordHash string, err error) {
	err = s.db.QueryRowContext(ctx,
		`SELECT id, password_hash FROM users WHERE username = ?`, username,
	).Scan(&userID, &passwordHash)
	return userID, passwordHash, err
}

// UsernameByID looks up the username for the given user ID (for dashboard and admin auth).
func (s *sqliteStore) UsernameByID(ctx context.Context, userID string) (string, error) {
	var username string
	err := s.db.QueryRowContext(ctx, `SELECT username FROM users WHERE id = ?`, userID).Scan(&username)
	return username, err
}

// UserRole returns the user's role ("user" or "admin"). Defaults to "user" if column missing or empty.
func (s *sqliteStore) UserRole(ctx context.Context, userID string) (string, error) {
	var role string
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(role, 'user') FROM users WHERE id = ?`, userID).Scan(&role)
	if err != nil {
		return "user", err
	}
	if role != "admin" {
		role = "user"
	}
	return role, nil
}

// SetUserRole sets the role for a user ("user" or "admin").
func (s *sqliteStore) SetUserRole(ctx context.Context, userID string, role string) error {
	if role != "user" && role != "admin" {
		role = "user"
	}
	_, err := s.db.ExecContext(ctx, `UPDATE users SET role = ? WHERE id = ?`, role, userID)
	return err
}

// EnsureAdminByUsername sets role to "admin" for the given username (for migration from GSBS_ADMIN_USERNAME).
func (s *sqliteStore) EnsureAdminByUsername(ctx context.Context, username string) error {
	if username == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `UPDATE users SET role = 'admin' WHERE username = ?`, username)
	return err
}

// IsUserDisabled returns true if the user is disabled.
func (s *sqliteStore) IsUserDisabled(ctx context.Context, userID string) (bool, error) {
	var disabled int
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(disabled, 0) FROM users WHERE id = ?`, userID).Scan(&disabled)
	if err != nil {
		return false, err
	}
	return disabled != 0, nil
}

// DisableUser sets the user as disabled.
func (s *sqliteStore) DisableUser(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET disabled = 1 WHERE id = ?`, userID)
	return err
}

// EnableUser clears the disabled flag.
func (s *sqliteStore) EnableUser(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET disabled = 0 WHERE id = ?`, userID)
	return err
}

// DeleteUser removes the user and all their clients, saves, and save_versions.
func (s *sqliteStore) DeleteUser(ctx context.Context, userID string) error {
	if s.filesystemEnabled() {
		rows, err := s.db.QueryContext(ctx, `SELECT storage_path FROM saves WHERE user_id = ? AND storage_path IS NOT NULL AND storage_path != ''`, userID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var p string
			if err := rows.Scan(&p); err != nil {
				rows.Close()
				return err
			}
			removeSaveFile(p)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM save_versions WHERE user_id = ?`, userID); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM saves WHERE user_id = ?`, userID); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM clients WHERE user_id = ?`, userID); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, userID); err != nil {
		return err
	}
	s.removeUserStorageDir(userID)
	return nil
}

// UserQuotaBytes returns the storage quota in bytes for the user (0 = unlimited).
func (s *sqliteStore) UserQuotaBytes(ctx context.Context, userID string) (int64, error) {
	var quota *int64
	err := s.db.QueryRowContext(ctx, `SELECT storage_quota_bytes FROM users WHERE id = ?`, userID).Scan(&quota)
	if err != nil {
		return 0, err
	}
	if quota == nil || *quota == 0 {
		return 0, nil
	}
	return *quota, nil
}

// SetUserQuota sets the storage quota in bytes for the user (0 = unlimited).
func (s *sqliteStore) SetUserQuota(ctx context.Context, userID string, maxBytes int64) error {
	var arg interface{} = maxBytes
	if maxBytes == 0 {
		arg = nil
	}
	_, err := s.db.ExecContext(ctx, `UPDATE users SET storage_quota_bytes = ? WHERE id = ?`, arg, userID)
	return err
}

func (s *sqliteStore) UpdateUserPassword(ctx context.Context, userID string, passwordHash string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, passwordHash, userID)
	return err
}

func (s *sqliteStore) UserPasswordHash(ctx context.Context, userID string) (string, error) {
	var hash string
	err := s.db.QueryRowContext(ctx, `SELECT password_hash FROM users WHERE id = ?`, userID).Scan(&hash)
	return hash, err
}

func (s *sqliteStore) IsTOTPEnabled(ctx context.Context, userID string) (bool, error) {
	var enabled int
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(totp_enabled, 0) FROM users WHERE id = ?`, userID).Scan(&enabled)
	return enabled != 0, err
}

func (s *sqliteStore) GetTOTPSecret(ctx context.Context, userID string) (string, error) {
	var secret sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT totp_secret FROM users WHERE id = ?`, userID).Scan(&secret)
	if err != nil || !secret.Valid {
		return "", err
	}
	return secret.String, nil
}

func (s *sqliteStore) SetTOTPSecret(ctx context.Context, userID string, secret string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET totp_secret = ? WHERE id = ?`, secret, userID)
	return err
}

func (s *sqliteStore) SetTOTPEnabled(ctx context.Context, userID string, enabled bool) error {
	v := 0
	if enabled {
		v = 1
	}
	_, err := s.db.ExecContext(ctx, `UPDATE users SET totp_enabled = ? WHERE id = ?`, v, userID)
	return err
}

func (s *sqliteStore) IsEncryptionEnabled(ctx context.Context, userID string) (bool, error) {
	var enabled int
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(encryption_enabled, 0) FROM users WHERE id = ?`, userID).Scan(&enabled)
	return enabled != 0, err
}

func (s *sqliteStore) SetEncryptionEnabled(ctx context.Context, userID string, enabled bool) error {
	v := 0
	if enabled {
		v = 1
	}
	_, err := s.db.ExecContext(ctx, `UPDATE users SET encryption_enabled = ? WHERE id = ?`, v, userID)
	return err
}

func (s *sqliteStore) CreateSession(ctx context.Context, userID, userAgent string) (string, error) {
	id, err := genID()
	if err != nil {
		return "", err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, user_id, created_at, last_seen, user_agent) VALUES (?, ?, ?, ?, ?)`,
		id, userID, now, now, userAgent,
	)
	return id, err
}

func (s *sqliteStore) GetSessionByID(ctx context.Context, sessionID string) (string, error) {
	var userID string
	err := s.db.QueryRowContext(ctx, `SELECT user_id FROM sessions WHERE id = ?`, sessionID).Scan(&userID)
	if err != nil {
		return "", err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, _ = s.db.ExecContext(ctx, `UPDATE sessions SET last_seen = ? WHERE id = ?`, now, sessionID)
	return userID, nil
}

func (s *sqliteStore) ListSessionsByUser(ctx context.Context, userID string) ([]SessionRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, created_at, last_seen, COALESCE(user_agent, '') FROM sessions WHERE user_id = ? ORDER BY last_seen DESC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SessionRow
	for rows.Next() {
		var r SessionRow
		if err := rows.Scan(&r.ID, &r.CreatedAt, &r.LastSeen, &r.UserAgent); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *sqliteStore) DeleteSession(ctx context.Context, sessionID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, sessionID)
	return err
}

func (s *sqliteStore) DeleteSessionsByUser(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID)
	return err
}

func (s *sqliteStore) DeleteExpiredSessions(ctx context.Context, cutoff time.Time) (int64, error) {
	cutoffStr := cutoff.UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE last_seen < ?`, cutoffStr)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *sqliteStore) RegisterClient(ctx context.Context, userID, name, os string) (string, error) {
	id, err := genID()
	if err != nil {
		return "", err
	}
	token, err := genID()
	if err != nil {
		return "", err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO clients (id, user_id, name, os, token, last_seen, created_at, token_created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, userID, name, os, hashToken(token), now, now, now,
	)
	if err != nil {
		return "", err
	}
	return token, nil
}

func (s *sqliteStore) ClientByToken(ctx context.Context, token string) (userID, clientID, name, os string, err error) {
	var tokenCreatedAt string
	err = s.db.QueryRowContext(ctx,
		`SELECT user_id, id, name, os, COALESCE(token_created_at, created_at) FROM clients WHERE token = ?`, hashToken(token),
	).Scan(&userID, &clientID, &name, &os, &tokenCreatedAt)
	if err != nil {
		return "", "", "", "", err
	}
	if tokenCreatedAt != "" {
		created, parseErr := time.Parse(time.RFC3339, tokenCreatedAt)
		if parseErr == nil && time.Since(created) > tokenMaxAge() {
			return "", "", "", "", fmt.Errorf("token expired")
		}
	}
	return userID, clientID, name, os, nil
}

func (s *sqliteStore) ListClientsByUserID(ctx context.Context, userID string) ([]ClientInfo, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, os, COALESCE(last_seen, created_at) FROM clients WHERE user_id = ? ORDER BY name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ClientInfo
	for rows.Next() {
		var c ClientInfo
		if err := rows.Scan(&c.ID, &c.Name, &c.OS, &c.LastSeen); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// RegenerateClientToken assigns a new token for the client; the previous token stops working (used by admin revoke).
func (s *sqliteStore) RegenerateClientToken(ctx context.Context, clientID string) error {
	newToken, err := genID()
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.ExecContext(ctx, `UPDATE clients SET token = ?, token_created_at = ? WHERE id = ?`, hashToken(newToken), now, clientID)
	return err
}

func (s *sqliteStore) ClientUserID(ctx context.Context, clientID string) (string, error) {
	var userID string
	err := s.db.QueryRowContext(ctx, `SELECT user_id FROM clients WHERE id = ?`, clientID).Scan(&userID)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("client not found")
	}
	return userID, err
}

func (s *sqliteStore) RefreshClientToken(ctx context.Context, currentToken string) (string, error) {
	newToken, err := genID()
	if err != nil {
		return "", err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx, `UPDATE clients SET token = ?, token_created_at = ? WHERE token = ?`, hashToken(newToken), now, hashToken(currentToken))
	if err != nil {
		return "", err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return "", fmt.Errorf("client not found")
	}
	return newToken, nil
}

const saveVersionRetentionDefault = 5

func hashContent(content []byte) string {
	h := sha256.Sum256(content)
	return hex.EncodeToString(h[:])
}

func (s *sqliteStore) UpsertSave(ctx context.Context, userID, gameID, pathKey string, content []byte) error {
	_, err := s.UpsertSaveWithMeta(ctx, userID, gameID, pathKey, content, nil)
	return err
}

func (s *sqliteStore) GetSaveHash(ctx context.Context, userID, gameID, pathKey string) (string, error) {
	var hash sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT content_hash FROM saves WHERE user_id = ? AND game_id = ? AND path_key = ?`,
		userID, gameID, pathKey,
	).Scan(&hash)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return hash.String, nil
}

func (s *sqliteStore) UpsertSaveWithMeta(ctx context.Context, userID, gameID, pathKey string, content []byte, meta *SaveMeta) (skipped bool, err error) {
	retention := s.versionRetention
	if retention < 5 {
		retention = saveVersionRetentionDefault
	}
	contentHash := hashContent(content)
	contentSize := int64(len(content))
	clientID := ""
	encrypted := 0
	relPath := ""
	if meta != nil {
		if meta.ContentHash != "" {
			contentHash = meta.ContentHash
		}
		if meta.ContentSize > 0 {
			contentSize = meta.ContentSize
		}
		clientID = meta.ClientID
		if meta.Encrypted {
			encrypted = 1
		}
		relPath = strings.TrimSpace(meta.RelativePath)
		existing, _ := s.GetSaveHash(ctx, userID, gameID, pathKey)
		if existing != "" && existing == contentHash {
			return true, nil
		}
	}
	if s.filesystemEnabled() {
		if relPath == "" {
			return false, fmt.Errorf("relative path required when GSBS_SAVE_ROOT is set")
		}
	}
	var storagePath string
	var dbContent interface{} = content
	if s.filesystemEnabled() {
		var oldStorage sql.NullString
		_ = s.db.QueryRowContext(ctx,
			`SELECT storage_path FROM saves WHERE user_id = ? AND game_id = ? AND path_key = ?`,
			userID, gameID, pathKey,
		).Scan(&oldStorage)
		storagePath, err = s.writeSaveToFilesystem(ctx, userID, gameID, relPath, content)
		if err != nil {
			return false, err
		}
		if oldStorage.Valid && oldStorage.String != "" && oldStorage.String != storagePath {
			removeSaveFile(oldStorage.String)
		}
		dbContent = nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO saves (user_id, game_id, path_key, content, relative_path, storage_path, updated_at, content_hash, content_size, client_id, encrypted)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(user_id, game_id, path_key) DO UPDATE SET
		   content = excluded.content, relative_path = excluded.relative_path, storage_path = excluded.storage_path,
		   updated_at = excluded.updated_at, content_hash = excluded.content_hash, content_size = excluded.content_size,
		   client_id = excluded.client_id, encrypted = excluded.encrypted`,
		userID, gameID, pathKey, dbContent, nullIfEmpty(relPath), nullIfEmpty(storagePath), now, contentHash, contentSize, nullIfEmpty(clientID), encrypted,
	)
	if err != nil {
		if s.filesystemEnabled() && storagePath != "" {
			removeSaveFile(storagePath)
		}
		return false, err
	}
	var nextVer int
	err = s.db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(version), 0) + 1 FROM save_versions WHERE user_id = ? AND game_id = ? AND path_key = ?`,
		userID, gameID, pathKey,
	).Scan(&nextVer)
	if err != nil {
		return false, err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO save_versions (user_id, game_id, path_key, version, content, updated_at, content_hash)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		userID, gameID, pathKey, nextVer, content, now, contentHash,
	)
	if err != nil {
		return false, err
	}
	var cutoff sql.NullInt64
	_ = s.db.QueryRowContext(ctx,
		`SELECT version FROM save_versions WHERE user_id = ? AND game_id = ? AND path_key = ? ORDER BY version DESC LIMIT 1 OFFSET ?`,
		userID, gameID, pathKey, retention-1,
	).Scan(&cutoff)
	if cutoff.Valid {
		_, _ = s.db.ExecContext(ctx,
			`DELETE FROM save_versions WHERE user_id = ? AND game_id = ? AND path_key = ? AND version < ?`,
			userID, gameID, pathKey, cutoff.Int64,
		)
	}
	return false, nil
}

func nullIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func (s *sqliteStore) ListSaves(ctx context.Context, userID string) ([]types.SaveBlob, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT game_id, path_key, content, storage_path, updated_at, COALESCE(encrypted, 0) FROM saves WHERE user_id = ?`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.SaveBlob
	for rows.Next() {
		var b types.SaveBlob
		var updatedAt string
		var enc int
		var rawContent []byte
		var storagePath sql.NullString
		if err := rows.Scan(&b.GameID, &b.PathKey, &rawContent, &storagePath, &updatedAt, &enc); err != nil {
			return nil, err
		}
		b.Content, err = s.readSaveContent(storagePath, rawContent)
		if err != nil {
			return nil, err
		}
		b.UpdatedAt = updatedAt
		b.Encrypted = enc != 0
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *sqliteStore) ListSavesPaginated(ctx context.Context, userID string, limit, offset int) ([]types.SaveBlob, int, error) {
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM saves WHERE user_id = ?`, userID).Scan(&total); err != nil {
		return nil, 0, err
	}
	if limit <= 0 && offset <= 0 {
		all, err := s.ListSaves(ctx, userID)
		return all, total, err
	}
	if limit <= 0 {
		limit = 1000
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT game_id, path_key, content, storage_path, updated_at, COALESCE(encrypted, 0) FROM saves WHERE user_id = ? ORDER BY updated_at DESC LIMIT ? OFFSET ?`,
		userID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []types.SaveBlob
	for rows.Next() {
		var b types.SaveBlob
		var updatedAt string
		var enc int
		var rawContent []byte
		var storagePath sql.NullString
		if err := rows.Scan(&b.GameID, &b.PathKey, &rawContent, &storagePath, &updatedAt, &enc); err != nil {
			return nil, 0, err
		}
		b.Content, err = s.readSaveContent(storagePath, rawContent)
		if err != nil {
			return nil, 0, err
		}
		b.UpdatedAt = updatedAt
		b.Encrypted = enc != 0
		out = append(out, b)
	}
	return out, total, rows.Err()
}

func (s *sqliteStore) GetSaveContentSize(ctx context.Context, userID, gameID, pathKey string) (int64, error) {
	var contentSize sql.NullInt64
	var contentLen int
	err := s.db.QueryRowContext(ctx,
		`SELECT content_size, COALESCE(LENGTH(content), 0) FROM saves WHERE user_id = ? AND game_id = ? AND path_key = ?`,
		userID, gameID, pathKey,
	).Scan(&contentSize, &contentLen)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if contentSize.Valid && contentSize.Int64 > 0 {
		return contentSize.Int64, nil
	}
	return int64(contentLen), nil
}

func (s *sqliteStore) GetSave(ctx context.Context, userID, gameID, pathKey string) (*types.SaveBlob, error) {
	var b types.SaveBlob
	var updatedAt string
	var enc int
	var rawContent []byte
	var storagePath sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT game_id, path_key, content, storage_path, updated_at, COALESCE(encrypted, 0) FROM saves WHERE user_id = ? AND game_id = ? AND path_key = ?`,
		userID, gameID, pathKey,
	).Scan(&b.GameID, &b.PathKey, &rawContent, &storagePath, &updatedAt, &enc)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	b.Content, err = s.readSaveContent(storagePath, rawContent)
	if err != nil {
		return nil, err
	}
	b.UpdatedAt = updatedAt
	b.Encrypted = enc != 0
	return &b, nil
}

func (s *sqliteStore) DeleteSave(ctx context.Context, userID, gameID, pathKey string) error {
	var storagePath sql.NullString
	_ = s.db.QueryRowContext(ctx,
		`SELECT storage_path FROM saves WHERE user_id = ? AND game_id = ? AND path_key = ?`,
		userID, gameID, pathKey,
	).Scan(&storagePath)
	if storagePath.Valid && storagePath.String != "" {
		removeSaveFile(storagePath.String)
	}
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM saves WHERE user_id = ? AND game_id = ? AND path_key = ?`,
		userID, gameID, pathKey,
	)
	if err != nil {
		return err
	}
	_, _ = s.db.ExecContext(ctx,
		`DELETE FROM save_versions WHERE user_id = ? AND game_id = ? AND path_key = ?`,
		userID, gameID, pathKey,
	)
	return nil
}

func (s *sqliteStore) ListSaveVersions(ctx context.Context, userID, gameID, pathKey string, limit int) ([]SaveVersionInfo, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT version, updated_at, LENGTH(content) FROM save_versions WHERE user_id = ? AND game_id = ? AND path_key = ? ORDER BY version DESC LIMIT ?`,
		userID, gameID, pathKey, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SaveVersionInfo
	for rows.Next() {
		var v SaveVersionInfo
		if err := rows.Scan(&v.Version, &v.UpdatedAt, &v.SizeBytes); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *sqliteStore) GetSaveVersion(ctx context.Context, userID, gameID, pathKey string, version int) (*types.SaveBlob, error) {
	var b types.SaveBlob
	var updatedAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT game_id, path_key, content, updated_at FROM save_versions WHERE user_id = ? AND game_id = ? AND path_key = ? AND version = ?`,
		userID, gameID, pathKey, version,
	).Scan(&b.GameID, &b.PathKey, &b.Content, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	b.UpdatedAt = updatedAt
	return &b, nil
}

func (s *sqliteStore) RestoreSaveVersion(ctx context.Context, userID, gameID, pathKey string, version int) error {
	blob, err := s.GetSaveVersion(ctx, userID, gameID, pathKey, version)
	if err != nil || blob == nil {
		return fmt.Errorf("version not found")
	}
	meta := &SaveMeta{}
	if s.filesystemEnabled() {
		var relPath sql.NullString
		err := s.db.QueryRowContext(ctx,
			`SELECT relative_path FROM saves WHERE user_id = ? AND game_id = ? AND path_key = ?`,
			userID, gameID, pathKey,
		).Scan(&relPath)
		if err == nil && relPath.Valid && relPath.String != "" {
			meta.RelativePath = relPath.String
		} else {
			meta.RelativePath = defaultRelativePath(gameID, pathKey)
		}
	}
	_, err = s.UpsertSaveWithMeta(ctx, userID, gameID, pathKey, blob.Content, meta)
	return err
}

func encodeSteamAppIDs(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	b, _ := json.Marshal(ids)
	return string(b)
}

func decodeSteamAppIDs(s string) []string {
	if s == "" {
		return nil
	}
	var ids []string
	if json.Unmarshal([]byte(s), &ids) == nil {
		return ids
	}
	return nil
}

func encodeSaveRules(rules []types.SaveRule) string {
	if len(rules) == 0 {
		return ""
	}
	b, _ := json.Marshal(rules)
	return string(b)
}

func decodeSaveRules(s string) []types.SaveRule {
	if s == "" {
		return nil
	}
	var rules []types.SaveRule
	if json.Unmarshal([]byte(s), &rules) == nil {
		return rules
	}
	return nil
}

func scanGameSaveLocation(scanner interface {
	Scan(dest ...interface{}) error
}) (types.GameSaveLocation, error) {
	var e types.GameSaveLocation
	var isConfig int
	var steamJSON, gogID, epicID, ubisoftID, saveRulesJSON sql.NullString
	err := scanner.Scan(&e.GameID, &e.PCGWPageID, &e.GameTitle, &e.Platform, &e.PathTemplate, &isConfig, &e.UpdatedAt, &e.Source, &e.Notes, &steamJSON, &gogID, &epicID, &ubisoftID, &saveRulesJSON)
	if err != nil {
		return e, err
	}
	e.IsConfig = isConfig != 0
	if steamJSON.Valid {
		e.SteamAppIDs = decodeSteamAppIDs(steamJSON.String)
	}
	if gogID.Valid {
		e.GOGID = gogID.String
	}
	if epicID.Valid {
		e.EpicID = epicID.String
	}
	if ubisoftID.Valid {
		e.UbisoftID = ubisoftID.String
	}
	if saveRulesJSON.Valid {
		e.SaveRules = decodeSaveRules(saveRulesJSON.String)
	}
	return e, nil
}

const gameSaveLocationSelect = `SELECT game_id, pcgw_page_id, game_title, platform, path_template, is_config, updated_at, source, COALESCE(notes, ''),
	COALESCE(steam_app_ids, ''), COALESCE(gog_id, ''), COALESCE(epic_id, ''), COALESCE(ubisoft_id, ''), COALESCE(save_rules_json, '') FROM game_save_locations`

func (s *sqliteStore) UpsertGameSaveLocations(ctx context.Context, entries []types.GameSaveLocation) error {
	if len(entries) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	now := time.Now().UTC().Format(time.RFC3339)
	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO game_save_locations (id, game_id, pcgw_page_id, game_title, platform, path_template, is_config, updated_at, source, notes, steam_app_ids, gog_id, epic_id, ubisoft_id, save_rules_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(game_id, platform, path_template) DO UPDATE SET
		   pcgw_page_id = excluded.pcgw_page_id,
		   game_title = excluded.game_title,
		   is_config = excluded.is_config,
		   updated_at = excluded.updated_at,
		   source = excluded.source,
		   notes = excluded.notes,
		   steam_app_ids = excluded.steam_app_ids,
		   gog_id = excluded.gog_id,
		   epic_id = excluded.epic_id,
		   ubisoft_id = excluded.ubisoft_id,
		   save_rules_json = excluded.save_rules_json`)
	if err != nil {
		return fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	for _, e := range entries {
		id, err := genID()
		if err != nil {
			return fmt.Errorf("gen id: %w", err)
		}
		isConfig := 0
		if e.IsConfig {
			isConfig = 1
		}
		if e.UpdatedAt == "" {
			e.UpdatedAt = now
		}
		if _, err := stmt.ExecContext(ctx, id, e.GameID, e.PCGWPageID, e.GameTitle, e.Platform, e.PathTemplate, isConfig, e.UpdatedAt, e.Source, e.Notes,
			encodeSteamAppIDs(e.SteamAppIDs), nullIfEmpty(e.GOGID), nullIfEmpty(e.EpicID), nullIfEmpty(e.UbisoftID), nullIfEmpty(encodeSaveRules(e.SaveRules))); err != nil {
			return fmt.Errorf("upsert game_save_location game=%s: %w", e.GameID, err)
		}
	}
	return tx.Commit()
}

func (s *sqliteStore) ListGameSaveLocations(ctx context.Context) ([]types.GameSaveLocation, error) {
	rows, err := s.db.QueryContext(ctx, gameSaveLocationSelect+` ORDER BY game_id, platform`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.GameSaveLocation
	for rows.Next() {
		e, err := scanGameSaveLocation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *sqliteStore) ListGameSaveLocationsPaginated(ctx context.Context, limit, offset int) ([]types.GameSaveLocation, error) {
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.QueryContext(ctx, gameSaveLocationSelect+` ORDER BY game_id, platform LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.GameSaveLocation
	for rows.Next() {
		e, err := scanGameSaveLocation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *sqliteStore) SearchGameSaveLocations(ctx context.Context, query string, limit, offset int) ([]types.GameSaveLocation, int, error) {
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	q := strings.TrimSpace(query)
	if q == "" {
		var total int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM game_save_locations`).Scan(&total); err != nil {
			return nil, 0, err
		}
		entries, err := s.ListGameSaveLocationsPaginated(ctx, limit, offset)
		return entries, total, err
	}
	pattern := "%" + q + "%"
	var total int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM game_save_locations
		WHERE game_title LIKE ? OR game_id LIKE ? OR platform LIKE ? OR path_template LIKE ? OR source LIKE ?`,
		pattern, pattern, pattern, pattern, pattern).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, gameSaveLocationSelect+`
		WHERE game_title LIKE ? OR game_id LIKE ? OR platform LIKE ? OR path_template LIKE ? OR source LIKE ?
		ORDER BY game_title, game_id, platform LIMIT ? OFFSET ?`,
		pattern, pattern, pattern, pattern, pattern, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []types.GameSaveLocation
	for rows.Next() {
		e, err := scanGameSaveLocation(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, e)
	}
	return out, total, rows.Err()
}

func (s *sqliteStore) listGameSaveLocationsForGame(ctx context.Context, gameID string) ([]types.GameSaveLocation, error) {
	rows, err := s.db.QueryContext(ctx, gameSaveLocationSelect+` WHERE game_id = ? ORDER BY platform, path_template`, gameID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.GameSaveLocation
	for rows.Next() {
		e, err := scanGameSaveLocation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *sqliteStore) GetManifestSince(ctx context.Context, since string) ([]types.GameSaveLocation, error) {
	rows, err := s.db.QueryContext(ctx, gameSaveLocationSelect+` WHERE updated_at > ? ORDER BY game_id, platform`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.GameSaveLocation
	for rows.Next() {
		e, err := scanGameSaveLocation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// CountUsers returns the total number of registered users (admin stats).
func (s *sqliteStore) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// CountClients returns the total number of registered clients (admin stats).
func (s *sqliteStore) CountClients(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM clients`).Scan(&n)
	return n, err
}

// CountSaves returns the total number of save blobs across all users (admin stats).
func (s *sqliteStore) CountSaves(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM saves`).Scan(&n)
	return n, err
}

// CountGameSaveLocations returns the number of manifest entries (PCGW game save locations).
func (s *sqliteStore) CountGameSaveLocations(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM game_save_locations`).Scan(&n)
	return n, err
}

// ListUsers returns all users for the admin UI (id, username, created_at).
func (s *sqliteStore) ListUsers(ctx context.Context) ([]UserInfo, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, username, created_at FROM users ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserInfo
	for rows.Next() {
		var u UserInfo
		if err := rows.Scan(&u.ID, &u.Username, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// UpdateClientLastSeen updates the last_seen timestamp for a client (called on push/pull API).
func (s *sqliteStore) UpdateClientLastSeen(ctx context.Context, clientID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE clients SET last_seen = ? WHERE id = ?`,
		time.Now().UTC().Format(time.RFC3339), clientID,
	)
	return err
}

// ListSaveSummaries returns lightweight save info (no content blob) with game title from manifest.
func (s *sqliteStore) ListSaveSummaries(ctx context.Context, userID string) ([]SaveSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT s.game_id, s.path_key, LENGTH(s.content) AS size_bytes, s.updated_at,
		       COALESCE(g.game_title, s.game_id) AS game_title,
		       COALESCE(s.content_hash, '') AS content_hash,
		       COALESCE(s.encrypted, 0) AS encrypted
		FROM saves s
		LEFT JOIN (SELECT DISTINCT game_id, game_title FROM game_save_locations) g
		  ON s.game_id = g.game_id
		WHERE s.user_id = ?
		ORDER BY s.updated_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SaveSummary
	for rows.Next() {
		var ss SaveSummary
		var enc int
		if err := rows.Scan(&ss.GameID, &ss.PathKey, &ss.SizeBytes, &ss.UpdatedAt, &ss.GameTitle, &ss.ContentHash, &enc); err != nil {
			return nil, err
		}
		ss.Encrypted = enc != 0
		out = append(out, ss)
	}
	return out, rows.Err()
}

func (s *sqliteStore) ListSaveSummariesPaginated(ctx context.Context, userID string, limit, offset int) ([]SaveSummary, int, error) {
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM saves WHERE user_id = ?`, userID).Scan(&total); err != nil {
		return nil, 0, err
	}
	if limit <= 0 && offset <= 0 {
		all, err := s.ListSaveSummaries(ctx, userID)
		return all, total, err
	}
	if limit <= 0 {
		limit = 1000
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT s.game_id, s.path_key, LENGTH(s.content) AS size_bytes, s.updated_at,
		       COALESCE(g.game_title, s.game_id) AS game_title,
		       COALESCE(s.content_hash, '') AS content_hash,
		       COALESCE(s.encrypted, 0) AS encrypted
		FROM saves s
		LEFT JOIN (SELECT DISTINCT game_id, game_title FROM game_save_locations) g ON s.game_id = g.game_id
		WHERE s.user_id = ?
		ORDER BY s.updated_at DESC LIMIT ? OFFSET ?`, userID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []SaveSummary
	for rows.Next() {
		var ss SaveSummary
		var enc int
		if err := rows.Scan(&ss.GameID, &ss.PathKey, &ss.SizeBytes, &ss.UpdatedAt, &ss.GameTitle, &ss.ContentHash, &enc); err != nil {
			return nil, 0, err
		}
		ss.Encrypted = enc != 0
		out = append(out, ss)
	}
	return out, total, rows.Err()
}

func (s *sqliteStore) ListSaveSummariesFiltered(ctx context.Context, userID, query string) ([]SaveSummary, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return s.ListSaveSummaries(ctx, userID)
	}
	pattern := "%" + q + "%"
	rows, err := s.db.QueryContext(ctx, `
		SELECT s.game_id, s.path_key, LENGTH(s.content) AS size_bytes, s.updated_at,
		       COALESCE(g.game_title, s.game_id) AS game_title,
		       COALESCE(s.content_hash, '') AS content_hash,
		       COALESCE(s.encrypted, 0) AS encrypted
		FROM saves s
		LEFT JOIN (SELECT DISTINCT game_id, game_title FROM game_save_locations) g
		  ON s.game_id = g.game_id
		WHERE s.user_id = ?
		  AND (s.game_id LIKE ? OR s.path_key LIKE ? OR COALESCE(g.game_title, s.game_id) LIKE ?)
		ORDER BY s.updated_at DESC`, userID, pattern, pattern, pattern)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SaveSummary
	for rows.Next() {
		var ss SaveSummary
		var enc int
		if err := rows.Scan(&ss.GameID, &ss.PathKey, &ss.SizeBytes, &ss.UpdatedAt, &ss.GameTitle, &ss.ContentHash, &enc); err != nil {
			return nil, err
		}
		ss.Encrypted = enc != 0
		out = append(out, ss)
	}
	return out, rows.Err()
}

// UserStorageBytes returns total storage in bytes for a user's saves.
func (s *sqliteStore) UserStorageBytes(ctx context.Context, userID string) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(COALESCE(content_size, LENGTH(content), 0)), 0) FROM saves WHERE user_id = ?`, userID,
	).Scan(&n)
	return n, err
}

// DistinctGameCount returns the number of unique games a user has saves for.
func (s *sqliteStore) DistinctGameCount(ctx context.Context, userID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT game_id) FROM saves WHERE user_id = ?`, userID).Scan(&n)
	return n, err
}

// TotalStorageBytes returns total storage in bytes across all saves (admin stats).
func (s *sqliteStore) TotalStorageBytes(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(COALESCE(content_size, LENGTH(content), 0)), 0) FROM saves`,
	).Scan(&n)
	return n, err
}

// ListUserStats returns all users with per-user aggregate stats (admin page).
func (s *sqliteStore) ListUserStats(ctx context.Context) ([]UserStatRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT u.id, u.username, u.created_at,
		       (SELECT COUNT(*) FROM clients c WHERE c.user_id = u.id) AS client_count,
		       (SELECT COUNT(*) FROM saves s WHERE s.user_id = u.id) AS save_count,
		       (SELECT COALESCE(SUM(COALESCE(s.content_size, LENGTH(s.content), 0)), 0) FROM saves s WHERE s.user_id = u.id) AS storage_bytes,
		       COALESCE(u.storage_quota_bytes, 0), COALESCE(u.disabled, 0)
		FROM users u
		ORDER BY u.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserStatRow
	for rows.Next() {
		var u UserStatRow
		var quotaBytes int64
		var disabled int
		if err := rows.Scan(&u.ID, &u.Username, &u.CreatedAt, &u.ClientCount, &u.SaveCount, &u.StorageBytes, &quotaBytes, &disabled); err != nil {
			return nil, err
		}
		u.QuotaBytes = quotaBytes
		u.Disabled = disabled != 0
		out = append(out, u)
	}
	return out, rows.Err()
}

// ListAllClients returns all clients with their owner username for the admin UI.
func (s *sqliteStore) ListAllClients(ctx context.Context) ([]ClientInfoWithUser, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT c.id, c.user_id, u.username, c.name, c.os, COALESCE(c.last_seen, c.created_at) FROM clients c JOIN users u ON c.user_id = u.id ORDER BY u.username, c.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ClientInfoWithUser
	for rows.Next() {
		var c ClientInfoWithUser
		if err := rows.Scan(&c.ID, &c.UserID, &c.Username, &c.Name, &c.OS, &c.LastSeen); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// LogJobStart records the start of a job run and returns the run ID.
func (s *sqliteStore) LogJobStart(ctx context.Context, jobName string) (string, error) {
	id, err := genID()
	if err != nil {
		return "", err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO job_runs (id, job_name, started_at, status, entries_count) VALUES (?, ?, ?, 'running', 0)`,
		id, jobName, now,
	)
	return id, err
}

// LogJobFinish records the completion of a job run.
func (s *sqliteStore) LogJobFinish(ctx context.Context, runID, status, errorMsg string, entriesCount int) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`UPDATE job_runs SET finished_at = ?, status = ?, error_message = ?, entries_count = ? WHERE id = ?`,
		now, status, errorMsg, entriesCount, runID,
	)
	return err
}

// ListJobRuns returns the most recent runs for a job.
func (s *sqliteStore) ListJobRuns(ctx context.Context, jobName string, limit int) ([]JobRun, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, job_name, started_at, COALESCE(finished_at, ''), status, COALESCE(error_message, ''), entries_count
		 FROM job_runs WHERE job_name = ? ORDER BY started_at DESC LIMIT ?`, jobName, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []JobRun
	for rows.Next() {
		var j JobRun
		if err := rows.Scan(&j.ID, &j.JobName, &j.StartedAt, &j.FinishedAt, &j.Status, &j.ErrorMessage, &j.EntriesCount); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// GetLatestJobRun returns the most recent run for a job, or nil if none.
func (s *sqliteStore) GetLatestJobRun(ctx context.Context, jobName string) (*JobRun, error) {
	var j JobRun
	err := s.db.QueryRowContext(ctx,
		`SELECT id, job_name, started_at, COALESCE(finished_at, ''), status, COALESCE(error_message, ''), entries_count
		 FROM job_runs WHERE job_name = ? ORDER BY started_at DESC LIMIT 1`, jobName,
	).Scan(&j.ID, &j.JobName, &j.StartedAt, &j.FinishedAt, &j.Status, &j.ErrorMessage, &j.EntriesCount)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &j, nil
}

const staleJobMessage = "server restarted while job was running"

// ReconcileStaleJobRuns marks in-flight job_runs as interrupted (e.g. after crash/restart).
func (s *sqliteStore) ReconcileStaleJobRuns(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, job_name, started_at FROM job_runs WHERE status = 'running'`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var stale []JobRun
	for rows.Next() {
		var j JobRun
		if err := rows.Scan(&j.ID, &j.JobName, &j.StartedAt); err != nil {
			return err
		}
		stale = append(stale, j)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(stale) == 0 {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, j := range stale {
		if _, err := s.db.ExecContext(ctx,
			`UPDATE job_runs SET status = ?, finished_at = ?, error_message = ? WHERE id = ?`,
			"interrupted", now, staleJobMessage, j.ID); err != nil {
			return err
		}
		log.Printf("reconcile: job_runs id=%s job=%s started=%s -> interrupted", j.ID, j.JobName, j.StartedAt)
	}
	return nil
}

// HasRunningJob reports whether a job has a row with status running.
func (s *sqliteStore) HasRunningJob(ctx context.Context, jobName string) bool {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM job_runs WHERE job_name = ? AND status = 'running'`, jobName).Scan(&n)
	return err == nil && n > 0
}

// GetLatestSuccessfulJobRun returns the most recent successful run for a job, or nil if none.
func (s *sqliteStore) GetLatestSuccessfulJobRun(ctx context.Context, jobName string) (*JobRun, error) {
	var j JobRun
	err := s.db.QueryRowContext(ctx,
		`SELECT id, job_name, started_at, COALESCE(finished_at, ''), status, COALESCE(error_message, ''), entries_count
		 FROM job_runs WHERE job_name = ? AND status = 'success' ORDER BY started_at DESC LIMIT 1`, jobName,
	).Scan(&j.ID, &j.JobName, &j.StartedAt, &j.FinishedAt, &j.Status, &j.ErrorMessage, &j.EntriesCount)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &j, nil
}

// LogManifestFetch records a manifest download.
func (s *sqliteStore) LogManifestFetch(ctx context.Context, clientID, clientName, username string, entriesCount int) error {
	id, err := genID()
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO manifest_fetches (id, client_id, client_name, username, entries_count, fetched_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		id, clientID, clientName, username, entriesCount, now,
	)
	return err
}

// ListManifestFetches returns the most recent manifest fetches.
func (s *sqliteStore) ListManifestFetches(ctx context.Context, limit int) ([]ManifestFetchRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, COALESCE(client_id, ''), COALESCE(client_name, ''), COALESCE(username, ''), entries_count, fetched_at
		 FROM manifest_fetches ORDER BY fetched_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ManifestFetchRow
	for rows.Next() {
		var f ManifestFetchRow
		if err := rows.Scan(&f.ID, &f.ClientID, &f.ClientName, &f.Username, &f.EntriesCount, &f.FetchedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// AppendAudit adds an audit log entry.
func (s *sqliteStore) AppendAudit(ctx context.Context, actorUserID, actorUsername, action, targetID, details string) error {
	id, err := genID()
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO audit_log (id, at, actor_user_id, actor_username, action, target_id, details) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, time.Now().UTC().Format(time.RFC3339), actorUserID, actorUsername, action, targetID, details)
	return err
}

// ListAuditLog returns the most recent audit entries. sinceID is optional for cursor pagination (returns rows before that id).
func (s *sqliteStore) ListAuditLog(ctx context.Context, limit int, sinceID string) ([]AuditRow, error) {
	if limit <= 0 {
		limit = 50
	}
	var rows *sql.Rows
	var err error
	if sinceID != "" {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, at, actor_user_id, actor_username, action, COALESCE(target_id, ''), COALESCE(details, '') FROM audit_log WHERE id < ? ORDER BY at DESC LIMIT ?`, sinceID, limit)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, at, actor_user_id, actor_username, action, COALESCE(target_id, ''), COALESCE(details, '') FROM audit_log ORDER BY at DESC LIMIT ?`, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditRow
	for rows.Next() {
		var a AuditRow
		if err := rows.Scan(&a.ID, &a.At, &a.ActorUserID, &a.ActorUsername, &a.Action, &a.TargetID, &a.Details); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *sqliteStore) ListAuditLogByUser(ctx context.Context, userID string, limit int) ([]AuditRow, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, at, actor_user_id, actor_username, action, COALESCE(target_id, ''), COALESCE(details, '')
		 FROM audit_log WHERE actor_user_id = ? ORDER BY at DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditRow
	for rows.Next() {
		var a AuditRow
		if err := rows.Scan(&a.ID, &a.At, &a.ActorUserID, &a.ActorUsername, &a.Action, &a.TargetID, &a.Details); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// AppendStatsSnapshot records current counts for the "Stats over time" admin panel.
func (s *sqliteStore) AppendStatsSnapshot(ctx context.Context) error {
	userCount, _ := s.CountUsers(ctx)
	clientCount, _ := s.CountClients(ctx)
	saveCount, _ := s.CountSaves(ctx)
	storageBytes, _ := s.TotalStorageBytes(ctx)
	id, err := genID()
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO stats_snapshots (id, at, user_count, client_count, save_count, storage_bytes) VALUES (?, ?, ?, ?, ?, ?)`,
		id, time.Now().UTC().Format(time.RFC3339), userCount, clientCount, saveCount, storageBytes)
	return err
}

// ListStatsSnapshots returns the most recent stats snapshots.
func (s *sqliteStore) ListStatsSnapshots(ctx context.Context, limit int) ([]StatsSnapshotRow, error) {
	if limit <= 0 {
		limit = 30
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, at, user_count, client_count, save_count, storage_bytes FROM stats_snapshots ORDER BY at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StatsSnapshotRow
	for rows.Next() {
		var r StatsSnapshotRow
		if err := rows.Scan(&r.ID, &r.At, &r.UserCount, &r.ClientCount, &r.SaveCount, &r.StorageBytes); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func genID() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("rand.Read: %w", err)
	}
	return hex.EncodeToString(b), nil
}
