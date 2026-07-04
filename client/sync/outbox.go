package sync

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gsbs/gsbs/pkg/retry"
)

const outboxMaxAge = 7 * 24 * time.Hour

var outboxMu sync.Mutex

// outboxAuthFailed is set when a push attempt returns ErrUnauthorized.
// While set, ProcessOutbox skips all retry attempts to avoid hammering a 401.
// Cleared by ClearOutboxAuthFailed after a successful auth (pull or push).
var outboxAuthFailed atomic.Bool

// ClearOutboxAuthFailed clears the auth-failed pause, allowing outbox retries to resume.
// Call after a successful pull or when the user re-authenticates.
func ClearOutboxAuthFailed() {
	outboxAuthFailed.Store(false)
}

// IsOutboxAuthFailed reports whether the outbox is paused due to an auth failure.
func IsOutboxAuthFailed() bool {
	return outboxAuthFailed.Load()
}

// OutboxEntry is a failed push persisted for later retry.
type OutboxEntry struct {
	ID           string    `json:"id"`
	GameID       string    `json:"game_id"`
	PathKey      string    `json:"path_key"`
	FilePath     string    `json:"file_path"`
	RelativePath string    `json:"relative_path,omitempty"`
	Content      string    `json:"content,omitempty"`      // legacy base64 inline payload
	ContentHash  string    `json:"content_hash,omitempty"` // expected content change hash (plaintext SHA-256) when Content empty
	ContentSize  int64     `json:"content_size,omitempty"` // plaintext size hint
	CreatedAt    time.Time `json:"created_at"`
	Attempts     int       `json:"attempts"`
	NextRetryAt  time.Time `json:"next_retry_at,omitempty"`
}

func outboxDir() string {
	dir, _ := os.UserConfigDir()
	return filepath.Join(dir, "gsbs", "outbox")
}

func outboxSlotKey(gameID, pathKey string) string {
	return gameID + "\x00" + pathKey
}

// EnqueueOutbox persists a failed push for later retry.
// When wireHash is non-empty, the file is re-read at send time instead of storing base64 content.
func EnqueueOutbox(gameID, pathKey, filePath, relativePath string, content []byte, wireHash string) error {
	outboxMu.Lock()
	defer outboxMu.Unlock()

	dir := outboxDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Collapse duplicate pending entries for the same slot (keep newest).
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var existing OutboxEntry
		if json.Unmarshal(data, &existing) != nil {
			continue
		}
		if outboxSlotKey(existing.GameID, existing.PathKey) == outboxSlotKey(gameID, pathKey) {
			_ = os.Remove(path)
			logSyncInfo("outbox_dedup", "game_id", gameID, "path_key", pathKey, "removed_id", existing.ID)
		}
	}

	id := fmt.Sprintf("%d", time.Now().UnixNano())
	entry := OutboxEntry{
		ID:           id,
		GameID:       gameID,
		PathKey:      pathKey,
		FilePath:     filePath,
		RelativePath: relativePath,
		CreatedAt:    time.Now(),
		NextRetryAt:  time.Now().Add(retry.OutboxBackoff().Initial),
		ContentSize:  int64(len(content)),
	}
	if wireHash != "" {
		entry.ContentHash = wireHash
	} else if len(content) > 0 {
		entry.Content = base64.StdEncoding.EncodeToString(content)
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, id+".json")
	if err := atomicWriteFile(path, data, 0600); err != nil {
		return err
	}
	logSyncInfo("outbox_enqueue", "game_id", gameID, "path_key", pathKey, "relative_path", relativePath,
		"bytes", len(content), "id", id, "wire_hash", wireHash != "")
	return nil
}

func loadOutboxContent(entry *OutboxEntry, client *Client) ([]byte, error) {
	if entry.Content != "" {
		return base64.StdEncoding.DecodeString(entry.Content)
	}
	if entry.FilePath == "" {
		return nil, fmt.Errorf("outbox entry missing content and file_path")
	}
	info, err := os.Stat(entry.FilePath)
	if err != nil {
		return nil, fmt.Errorf("outbox file missing: %w", err)
	}
	if info.Size() == 0 {
		return nil, fmt.Errorf("outbox file empty")
	}
	content, err := os.ReadFile(entry.FilePath)
	if err != nil {
		return nil, err
	}
	if entry.ContentHash != "" {
		hash, err := client.ContentChangeHash(content)
		if err != nil {
			return nil, err
		}
		if hash != entry.ContentHash {
			// File changed since enqueue: push the current content rather than
			// dropping the entry. Update the stored hash so retry persists correctly.
			logSyncInfo("outbox_hash_updated", "game_id", entry.GameID, "path_key", entry.PathKey,
				"old_hash", entry.ContentHash, "new_hash", hash)
			entry.ContentHash = hash
		}
	}
	return content, nil
}

