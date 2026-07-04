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
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gsbs/gsbs/pkg/types"
	"github.com/gsbs/gsbs/server/logx"
	_ "github.com/mattn/go-sqlite3"
)

type sqliteStore struct {
	db               *sql.DB
	versionRetention int
	saveRoot         string // non-empty enables filesystem storage (GSBS_SAVE_ROOT)
	dbPath           string // original path, used to skip migration sleep for :memory: DBs

	// Lazily-loaded at-rest encryption key for the TOTP column (secretbox.go).
	totpKeyOnce  sync.Once
	totpKeyBytes []byte
	totpKeyErr   error
}

// NewSQLite creates a SQLite-backed store.
func NewSQLite(path string) (Store, error) {
	retention := 8
	if s := os.Getenv("GSBS_SAVE_VERSION_RETENTION"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n >= 5 && n <= 10 {
			retention = n
		}
	}
	// WAL mode enables concurrent readers. _busy_timeout=5000 (ms) covers the rare
	// write-write contention window (e.g. cmd/pcgw-sync running alongside the server).
	// MaxOpenConns>1 lets concurrent read queries use separate connections while WAL
	// serialises writes internally. MaxIdleConns=1 avoids idle-connection bloat.
	//
	// For in-memory databases (:memory:) each SQLite connection gets its own separate
	// database, so multiple open connections would create multiple isolated DBs. Pin
	// to a single connection so migration and subsequent queries always see the same DB.
	// _txlock=immediate makes BeginTx take the write lock up front, so
	// read-then-write transactions (quota check, version numbering) serialize
	// behind busy_timeout instead of failing mid-flight with a
	// SQLITE_BUSY_SNAPSHOT lock upgrade under concurrent writers.
	db, err := sql.Open("sqlite3", path+"?_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000&_txlock=immediate")
	if err != nil {
		return nil, err
	}
	if isInMemoryPath(path) {
		db.SetMaxOpenConns(1)
	} else {
		db.SetMaxOpenConns(5)
	}
	db.SetMaxIdleConns(1)
	s := &sqliteStore{db: db, versionRetention: retention, saveRoot: saveRootFromEnv(), dbPath: path}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	if os.Getenv("GSBS_MIGRATE_BLOBS_TO_FS") == "1" {
		if err := s.migrateBlobsToFS(); err != nil {
			logx.Logger().Error().Str("component", "store").Err(err).
				Msg("GSBS: blob-to-filesystem migration failed")
		}
	}
	// Discard staged save temp files orphaned by a crash between staging and
	// promotion (see UpsertSaveWithMeta); runs before any writes are accepted.
	s.sweepStagedTempFiles()
	return s, nil
}

func (s *sqliteStore) Close() error {
	return s.db.Close()
}

