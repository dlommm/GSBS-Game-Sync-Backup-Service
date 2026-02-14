package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"time"

	"github.com/gsbs/gsbs/pkg/types"
	_ "github.com/mattn/go-sqlite3"
)

type sqliteStore struct {
	db *sql.DB
}

// NewSQLite creates a SQLite-backed store.
func NewSQLite(path string) (Store, error) {
	db, err := sql.Open("sqlite3", path+"?_foreign_keys=on")
	if err != nil {
		return nil, err
	}
	s := &sqliteStore{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
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
			UNIQUE(game_id, platform, path_template)
		);
		CREATE INDEX IF NOT EXISTS idx_clients_token ON clients(token);
		CREATE INDEX IF NOT EXISTS idx_saves_user ON saves(user_id);
		CREATE INDEX IF NOT EXISTS idx_manifest_updated ON game_save_locations(updated_at);
	`)
	return err
}

func (s *sqliteStore) CreateUser(ctx context.Context, username, passwordHash string) (string, error) {
	id := genID()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO users (id, username, password_hash, created_at) VALUES (?, ?, ?, ?)`,
		id, username, passwordHash, time.Now().UTC().Format(time.RFC3339),
	)
	return id, err
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

func (s *sqliteStore) RegisterClient(ctx context.Context, userID, name, os string) (string, error) {
	id := genID()
	token := genID()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO clients (id, user_id, name, os, token, last_seen, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, userID, name, os, token, time.Now().UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return "", err
	}
	return token, nil
}

func (s *sqliteStore) ClientByToken(ctx context.Context, token string) (userID, clientID, name, os string, err error) {
	err = s.db.QueryRowContext(ctx,
		`SELECT user_id, id, name, os FROM clients WHERE token = ?`, token,
	).Scan(&userID, &clientID, &name, &os)
	return userID, clientID, name, os, err
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
	newToken := genID()
	_, err := s.db.ExecContext(ctx, `UPDATE clients SET token = ? WHERE id = ?`, newToken, clientID)
	return err
}

func (s *sqliteStore) UpsertSave(ctx context.Context, userID, gameID, pathKey string, content []byte) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO saves (user_id, game_id, path_key, content, updated_at) VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(user_id, game_id, path_key) DO UPDATE SET content = excluded.content, updated_at = excluded.updated_at`,
		userID, gameID, pathKey, content, now,
	)
	return err
}

func (s *sqliteStore) ListSaves(ctx context.Context, userID string) ([]types.SaveBlob, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT game_id, path_key, content, updated_at FROM saves WHERE user_id = ?`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.SaveBlob
	for rows.Next() {
		var b types.SaveBlob
		var updatedAt string
		if err := rows.Scan(&b.GameID, &b.PathKey, &b.Content, &updatedAt); err != nil {
			return nil, err
		}
		b.UpdatedAt = updatedAt
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *sqliteStore) GetSave(ctx context.Context, userID, gameID, pathKey string) (*types.SaveBlob, error) {
	var b types.SaveBlob
	var updatedAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT game_id, path_key, content, updated_at FROM saves WHERE user_id = ? AND game_id = ? AND path_key = ?`,
		userID, gameID, pathKey,
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

func (s *sqliteStore) UpsertGameSaveLocations(ctx context.Context, entries []types.GameSaveLocation) error {
	now := time.Now().UTC().Format(time.RFC3339)
	for _, e := range entries {
		id := genID()
		isConfig := 0
		if e.IsConfig {
			isConfig = 1
		}
		if e.UpdatedAt == "" {
			e.UpdatedAt = now
		}
		_, err := s.db.ExecContext(ctx,
			`INSERT INTO game_save_locations (id, game_id, pcgw_page_id, game_title, platform, path_template, is_config, updated_at, source)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT(game_id, platform, path_template) DO UPDATE SET
			   pcgw_page_id = excluded.pcgw_page_id,
			   game_title = excluded.game_title,
			   is_config = excluded.is_config,
			   updated_at = excluded.updated_at,
			   source = excluded.source`,
			id, e.GameID, e.PCGWPageID, e.GameTitle, e.Platform, e.PathTemplate, isConfig, e.UpdatedAt, e.Source,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *sqliteStore) ListGameSaveLocations(ctx context.Context) ([]types.GameSaveLocation, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT game_id, pcgw_page_id, game_title, platform, path_template, is_config, updated_at, source FROM game_save_locations ORDER BY game_id, platform`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.GameSaveLocation
	for rows.Next() {
		var e types.GameSaveLocation
		var isConfig int
		if err := rows.Scan(&e.GameID, &e.PCGWPageID, &e.GameTitle, &e.Platform, &e.PathTemplate, &isConfig, &e.UpdatedAt, &e.Source); err != nil {
			return nil, err
		}
		e.IsConfig = isConfig != 0
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *sqliteStore) GetManifestSince(ctx context.Context, since string) ([]types.GameSaveLocation, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT game_id, pcgw_page_id, game_title, platform, path_template, is_config, updated_at, source FROM game_save_locations WHERE updated_at > ? ORDER BY game_id, platform`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.GameSaveLocation
	for rows.Next() {
		var e types.GameSaveLocation
		var isConfig int
		if err := rows.Scan(&e.GameID, &e.PCGWPageID, &e.GameTitle, &e.Platform, &e.PathTemplate, &isConfig, &e.UpdatedAt, &e.Source); err != nil {
			return nil, err
		}
		e.IsConfig = isConfig != 0
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

func genID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return time.Now().Format("20060102150405") + "00000000"
	}
	return hex.EncodeToString(b)
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "00000000"
	}
	return hex.EncodeToString(b)
}
