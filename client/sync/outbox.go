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
)

// OutboxEntry is a failed push persisted for later retry.
type OutboxEntry struct {
	ID        string    `json:"id"`
	GameID    string    `json:"game_id"`
	PathKey   string    `json:"path_key"`
	FilePath  string    `json:"file_path"`
	Content   string    `json:"content"` // base64
	CreatedAt time.Time `json:"created_at"`
	Attempts  int       `json:"attempts"`
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
		ID:        id,
		GameID:    gameID,
		PathKey:   pathKey,
		FilePath:  filePath,
		Content:   base64.StdEncoding.EncodeToString(content),
		CreatedAt: time.Now(),
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

// ProcessOutbox retries all pending outbox entries. Returns count of successfully sent items.
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
	sent := 0
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
			os.Remove(path)
			continue
		}
		content, err := base64.StdEncoding.DecodeString(entry.Content)
		if err != nil {
			os.Remove(path)
			continue
		}
		if err := client.Push(ctx, entry.GameID, entry.PathKey, entry.FilePath, content); err != nil {
			entry.Attempts++
			if updated, err := json.Marshal(entry); err == nil {
				_ = os.WriteFile(path, updated, 0600)
			}
			log.Printf("outbox: retry failed id=%s attempts=%d: %v", entry.ID, entry.Attempts, err)
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
