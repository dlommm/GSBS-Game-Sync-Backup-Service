package sync

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gsbs/gsbs/pkg/paths"
	"golang.org/x/time/rate"
)

// Client talks to the GSBS server for push/pull.
type Client struct {
	baseURL        string
	token          string
	resolver       *paths.Resolver
	currentOS      paths.OS
	http           *http.Client
	useCompression bool
	verbose        bool
}

// HTTP timeout for sync requests (pull can return large payloads).
const syncTimeout = 5 * time.Minute

// NewClient creates a sync client. If maxKbps > 0, sync bandwidth is limited to that many KiB/s.
// If useCompression is true, push body is gzip-compressed and pull requests Accept-Encoding: gzip.
// If verbose is true, extra detail is logged (e.g. per-file sync).
func NewClient(baseURL, token string, resolver *paths.Resolver, currentOS paths.OS, maxKbps int, useCompression bool, verbose bool) (*Client, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("token is required for sync") // avoid sending "Bearer " and getting 401 missing token
	}
	var transport http.RoundTripper = http.DefaultTransport.(*http.Transport).Clone()
	if maxKbps > 0 {
		transport = &rateLimitTransport{
			base:    transport,
			limiter: rate.NewLimiter(rate.Limit(maxKbps*1024), maxKbps*1024*2), // bytes per second, burst 2x
		}
	}
	httpClient := &http.Client{Timeout: syncTimeout, Transport: transport}
	c := &Client{
		baseURL:        baseURL,
		token:          token,
		resolver:       resolver,
		currentOS:      currentOS,
		http:           httpClient,
		useCompression: useCompression,
		verbose:        verbose,
	}
	// Preserve Authorization on redirect: Go's client strips it when following redirects to another host.
	httpClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if c.token != "" {
			req.Header.Set("Authorization", "Bearer "+c.token)
		}
		return nil
	}
	return c, nil
}

// rateLimitTransport throttles request and response body reads/writes.
type rateLimitTransport struct {
	base    http.RoundTripper
	limiter *rate.Limiter
}

func (t *rateLimitTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	if req.Body != nil {
		req.Body = &rateLimitReader{Reader: req.Body, limiter: t.limiter, ctx: ctx}
	}
	resp, err := t.base.RoundTrip(req)
	if err != nil || resp.Body == nil {
		return resp, err
	}
	resp.Body = &rateLimitReader{Reader: resp.Body, limiter: t.limiter, ctx: ctx}
	return resp, nil
}

type rateLimitReader struct {
	io.Reader
	limiter *rate.Limiter
	ctx     context.Context
}

func (r *rateLimitReader) Read(p []byte) (n int, err error) {
	n, err = r.Reader.Read(p)
	if n > 0 && r.limiter != nil {
		_ = r.limiter.WaitN(r.ctx, n)
	}
	return n, err
}

