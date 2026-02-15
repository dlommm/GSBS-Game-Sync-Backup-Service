package sync

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gsbs/gsbs/pkg/paths"
)

// Client talks to the GSBS server for push/pull.
type Client struct {
	baseURL   string
	token     string
	resolver  *paths.Resolver
	currentOS paths.OS
	http      *http.Client
}

// HTTP timeout for sync requests (pull can return large payloads).
const syncTimeout = 5 * time.Minute

// NewClient creates a sync client.
func NewClient(baseURL, token string, resolver *paths.Resolver, currentOS paths.OS) (*Client, error) {
	return &Client{
		baseURL:   baseURL,
		token:     token,
		resolver:  resolver,
		currentOS: currentOS,
		http:      &http.Client{Timeout: syncTimeout},
	}, nil
}

// PullResponse is the decoded pull API response.
type PullResponse struct {
	Saves []struct {
		GameID   string `json:"game_id"`
		PathKey  string `json:"path_key"`
		UpdatedAt string `json:"updated_at"`
		Content  string `json:"content"` // base64
	} `json:"saves"`
}

// PullAndApplyWithResolver fetches all saves and writes using the given path resolver.
// resolvePath returns the absolute path for this client for (gameID, pathKey), or "" if not applicable.
func (c *Client) PullAndApplyWithResolver(ctx context.Context, resolvePath func(gameID, pathKey string) string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/saves", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("pull: status %d", resp.StatusCode)
	}
	var out PullResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return err
	}
	for _, s := range out.Saves {
		content, err := base64.StdEncoding.DecodeString(s.Content)
		if err != nil {
			log.Printf("pull: decode game=%s path_key=%s: %v", s.GameID, s.PathKey, err)
			continue
		}
		absPath := resolvePath(s.GameID, s.PathKey)
		if absPath == "" {
			continue
		}
		if !paths.PathExists(absPath) {
			// Folder does not exist = game not installed; do not write
			continue
		}
		if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
			log.Printf("pull: mkdir game=%s path=%s: %v", s.GameID, absPath, err)
			continue
		}
		if err := os.WriteFile(absPath, content, 0644); err != nil {
			log.Printf("pull: write game=%s path=%s: %v", s.GameID, absPath, err)
			continue
		}
		log.Printf("pull: wrote game=%s path=%s size=%d", s.GameID, absPath, len(content))
	}
	return nil
}

// Push uploads a save file.
func (c *Client) Push(ctx context.Context, gameID, pathKey, filePath string, content []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/saves", bytes.NewReader(content))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-Game-ID", gameID)
	req.Header.Set("X-Path-Key", pathKey)
	req.Header.Set("X-File-Path", filePath)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("push: status %d", resp.StatusCode)
	}
	return nil
}