// ProcessOutbox retries pending outbox entries due for retry. Returns count sent.
func ProcessOutbox(ctx context.Context, client *Client) int {
	if outboxAuthFailed.Load() {
		logSyncDebug("outbox_auth_paused", "reason", "auth failed; waiting for successful auth before retrying")
		return 0
	}

	// Phase 1: snapshot candidates under lock so EnqueueOutbox is not blocked
	// during network I/O.
	outboxMu.Lock()
	dir := outboxDir()
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		outboxMu.Unlock()
		if os.IsNotExist(err) {
			return 0
		}
		logSyncWarn("outbox_read_dir", "error", err)
		return 0
	}
	type candidate struct {
		entry OutboxEntry
		path  string
	}
	now := time.Now()
	var candidates []candidate
	for _, e := range dirEntries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var entry OutboxEntry
		if json.Unmarshal(data, &entry) != nil {
			_ = os.Remove(path)
			continue
		}
		if now.Sub(entry.CreatedAt) > outboxMaxAge {
			logSyncInfo("outbox_expired", "id", entry.ID, "game_id", entry.GameID, "path_key", entry.PathKey,
				"age", now.Sub(entry.CreatedAt).Round(time.Minute).String())
			_ = os.Remove(path)
			continue
		}
		if !entry.NextRetryAt.IsZero() && now.Before(entry.NextRetryAt) {
			continue
		}
		candidates = append(candidates, candidate{entry, path})
	}
	outboxMu.Unlock()

	// Phase 2: process each candidate — no lock held during network I/O.
	sent := 0
	for _, c := range candidates {
		entry := c.entry
		path := c.path

		content, loadErr := loadOutboxContent(&entry, client)
		if loadErr != nil {
			logSyncWarn("outbox_load", "id", entry.ID, "game_id", entry.GameID, "path_key", entry.PathKey, "error", loadErr)
			if errors.Is(loadErr, os.ErrNotExist) {
				// File gone — entry can never be pushed; remove it.
				outboxMu.Lock()
				_ = os.Remove(path)
				outboxMu.Unlock()
			}
			continue
		}

		relPath := entry.RelativePath
		if err := client.pushOnce(ctx, entry.GameID, entry.PathKey, entry.FilePath, relPath, content); err != nil {
			if errors.Is(err, ErrUnauthorized) {
				outboxAuthFailed.Store(true)
				logSyncWarn("outbox_auth_failed", "id", entry.ID, "game_id", entry.GameID, "path_key", entry.PathKey,
					"msg", "401 Unauthorized — pausing outbox retry until re-login")
				if OnAuthError != nil {
					OnAuthError("Sync paused: token invalid or expired — please log in again from the tray")
				}
				return sent
			}
			if !retry.IsRetryableError(err) {
				logSyncWarn("outbox_non_retryable", "id", entry.ID, "game_id", entry.GameID, "path_key", entry.PathKey, "error", err)
				outboxMu.Lock()
				_ = os.Remove(path)
				outboxMu.Unlock()
				continue
			}
			// Fix 5: fresh backoff per entry so earlier entries don't inflate later delays.
			bo := retry.OutboxBackoff()
			entry.Attempts++
			for i := 0; i < entry.Attempts; i++ {
				bo.Next()
			}
			entry.NextRetryAt = now.Add(bo.Current())
			outboxMu.Lock()
			if updated, err := json.Marshal(entry); err == nil {
				// Best-effort: a failed rewrite only loses the bumped retry
				// counter, not the entry itself.
				if writeErr := atomicWriteFile(path, updated, 0600); writeErr != nil {
					logSyncWarn("outbox_rewrite_error", "path", path, "error", writeErr)
				}
			}
			outboxMu.Unlock()
			logSyncWarn("outbox_retry_failed", "id", entry.ID, "game_id", entry.GameID, "path_key", entry.PathKey,
				"attempts", entry.Attempts, "next_retry", entry.NextRetryAt.Format(time.RFC3339), "error", err)
			continue
		}

		outboxMu.Lock()
		if err := os.Remove(path); err != nil {
			logSyncWarn("outbox_remove", "path", path, "error", err)
		} else {
			sent++
			logSyncInfo("outbox_sent", "game_id", entry.GameID, "path_key", entry.PathKey, "relative_path", relPath,
				"bytes", len(content))
			if OnSaveEvent != nil {
				OnSaveEvent(entry.GameID, entry.PathKey, "", SaveDirPush, nil)
			}
		}
		outboxMu.Unlock()
	}
	return sent
}

// OutboxCount returns the number of pending outbox entries.
func OutboxCount() int {
	dir := outboxDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			n++
		}
	}
	return n
}
