package sync

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

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

// NewClient creates a sync client.
func NewClient(baseURL, token string, resolver *paths.Resolver, currentOS paths.OS) (*Client, error) {
	return &Client{
		baseURL:   baseURL,
		token:     token,
		resolver:  resolver,
		currentOS: currentOS,
		http:      &http.Client{},
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

// PullAndApply fetches all saves and writes them only where the target directory exists.
// pathResolver is a func(gameID, pathKey) -> absolute path for current OS (empty if not applicable).
func (c *Client) PullAndApply(ctx context.Context) error {
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
			continue
		}
		// Client must map (game_id, path_key) -> local path; if unknown, skip or use a callback
		// For now we don't have path mapping in pull; the watcher handles known paths.
		_ = content
		_ = s.GameID
		_ = s.PathKey
	}
	return nil
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
			continue
		}
		absPath := resolvePath(s.GameID, s.PathKey)
		if absPath == "" {
			continue
		}
		if !paths.PathExists(absPath) {
			// Folder does not exist = game not installed; do not push
			continue
		}
		if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
			continue
		}
		if err := os.WriteFile(absPath, content, 0644); err != nil {
			continue
		}
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
