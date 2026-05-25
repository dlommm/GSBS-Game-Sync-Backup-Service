package store

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/gsbs/gsbs/pkg/savepath"
)

const defaultSaveRoot = "/app/data/gamesaves"

func saveRootFromEnv() string {
	root := strings.TrimSpace(os.Getenv("GSBS_SAVE_ROOT"))
	if root == "" {
		return ""
	}
	return filepath.Clean(root)
}

// DefaultSaveRoot is the documented default path when GSBS_SAVE_ROOT is unset in Docker examples.
func DefaultSaveRoot() string {
	return defaultSaveRoot
}

func (s *sqliteStore) filesystemEnabled() bool {
	return s.saveRoot != ""
}

func (s *sqliteStore) EnsureUserStorage(ctx context.Context, userID string) error {
	if !s.filesystemEnabled() {
		return nil
	}
	dir := filepath.Join(s.saveRoot, userID)
	return os.MkdirAll(dir, 0o750)
}

func (s *sqliteStore) migrateSaveFilesystem() error {
	hasRelative, contentNotNull, err := s.savesColumnState()
	if err != nil {
		return err
	}
	if hasRelative && !contentNotNull {
		return nil
	}
	_, err = s.db.Exec(`
		CREATE TABLE saves_fs (
			user_id TEXT NOT NULL,
			game_id TEXT NOT NULL,
			path_key TEXT NOT NULL,
			content BLOB,
			relative_path TEXT,
			storage_path TEXT,
			updated_at TEXT NOT NULL,
			content_hash TEXT,
			content_size INTEGER,
			client_id TEXT,
			encrypted INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (user_id, game_id, path_key),
			FOREIGN KEY (user_id) REFERENCES users(id)
		)`)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`
		INSERT INTO saves_fs (user_id, game_id, path_key, content, relative_path, storage_path, updated_at, content_hash, content_size, client_id, encrypted)
		SELECT user_id, game_id, path_key, content, NULL, NULL, updated_at, content_hash, content_size, client_id, COALESCE(encrypted, 0)
		FROM saves`)
	if err != nil {
		return err
	}
	if _, err = s.db.Exec(`DROP TABLE saves`); err != nil {
		return err
	}
	if _, err = s.db.Exec(`ALTER TABLE saves_fs RENAME TO saves`); err != nil {
		return err
	}
	_, err = s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_saves_user ON saves(user_id)`)
	return err
}

func (s *sqliteStore) savesColumnState() (hasRelative bool, contentNotNull bool, err error) {
	rows, err := s.db.Query(`PRAGMA table_info(saves)`)
	if err != nil {
		return false, false, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return false, false, err
		}
		switch name {
		case "relative_path":
			hasRelative = true
		case "content":
			contentNotNull = notnull != 0
		}
	}
	return hasRelative, contentNotNull, rows.Err()
}

func (s *sqliteStore) migrateBlobsToFS() error {
	if !s.filesystemEnabled() {
		return nil
	}
	rows, err := s.db.Query(`
		SELECT user_id, game_id, path_key, content, relative_path, storage_path
		FROM saves
		WHERE content IS NOT NULL AND LENGTH(content) > 0 AND (storage_path IS NULL OR storage_path = '')`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	var migrated int
	for rows.Next() {
		var userID, gameID, pathKey string
		var content []byte
		var relPath, storagePath sql.NullString
		if err := rows.Scan(&userID, &gameID, &pathKey, &content, &relPath, &storagePath); err != nil {
			return err
		}
		rel := relPath.String
		if rel == "" {
			rel = filepath.Join(gameID, pathKey)
		}
		if err := s.EnsureUserStorage(context.Background(), userID); err != nil {
			return fmt.Errorf("ensure storage for user %s: %w", userID, err)
		}
		absPath, err := savepath.JoinUserGamePath(s.saveRoot, userID, gameID, rel)
		if err != nil {
			return fmt.Errorf("join path user=%s game=%s: %w", userID, gameID, err)
		}
		if err := os.MkdirAll(filepath.Dir(absPath), 0o750); err != nil {
			return err
		}
		if err := os.WriteFile(absPath, content, 0o640); err != nil {
			return err
		}
		_, err = s.db.Exec(`
			UPDATE saves SET content = NULL, relative_path = ?, storage_path = ? WHERE user_id = ? AND game_id = ? AND path_key = ?`,
			rel, absPath, userID, gameID, pathKey)
		if err != nil {
			return err
		}
		migrated++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if migrated > 0 {
		log.Printf("GSBS: migrated %d save blob(s) to filesystem under %s", migrated, s.saveRoot)
	}
	return nil
}

func (s *sqliteStore) readSaveContent(storagePath sql.NullString, content []byte) ([]byte, error) {
	if storagePath.Valid && storagePath.String != "" {
		data, err := os.ReadFile(storagePath.String)
		if err != nil {
			return nil, fmt.Errorf("read save file: %w", err)
		}
		return data, nil
	}
	return content, nil
}

func (s *sqliteStore) writeSaveToFilesystem(ctx context.Context, userID, gameID, relPath string, content []byte) (string, error) {
	if err := s.EnsureUserStorage(ctx, userID); err != nil {
		return "", err
	}
	absPath, err := savepath.JoinUserGamePath(s.saveRoot, userID, gameID, relPath)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0o750); err != nil {
		return "", err
	}
	if err := os.WriteFile(absPath, content, 0o640); err != nil {
		return "", err
	}
	return absPath, nil
}

func removeSaveFile(storagePath string) {
	if storagePath == "" {
		return
	}
	_ = os.Remove(storagePath)
}

func (s *sqliteStore) removeUserStorageDir(userID string) {
	if !s.filesystemEnabled() {
		return
	}
	_ = os.RemoveAll(filepath.Join(s.saveRoot, userID))
}

func defaultRelativePath(gameID, pathKey string) string {
	return filepath.Join(gameID, pathKey)
}