// isInMemoryPath reports whether a SQLite path refers to an in-memory database.
// Handles ":memory:", URI form "file::memory:?...", and mode=memory query strings.
func isInMemoryPath(path string) bool {
	return strings.Contains(path, ":memory:") || strings.Contains(path, "mode=memory")
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
// SQL deletions are wrapped in a single transaction; FS cleanup happens after commit.
func (s *sqliteStore) DeleteUser(ctx context.Context, userID string) error {
	// Collect filesystem paths before the transaction so we don't hold the
	// connection inside a tx while also trying to query via s.db.
	var fsPaths []string
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
			fsPaths = append(fsPaths, p)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM save_versions WHERE user_id = ?`, userID); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM saves WHERE user_id = ?`, userID); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM clients WHERE user_id = ?`, userID); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, userID); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	// FS cleanup after commit — partial cleanup is acceptable if the process is
	// interrupted here; the DB rows are already gone.
	for _, p := range fsPaths {
		removeSaveFile(p)
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
	if err != nil || !secret.Valid || secret.String == "" {
		return "", err
	}
	key, err := s.totpKey()
	if err != nil {
		return "", fmt.Errorf("totp key: %w", err)
	}
	// Sealed values decrypt; legacy plaintext passes through (fails closed on
	// a wrong/missing key file — 2FA login is blocked rather than bypassed).
	return openColumn(key, secret.String)
}

func (s *sqliteStore) SetTOTPSecret(ctx context.Context, userID string, secret string) error {
	stored := secret
	if secret != "" {
		key, err := s.totpKey()
		if err != nil {
			return fmt.Errorf("totp key: %w", err)
		}
		sealed, err := sealColumn(key, secret)
		if err != nil {
			return err
		}
		stored = sealed
	}
	_, err := s.db.ExecContext(ctx, `UPDATE users SET totp_secret = ? WHERE id = ?`, stored, userID)
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
		`SELECT id, name, os, COALESCE(last_seen, created_at), COALESCE(app_version, '') FROM clients WHERE user_id = ? ORDER BY name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ClientInfo
	for rows.Next() {
		var c ClientInfo
		if err := rows.Scan(&c.ID, &c.Name, &c.OS, &c.LastSeen, &c.AppVersion); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// RegenerateClientToken assigns a new token for the client; the previous token stops working.
// The client row is kept (e.g. after password or 2FA change via RevokeAllClientTokens).
func (s *sqliteStore) RegenerateClientToken(ctx context.Context, clientID string) error {
	newToken, err := genID()
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.ExecContext(ctx, `UPDATE clients SET token = ?, token_created_at = ? WHERE id = ?`, hashToken(newToken), now, clientID)
	return err
}

// RevokeClient deletes the client row so revoked devices no longer appear in WebUI lists.
func (s *sqliteStore) RevokeClient(ctx context.Context, clientID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM clients WHERE id = ?`, clientID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("client not found")
	}
	return nil
}

// RenameClient updates a client's display name, scoped to its owning user so a
// user can only rename their own devices.
func (s *sqliteStore) RenameClient(ctx context.Context, userID, clientID, name string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE clients SET name = ? WHERE id = ? AND user_id = ?`, name, clientID, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("client not found")
	}
	return nil
}

// RevokeAllClientTokens rotates the API token for every client owned by userID,
// forcing those clients to re-authenticate (e.g. after a password or 2FA change).
func (s *sqliteStore) RevokeAllClientTokens(ctx context.Context, userID string) error {
	return s.RevokeAllClientTokensExcept(ctx, userID, "")
}

// RevokeAllClientTokensExcept rotates tokens for every client owned by userID
// except keepClientID (empty = all), so the device that initiated a password
// change stays logged in while every other device must re-authenticate.
func (s *sqliteStore) RevokeAllClientTokensExcept(ctx context.Context, userID, keepClientID string) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM clients WHERE user_id = ?`, userID)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if scanErr := rows.Scan(&id); scanErr != nil {
			rows.Close()
			return scanErr
		}
		if id != keepClientID {
			ids = append(ids, id)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		if err := s.RegenerateClientToken(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

// DeleteSessionsByUserExcept removes all browser sessions for a user except
// keepSessionID (empty = all): a password change logs out every other browser.
func (s *sqliteStore) DeleteSessionsByUserExcept(ctx context.Context, userID, keepSessionID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ? AND id != ?`, userID, keepSessionID)
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

