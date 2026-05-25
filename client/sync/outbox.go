package sync

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/gsbs/gsbs/pkg/retry"
)

const outboxMaxAge = 7 * 24 * time.Hour

// OutboxEntry is a failed push persisted for later retry.
type OutboxEntry struct {
	ID          string    `json:"id"`
	GameID      string    `json:"game_id"`
	PathKey     string    `json:"path_key"`
	FilePath    string    `json:"file_path"`
	Content     string    `json:"content"` // base64
	CreatedAt   time.Time `json:"created_at"`
	Attempts    int       `json:"attempts"`
	NextRetryAt time.Time `json:"next_retry_at,omitempty"`
}

func outboxDir() string {
	dir, _ := os.UserConfigDir()
	return filepath.Join(dir, "gsbs", "outbox")
}

// EnqueueOutbox persists a failed push for later retry.
func EnqueueOutbox(gameID, pathKey, filePath string, content []byte) error {
	dir := outboxDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	id := fmt.Sprintf("%d", time.Now().UnixNano())
	entry := OutboxEntry{
		ID:          id,
		GameID:      gameID,
		PathKey:     pathKey,
		FilePath:    filePath,
		Content:     base64.StdEncoding.EncodeToString(content),
		CreatedAt:   time.Now(),
		NextRetryAt: time.Now().Add(retry.OutboxBackoff().Initial),
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, id+".json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ProcessOutbox retries pending outbox entries due for retry. Returns count sent.
func ProcessOutbox(ctx context.Context, client *Client) int {
	dir := outboxDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		log.Printf("outbox: read dir: %v", err)
		return 0
	}
	now := time.Now()
	sent := 0
	bo := retry.OutboxBackoff()
	for _, e := range entries {
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
			log.Printf("outbox: dropping expired id=%s age=%s", entry.ID, now.Sub(entry.CreatedAt).Round(time.Minute))
			_ = os.Remove(path)
			continue
		}
		if !entry.NextRetryAt.IsZero() && now.Before(entry.NextRetryAt) {
			continue
		}
		content, err := base64.StdEncoding.DecodeString(entry.Content)
		if err != nil {
			_ = os.Remove(path)
			continue
		}
		if err := client.pushOnce(ctx, entry.GameID, entry.PathKey, entry.FilePath, "", content); err != nil {
			if !retry.IsRetryableError(err) {
				log.Printf("outbox: non-retryable id=%s: %v — removing", entry.ID, err)
				_ = os.Remove(path)
				continue
			}
			entry.Attempts++
			for i := 0; i < entry.Attempts; i++ {
				bo.Next()
			}
			entry.NextRetryAt = now.Add(bo.Current())
			if updated, err := json.Marshal(entry); err == nil {
				_ = os.WriteFile(path, updated, 0600)
			}
			log.Printf("outbox: retry failed id=%s attempts=%d next=%s: %v", entry.ID, entry.Attempts, entry.NextRetryAt.Format(time.RFC3339), err)
			continue
		}
		if err := os.Remove(path); err != nil {
			log.Printf("outbox: remove %s: %v", path, err)
		} else {
			sent++
			log.Printf("outbox: sent game=%s file=%s", entry.GameID, entry.FilePath)
		}
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