func (r *rateLimitReader) Close() error {
	if c, ok := r.Reader.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

// FileHash returns SHA256 hex of file content.
func FileHash(content []byte) string {
	h := sha256.Sum256(content)
	return hex.EncodeToString(h[:])
}

// SummaryResponse is the decoded summaries API response.
type SummaryResponse struct {
	Saves []struct {
		GameID      string `json:"game_id"`
		PathKey     string `json:"path_key"`
		UpdatedAt   string `json:"updated_at"`
		ContentHash string `json:"content_hash"`
	} `json:"saves"`
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

// maxPullRetries is the number of retries on transient errors (5xx, timeouts).
const maxPullRetries = 4

func (c *Client) pullSummaries(ctx context.Context) (*SummaryResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/saves?summaries=1", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("summaries: status %d", resp.StatusCode)
	}
	var out SummaryResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) pullSingle(ctx context.Context, gameID, pathKey string) (*PullResponse, error) {
	url := fmt.Sprintf("%s/api/saves?game_id=%s&path_key=%s", c.baseURL, gameID, pathKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if c.useCompression {
		req.Header.Set("Accept-Encoding", "gzip")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pull single: status %d", resp.StatusCode)
	}
	body := io.Reader(resp.Body)
	if strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip") {
		gr, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, err
		}
		defer gr.Close()
		body = gr
	}
	var out PullResponse
	if err := json.NewDecoder(body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) pullOnce(ctx context.Context) (*PullResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/saves", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if c.useCompression {
		req.Header.Set("Accept-Encoding", "gzip")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusUnauthorized {
			return nil, fmt.Errorf("401 Unauthorized — token may be invalid or expired; try logging in again from the tray")
		}
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	body := io.Reader(resp.Body)
	if strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip") {
		gr, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("gzip decode: %w", err)
		}
		defer gr.Close()
		body = gr
	}
	var out PullResponse
	if err := json.NewDecoder(body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

// isRetryablePullError reports whether the error is transient (retry with backoff).
func isRetryablePullError(err error, statusCode int) bool {
	if err != nil {
		return true // e.g. timeout, connection reset
	}
	return statusCode >= 500 && statusCode < 600
}

// PullAndApplyWithResolver fetches saves and writes using the given path resolver.
// Uses summary-based conditional fetch when possible (hash comparison).
func (c *Client) PullAndApplyWithResolver(ctx context.Context, resolvePath func(gameID, pathKey string) string, backupBeforeOverwrite, skipOverwriteWhenLocalNewer bool, onRetryIn func(time.Duration)) error {
	summaries, sumErr := c.pullSummaries(ctx)
	if sumErr == nil && len(summaries.Saves) > 0 {
		return c.applyFromSummaries(ctx, summaries, resolvePath, backupBeforeOverwrite, skipOverwriteWhenLocalNewer)
	}
	if sumErr != nil {
		log.Printf("pull: summaries failed, falling back to full pull: %v", sumErr)
	}
	return c.pullAndApplyFull(ctx, resolvePath, backupBeforeOverwrite, skipOverwriteWhenLocalNewer, onRetryIn)
}

func (c *Client) applyFromSummaries(ctx context.Context, summaries *SummaryResponse, resolvePath func(gameID, pathKey string) string, backupBeforeOverwrite, skipOverwriteWhenLocalNewer bool) error {
	for _, s := range summaries.Saves {
		absPath := resolvePath(s.GameID, s.PathKey)
		if absPath == "" || !paths.PathExists(absPath) {
			continue
		}
		localHash := ""
		if data, err := os.ReadFile(absPath); err == nil {
			localHash = FileHash(data)
			if s.ContentHash != "" && localHash == s.ContentHash {
				continue
			}
			if skipOverwriteWhenLocalNewer {
				if fi, err := os.Stat(absPath); err == nil {
					serverTime, err := time.Parse(time.RFC3339, s.UpdatedAt)
					if err == nil && fi.ModTime().After(serverTime) && s.ContentHash != "" && localHash != s.ContentHash {
						RecordConflict(s.GameID, s.PathKey, absPath)
						log.Printf("pull: conflict game=%s path=%s", s.GameID, absPath)
						continue
					}
				}
			}
		}
		out, err := c.pullSingle(ctx, s.GameID, s.PathKey)
		if err != nil {
			log.Printf("pull single: game=%s path_key=%s: %v", s.GameID, s.PathKey, err)
			continue
		}
		for _, item := range out.Saves {
			if err := c.applyOneSave(item.GameID, item.PathKey, item.UpdatedAt, item.Content, absPath, backupBeforeOverwrite, skipOverwriteWhenLocalNewer); err != nil {
				log.Printf("pull apply: %v", err)
			}
		}
	}
	return nil
}

func (c *Client) pullAndApplyFull(ctx context.Context, resolvePath func(gameID, pathKey string) string, backupBeforeOverwrite, skipOverwriteWhenLocalNewer bool, onRetryIn func(time.Duration)) error {
	var out *PullResponse
	var lastErr error
	backoff := 2 * time.Second
	for attempt := 0; attempt <= maxPullRetries; attempt++ {
		if attempt > 0 {
			if onRetryIn != nil {
				onRetryIn(backoff)
			}
			select {
			case <-ctx.Done():
				if onRetryIn != nil {
					onRetryIn(0)
				}
				return ctx.Err()
			case <-time.After(backoff):
				backoff *= 2
				if backoff > 30*time.Second {
					backoff = 30 * time.Second
				}
			}
			log.Printf("pull: retry %d/%d after %v", attempt, maxPullRetries, backoff/2)
		}
		out, lastErr = c.pullOnce(ctx)
		if lastErr == nil {
			break
		}
		statusCode := 0
		if strings.HasPrefix(lastErr.Error(), "status ") {
			fmt.Sscanf(lastErr.Error(), "status %d", &statusCode)
		}
		if !isRetryablePullError(lastErr, statusCode) {
			if onRetryIn != nil {
				onRetryIn(0)
			}
			return lastErr
		}
	}
	if onRetryIn != nil {
		onRetryIn(0)
	}
	if lastErr != nil {
		return fmt.Errorf("pull: %w", lastErr)
	}
	for _, s := range out.Saves {
		absPath := resolvePath(s.GameID, s.PathKey)
		if absPath == "" {
			continue
		}
		if err := c.applyOneSave(s.GameID, s.PathKey, s.UpdatedAt, s.Content, absPath, backupBeforeOverwrite, skipOverwriteWhenLocalNewer); err != nil {
			log.Printf("pull apply: %v", err)
		}
	}
	return nil
}

func (c *Client) applyOneSave(gameID, pathKey, updatedAt, contentB64, absPath string, backupBeforeOverwrite, skipOverwriteWhenLocalNewer bool) error {
	content, err := base64.StdEncoding.DecodeString(contentB64)
	if err != nil {
		return fmt.Errorf("decode game=%s: %w", gameID, err)
	}
	if !paths.PathExists(absPath) {
		return nil
	}
	if skipOverwriteWhenLocalNewer {
		if fi, err := os.Stat(absPath); err == nil && !fi.IsDir() {
			serverTime, err := time.Parse(time.RFC3339, updatedAt)
			if err == nil && fi.ModTime().After(serverTime) {
				log.Printf("pull: skip (local newer) game=%s path=%s", gameID, absPath)
				return nil
			}
		}
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		return err
	}
	if backupBeforeOverwrite {
		if _, err := os.Stat(absPath); err == nil {
			if data, err := os.ReadFile(absPath); err == nil {
				_ = os.WriteFile(absPath+".gsbs.bak", data, 0644)
			}
		}
	}
	if err := os.WriteFile(absPath, content, 0644); err != nil {
		return err
	}
	if c.verbose {
		log.Printf("pull: wrote game=%s path=%s size=%d", gameID, absPath, len(content))
	}
	return nil
}

// Push uploads a save file with content hash metadata.
func (c *Client) Push(ctx context.Context, gameID, pathKey, filePath string, content []byte) error {
	hash := FileHash(content)
	var body io.Reader = bytes.NewReader(content)
	if c.useCompression {
		var buf bytes.Buffer
		gw := gzip.NewWriter(&buf)
		if _, err := gw.Write(content); err != nil {
			return err
		}
		if err := gw.Close(); err != nil {
			return err
		}
		body = &buf
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/saves", body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-Content-Hash", hash)
	req.Header.Set("X-Content-Size", fmt.Sprintf("%d", len(content)))
	if c.useCompression {
		req.Header.Set("Content-Encoding", "gzip")
	}
	req.Header.Set("X-Game-ID", gameID)
	req.Header.Set("X-Path-Key", pathKey)
	req.Header.Set("X-File-Path", filePath)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusUnauthorized {
			return fmt.Errorf("push: 401 Unauthorized — token may be invalid or expired; try logging in again from the tray")
		}
		return fmt.Errorf("push: status %d", resp.StatusCode)
	}
	return nil
}

// ListVersions returns version history for a save slot.
func (c *Client) ListVersions(ctx context.Context, gameID, pathKey string) ([]map[string]interface{}, error) {
	url := fmt.Sprintf("%s/api/saves/versions?game_id=%s&path_key=%s", c.baseURL, gameID, pathKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("versions: status %d", resp.StatusCode)
	}
	var out struct {
		Versions []map[string]interface{} `json:"versions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Versions, nil
}

// RestoreVersion restores a previous save version on the server.
func (c *Client) RestoreVersion(ctx context.Context, gameID, pathKey string, version int) error {
	body, _ := json.Marshal(map[string]interface{}{
		"game_id": gameID, "path_key": pathKey, "version": version,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/saves/versions/restore", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("restore: status %d", resp.StatusCode)
	}
	return nil
}