func (s *sqliteStore) GetSaveHashAndVersion(ctx context.Context, userID, gameID, pathKey string) (string, int, error) {
	var hash sql.NullString
	var version sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT s.content_hash, COALESCE(MAX(v.version), 0)
		 FROM saves s
		 LEFT JOIN save_versions v ON v.user_id = s.user_id AND v.game_id = s.game_id AND v.path_key = s.path_key
		 WHERE s.user_id = ? AND s.game_id = ? AND s.path_key = ?
		 GROUP BY s.content_hash`,
		userID, gameID, pathKey,
	).Scan(&hash, &version)
	if err == sql.ErrNoRows {
		return "", 0, nil
	}
	if err != nil {
		return "", 0, err
	}
	return hash.String, int(version.Int64), nil
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
	var storagePath, stagedTmp, oldStorage string
	var dbContent interface{} = content
	if s.filesystemEnabled() {
		var old sql.NullString
		_ = s.db.QueryRowContext(ctx,
			`SELECT storage_path FROM saves WHERE user_id = ? AND game_id = ? AND path_key = ?`,
			userID, gameID, pathKey,
		).Scan(&old)
		oldStorage = old.String
		// Stage beside the canonical file; it is promoted (renamed into
		// place) only after the transaction commits, so no error path below
		// can destroy the previous good save. The deferred remove discards
		// the staged bytes on every non-promoted return.
		stagedTmp, storagePath, err = s.stageSaveWrite(ctx, userID, gameID, relPath, content)
		if err != nil {
			return false, err
		}
		defer func() {
			if stagedTmp != "" {
				_ = os.Remove(stagedTmp)
			}
		}()
		dbContent = nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	enforceQuota := meta != nil && meta.QuotaBytes > 0
	enforceGlobal := meta != nil && meta.GlobalLimitBytes > 0
	var preUsage, preTotal int64
	if enforceQuota {
		if preUsage, err = userStorageUsage(ctx, tx, userID); err != nil {
			_ = tx.Rollback()
			return false, err
		}
	}
	if enforceGlobal {
		if preTotal, err = totalStorageUsage(ctx, tx); err != nil {
			_ = tx.Rollback()
			return false, err
		}
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO saves (user_id, game_id, path_key, content, relative_path, storage_path, updated_at, content_hash, content_size, client_id, encrypted)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(user_id, game_id, path_key) DO UPDATE SET
		   content = excluded.content, relative_path = excluded.relative_path, storage_path = excluded.storage_path,
		   updated_at = excluded.updated_at, content_hash = excluded.content_hash, content_size = excluded.content_size,
		   client_id = excluded.client_id, encrypted = excluded.encrypted`,
		userID, gameID, pathKey, dbContent, nullIfEmpty(relPath), nullIfEmpty(storagePath), now, contentHash, contentSize, nullIfEmpty(clientID), encrypted,
	)
	if err != nil {
		_ = tx.Rollback()
		return false, err
	}
	var nextVer int
	err = tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(version), 0) + 1 FROM save_versions WHERE user_id = ? AND game_id = ? AND path_key = ?`,
		userID, gameID, pathKey,
	).Scan(&nextVer)
	if err != nil {
		_ = tx.Rollback()
		return false, err
	}
	// change_bytes is the size delta versus the immediately-previous version
	// (the full size for the first version), measured the same way ListSaveVersions
	// reports SizeBytes (LENGTH(content)).
	changeBytes := int64(len(content))
	if nextVer > 1 {
		var prevSize sql.NullInt64
		_ = tx.QueryRowContext(ctx,
			`SELECT LENGTH(content) FROM save_versions WHERE user_id = ? AND game_id = ? AND path_key = ? AND version = ?`,
			userID, gameID, pathKey, nextVer-1,
		).Scan(&prevSize)
		if prevSize.Valid {
			changeBytes = int64(len(content)) - prevSize.Int64
		}
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO save_versions (user_id, game_id, path_key, version, content, updated_at, content_hash, client_id, change_bytes)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, gameID, pathKey, nextVer, content, now, contentHash, nullIfEmpty(clientID), changeBytes,
	)
	if err != nil {
		_ = tx.Rollback()
		return false, err
	}
	var cutoff sql.NullInt64
	_ = tx.QueryRowContext(ctx,
		`SELECT version FROM save_versions WHERE user_id = ? AND game_id = ? AND path_key = ? ORDER BY version DESC LIMIT 1 OFFSET ?`,
		userID, gameID, pathKey, retention-1,
	).Scan(&cutoff)
	if cutoff.Valid {
		_, _ = tx.ExecContext(ctx,
			`DELETE FROM save_versions WHERE user_id = ? AND game_id = ? AND path_key = ? AND version < ?`,
			userID, gameID, pathKey, cutoff.Int64,
		)
	}
	// Quota is enforced after all writes and pruning inside this transaction,
	// so the projected usage is exact and concurrent pushes serialize on
	// SQLite's single writer — no check-then-write race. Grandfathering: a
	// user already over the limit is only blocked from growing (post > pre),
	// never from shrinking or replacing.
	if enforceQuota {
		postUsage, uerr := userStorageUsage(ctx, tx, userID)
		if uerr != nil {
			_ = tx.Rollback()
			return false, uerr
		}
		if postUsage > meta.QuotaBytes && postUsage > preUsage {
			_ = tx.Rollback()
			return false, ErrQuotaExceeded
		}
	}
	if enforceGlobal {
		postTotal, terr := totalStorageUsage(ctx, tx)
		if terr != nil {
			_ = tx.Rollback()
			return false, terr
		}
		if postTotal > meta.GlobalLimitBytes && postTotal > preTotal {
			_ = tx.Rollback()
			return false, ErrGlobalLimitExceeded
		}
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	if stagedTmp != "" {
		if perr := promoteStagedFile(stagedTmp, storagePath, 0o640); perr != nil {
			// The row is committed but the canonical file still holds the
			// previous bytes, so the stored hash disagrees with the file
			// until the next successful push. Loud log so operators notice;
			// the deferred remove cleans up the staged temp.
			logx.Logger().Error().Str("component", "store").Str("user_id", userID).Str("game_id", gameID).
				Str("path_key", pathKey).Str("path", storagePath).Err(perr).
				Msg("GSBS: failed to promote staged save after commit; canonical file is stale")
		} else {
			stagedTmp = ""
			if oldStorage != "" && oldStorage != storagePath {
				removeSaveFile(oldStorage)
			}
		}
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

// GetSaveClientID returns the client that last wrote a save slot ("" when the
// slot doesn't exist or predates client tracking).
func (s *sqliteStore) GetSaveClientID(ctx context.Context, userID, gameID, pathKey string) (string, error) {
	var clientID sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT client_id FROM saves WHERE user_id = ? AND game_id = ? AND path_key = ?`,
		userID, gameID, pathKey,
	).Scan(&clientID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return clientID.String, err
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

// DeleteSavesForGame removes every save (and its versions and filesystem blobs)
// for one game belonging to a user. Returns the number of save rows deleted.
func (s *sqliteStore) DeleteSavesForGame(ctx context.Context, userID, gameID string) (int, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT storage_path FROM saves WHERE user_id = ? AND game_id = ? AND storage_path IS NOT NULL AND storage_path != ''`,
		userID, gameID,
	)
	if err == nil {
		var paths []string
		for rows.Next() {
			var p sql.NullString
			if rows.Scan(&p) == nil && p.Valid && p.String != "" {
				paths = append(paths, p.String)
			}
		}
		rows.Close()
		for _, p := range paths {
			removeSaveFile(p)
		}
	}
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM saves WHERE user_id = ? AND game_id = ?`, userID, gameID)
	if err != nil {
		return 0, err
	}
	_, _ = s.db.ExecContext(ctx,
		`DELETE FROM save_versions WHERE user_id = ? AND game_id = ?`, userID, gameID)
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (s *sqliteStore) ListSaveVersions(ctx context.Context, userID, gameID, pathKey string, limit int) ([]SaveVersionInfo, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT sv.version, sv.updated_at, LENGTH(sv.content), COALESCE(sv.change_bytes, 0),
		        COALESCE(sv.client_id, ''), COALESCE(c.name, '')
		 FROM save_versions sv
		 LEFT JOIN clients c ON c.id = sv.client_id
		 WHERE sv.user_id = ? AND sv.game_id = ? AND sv.path_key = ?
		 ORDER BY sv.version DESC LIMIT ?`,
		userID, gameID, pathKey, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SaveVersionInfo
	for rows.Next() {
		var v SaveVersionInfo
		if err := rows.Scan(&v.Version, &v.UpdatedAt, &v.SizeBytes, &v.ChangeBytes, &v.ClientID, &v.ClientName); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// LargestChangeForGame returns the biggest positive byte delta across all of a
// user's save versions for a game (with the device that produced it).
func (s *sqliteStore) LargestChangeForGame(ctx context.Context, userID, gameID string) (SaveChangeRow, bool, error) {
	var row SaveChangeRow
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(sv.change_bytes, 0), COALESCE(c.name, ''), sv.path_key, sv.updated_at
		 FROM save_versions sv
		 LEFT JOIN clients c ON c.id = sv.client_id
		 WHERE sv.user_id = ? AND sv.game_id = ?
		 ORDER BY sv.change_bytes DESC LIMIT 1`,
		userID, gameID,
	).Scan(&row.ChangeBytes, &row.ClientName, &row.PathKey, &row.UpdatedAt)
	if err == sql.ErrNoRows {
		return SaveChangeRow{}, false, nil
	}
	if err != nil {
		return SaveChangeRow{}, false, err
	}
	if row.ChangeBytes <= 0 {
		return SaveChangeRow{}, false, nil
	}
	return row, true, nil
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	s.enrichSteamAppIDsFromInfobox(ctx, out)
	return out, nil
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	s.enrichSteamAppIDsFromInfobox(ctx, out)
	return out, nil
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

// UpdateClientLastSeen updates the last_seen timestamp for a client (called on
// every authenticated API request) and records the client's reported app
// version when present (empty preserves the previous value).
func (s *sqliteStore) UpdateClientLastSeen(ctx context.Context, clientID, appVersion string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE clients SET last_seen = ?, app_version = COALESCE(NULLIF(?, ''), app_version) WHERE id = ?`,
		time.Now().UTC().Format(time.RFC3339), strings.TrimSpace(appVersion), clientID,
	)
	return err
}

// cryptoV2MinMajor is the first client major version whose crypto layer reads
// the gsbs2: (Argon2id) save-encryption format.
const cryptoV2MinMajor = 4

// CryptoV2Ready reports whether every one of the user's recently-seen clients
// (last 30 days) reports an app version that can read the v2 save-encryption
// format. Stale or revoked devices don't hold the fleet back; a device with
// no reported version counts as legacy. The caller is always among the recent
// clients (its version was just recorded), so a lone up-to-date device is
// ready immediately.
func (s *sqliteStore) CryptoV2Ready(ctx context.Context, userID string) (bool, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT COALESCE(app_version, ''), COALESCE(last_seen, '') FROM clients WHERE user_id = ?`, userID)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	cutoff := time.Now().UTC().AddDate(0, 0, -30)
	for rows.Next() {
		var version, lastSeen string
		if err := rows.Scan(&version, &lastSeen); err != nil {
			return false, err
		}
		seen, perr := time.Parse(time.RFC3339, lastSeen)
		if perr != nil || seen.Before(cutoff) {
			continue // never-seen or stale device: doesn't hold the fleet back
		}
		if versionMajor(version) < cryptoV2MinMajor {
			return false, nil
		}
	}
	return true, rows.Err()
}

