package store

import (
	"context"
	"database/sql"
	"fmt"
	iofs "io/fs"
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
		if err := atomicWriteFileSync(absPath, b.content, 0o640); err != nil {
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

// stageSaveWrite prepares a save write without touching the canonical file:
// it resolves the final path, ensures the directories exist, and stages the
// fsync'd content in a temp file beside it. The canonical file is replaced
// only by promoteStagedFile after the accompanying DB transaction commits,
// so a failed transaction can never destroy the previous good save.
func (s *sqliteStore) stageSaveWrite(ctx context.Context, userID, gameID, relPath string, content []byte) (tmpPath, finalPath string, err error) {
	if err := s.EnsureUserStorage(ctx, userID); err != nil {
		return "", "", err
	}
	absPath, err := savepath.JoinUserGamePath(s.saveRoot, userID, gameID, relPath)
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0o750); err != nil {
		return "", "", err
	}
	tmp, err := stageTempFile(filepath.Dir(absPath), content)
	if err != nil {
		return "", "", err
	}
	return tmp, absPath, nil
}

// stageTempFile writes content to an fsync'd temp file inside dir and returns
// its path. The data is durable but not yet visible under any canonical name;
// pair with promoteStagedFile, or os.Remove to discard.
func stageTempFile(dir string, content []byte) (string, error) {
	tmp, err := os.CreateTemp(dir, ".gsbs-*.tmp")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return "", err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return "", err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return "", err
	}
	return tmpName, nil
}

// promoteStagedFile atomically renames a staged temp file onto path and
// fsyncs the parent directory so the rename survives a crash. After a crash
// or power loss the destination is either the old bytes or the complete new
// bytes, never a truncated mix.
func promoteStagedFile(tmpName, path string, perm os.FileMode) error {
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	if d, derr := os.Open(filepath.Dir(path)); derr == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// atomicWriteFileSync is stage+promote in one step, for writes that carry no
// DB transaction (blob migration and similar).
func atomicWriteFileSync(path string, content []byte, perm os.FileMode) error {
	tmpName, err := stageTempFile(filepath.Dir(path), content)
	if err != nil {
		return err
	}
	if err := promoteStagedFile(tmpName, path, perm); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

// sweepStagedTempFiles removes orphaned staged temp files (".gsbs-*.tmp")
// left under the save root by a crash between staging and promotion. Called
// once at startup, before the server accepts writes.
func (s *sqliteStore) sweepStagedTempFiles() {
	if !s.filesystemEnabled() {
		return
	}
	var removed int
	_ = filepath.WalkDir(s.saveRoot, func(path string, d iofs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, ".gsbs-") && strings.HasSuffix(name, ".tmp") {
			if os.Remove(path) == nil {
				removed++
			}
		}
		return nil
	})
	if removed > 0 {
		logx.Logger().Info().Str("component", "store").Int("count", removed).Str("save_root", s.saveRoot).
			Msg("GSBS: removed orphaned staged save temp file(s)")
	}
}

// FreeSpaceForWrites reports free bytes on the volume that receives save
// writes: the save root in filesystem mode, otherwise the database directory.
// Returns -1 for in-memory databases (no meaningful volume).
func (s *sqliteStore) FreeSpaceForWrites() (int64, error) {
	target := s.saveRoot
	if target == "" {
		if isInMemoryPath(s.dbPath) {
			return -1, nil
		}
		target = filepath.Dir(s.dbPath)
	}
	return freeDiskBytes(target)
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
