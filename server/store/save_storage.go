package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gsbs/gsbs/pkg/savepath"
	"github.com/gsbs/gsbs/server/logx"
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
	type blobRow struct {
		userID, gameID, pathKey string
		content                 []byte
		relPath, storagePath    sql.NullString
	}
	var blobs []blobRow
	for rows.Next() {
		var b blobRow
		if err := rows.Scan(&b.userID, &b.gameID, &b.pathKey, &b.content, &b.relPath, &b.storagePath); err != nil {
			rows.Close()
			return err
		}
		blobs = append(blobs, b)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	var migrated int
	for _, b := range blobs {
		rel := b.relPath.String
		if rel == "" {
			rel = filepath.Join(b.gameID, b.pathKey)
		}
		if err := s.EnsureUserStorage(context.Background(), b.userID); err != nil {
			return fmt.Errorf("ensure storage for user %s: %w", b.userID, err)
		}
		absPath, err := savepath.JoinUserGamePath(s.saveRoot, b.userID, b.gameID, rel)
		if err != nil {
			return fmt.Errorf("join path user=%s game=%s: %w", b.userID, b.gameID, err)
		}
		if err := os.MkdirAll(filepath.Dir(absPath), 0o750); err != nil {
			return err
		}
		if err := os.WriteFile(absPath, b.content, 0o640); err != nil {
			return err
		}
		_, err = s.db.Exec(`
			UPDATE saves SET content = NULL, relative_path = ?, storage_path = ? WHERE user_id = ? AND game_id = ? AND path_key = ?`,
			rel, absPath, b.userID, b.gameID, b.pathKey)
		if err != nil {
			return err
		}
		migrated++
	}
	if migrated > 0 {
		logx.Logger().Info().Str("component", "store").Int("count", migrated).Str("save_root", s.saveRoot).
			Msg("GSBS: migrated save blob(s) to filesystem")
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