// versionMajor parses the leading major number from "4.0.0", "v4.1.2", or
// "4.0.0-dev"; anything unparsable is 0 (legacy).
func versionMajor(v string) int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, ".-+"); i >= 0 {
		v = v[:i]
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0
	}
	return n
}

// ListSaveSummaries returns lightweight save info (no content blob) with game title from manifest.
func (s *sqliteStore) ListSaveSummaries(ctx context.Context, userID string) ([]SaveSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT s.game_id, s.path_key, COALESCE(s.content_size, LENGTH(s.content), 0) AS size_bytes, s.updated_at,
		       COALESCE(g.game_title, s.game_id) AS game_title,
		       COALESCE(s.content_hash, '') AS content_hash,
		       COALESCE(s.relative_path, '') AS relative_path,
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
		if err := rows.Scan(&ss.GameID, &ss.PathKey, &ss.SizeBytes, &ss.UpdatedAt, &ss.GameTitle, &ss.ContentHash, &ss.RelativePath, &enc); err != nil {
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
		SELECT s.game_id, s.path_key, COALESCE(s.content_size, LENGTH(s.content), 0) AS size_bytes, s.updated_at,
		       COALESCE(g.game_title, s.game_id) AS game_title,
		       COALESCE(s.content_hash, '') AS content_hash,
		       COALESCE(s.relative_path, '') AS relative_path,
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
		if err := rows.Scan(&ss.GameID, &ss.PathKey, &ss.SizeBytes, &ss.UpdatedAt, &ss.GameTitle, &ss.ContentHash, &ss.RelativePath, &enc); err != nil {
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
		SELECT s.game_id, s.path_key, COALESCE(s.content_size, LENGTH(s.content), 0) AS size_bytes, s.updated_at,
		       COALESCE(g.game_title, s.game_id) AS game_title,
		       COALESCE(s.content_hash, '') AS content_hash,
		       COALESCE(s.relative_path, '') AS relative_path,
		       COALESCE(s.encrypted, 0) AS encrypted
		FROM saves s
		LEFT JOIN (SELECT DISTINCT game_id, game_title FROM game_save_locations) g
		  ON s.game_id = g.game_id
		WHERE s.user_id = ?
		  AND (s.game_id LIKE ? OR s.path_key LIKE ? OR COALESCE(g.game_title, s.game_id) LIKE ?
		       OR COALESCE(s.relative_path, '') LIKE ?)
		ORDER BY s.updated_at DESC`, userID, pattern, pattern, pattern, pattern)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SaveSummary
	for rows.Next() {
		var ss SaveSummary
		var enc int
		if err := rows.Scan(&ss.GameID, &ss.PathKey, &ss.SizeBytes, &ss.UpdatedAt, &ss.GameTitle, &ss.ContentHash, &ss.RelativePath, &enc); err != nil {
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

// Usage = current saves (file/blob bytes) + retained version history (always
// stored as DB blobs). Both terms are real disk footprint, so this is what
// quotas enforce against.
const userUsageSQL = `SELECT
	COALESCE((SELECT SUM(COALESCE(content_size, LENGTH(content), 0)) FROM saves WHERE user_id = ?), 0)
  + COALESCE((SELECT SUM(LENGTH(content)) FROM save_versions WHERE user_id = ?), 0)`

const totalUsageSQL = `SELECT
	COALESCE((SELECT SUM(COALESCE(content_size, LENGTH(content), 0)) FROM saves), 0)
  + COALESCE((SELECT SUM(LENGTH(content)) FROM save_versions), 0)`

// queryRower is satisfied by both *sql.DB and *sql.Tx so the usage queries
// can run standalone or inside the quota-enforcing write transaction.
type queryRower interface {
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

func userStorageUsage(ctx context.Context, q queryRower, userID string) (int64, error) {
	var n int64
	err := q.QueryRowContext(ctx, userUsageSQL, userID, userID).Scan(&n)
	return n, err
}

func totalStorageUsage(ctx context.Context, q queryRower) (int64, error) {
	var n int64
	err := q.QueryRowContext(ctx, totalUsageSQL).Scan(&n)
	return n, err
}

// StorageUsage returns the user's total stored bytes including version history.
func (s *sqliteStore) StorageUsage(ctx context.Context, userID string) (int64, error) {
	return userStorageUsage(ctx, s.db, userID)
}

// TotalStorageUsage returns total stored bytes including version history.
func (s *sqliteStore) TotalStorageUsage(ctx context.Context) (int64, error) {
	return totalStorageUsage(ctx, s.db)
}

// PruneHistory deletes append-only history older than the retention windows
// and optionally age-prunes save versions (keeping a newest-N floor per slot).
func (s *sqliteStore) PruneHistory(ctx context.Context, auditDays, manifestDays, statsDays, versionMaxAgeDays int) (PruneCounts, error) {
	var pc PruneCounts
	cutoff := func(days int) string {
		return time.Now().UTC().AddDate(0, 0, -days).Format(time.RFC3339)
	}
	del := func(query, before string) (int64, error) {
		res, err := s.db.ExecContext(ctx, query, before)
		if err != nil {
			return 0, err
		}
		n, _ := res.RowsAffected()
		return n, nil
	}
	var err error
	if auditDays > 0 {
		if pc.Audit, err = del(`DELETE FROM audit_log WHERE at < ?`, cutoff(auditDays)); err != nil {
			return pc, fmt.Errorf("prune audit_log: %w", err)
		}
	}
	if manifestDays > 0 {
		if pc.ManifestFetches, err = del(`DELETE FROM manifest_fetches WHERE fetched_at < ?`, cutoff(manifestDays)); err != nil {
			return pc, fmt.Errorf("prune manifest_fetches: %w", err)
		}
	}
	if statsDays > 0 {
		if pc.Stats, err = del(`DELETE FROM stats_snapshots WHERE at < ?`, cutoff(statsDays)); err != nil {
			return pc, fmt.Errorf("prune stats_snapshots: %w", err)
		}
	}
	if versionMaxAgeDays > 0 {
		// Age-based version pruning with a newest-N floor: even if every
		// version of a slot is ancient, the newest min(retention, 3) survive
		// so a restore point always exists.
		keep := 3
		if s.versionRetention < keep {
			keep = s.versionRetention
		}
		res, rerr := s.db.ExecContext(ctx, `
			DELETE FROM save_versions
			WHERE updated_at < ?
			  AND version NOT IN (
			    SELECT v2.version FROM save_versions v2
			    WHERE v2.user_id = save_versions.user_id
			      AND v2.game_id = save_versions.game_id
			      AND v2.path_key = save_versions.path_key
			    ORDER BY v2.version DESC LIMIT ?
			  )`, cutoff(versionMaxAgeDays), keep)
		if rerr != nil {
			return pc, fmt.Errorf("prune save_versions: %w", rerr)
		}
		pc.SaveVersions, _ = res.RowsAffected()
	}
	return pc, nil
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
		logx.Logger().Warn().Str("component", "store").Str("run_id", j.ID).
			Str("job", j.JobName).Str("started", j.StartedAt).
			Msg("reconcile: job_runs -> interrupted")
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
