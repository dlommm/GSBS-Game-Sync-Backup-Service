package sync

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
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
	var transport http.RoundTripper = http.DefaultTransport.(*http.Transport).Clone()
	if maxKbps > 0 {
		transport = &rateLimitTransport{
			base:    transport,
			limiter: rate.NewLimiter(rate.Limit(maxKbps*1024), maxKbps*1024*2), // bytes per second, burst 2x
		}
	}
	return &Client{
		baseURL:        baseURL,
		token:          token,
		resolver:       resolver,
		currentOS:      currentOS,
		http:           &http.Client{Timeout: syncTimeout, Transport: transport},
		useCompression: useCompression,
		verbose:        verbose,
	}, nil
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
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	var out PullResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
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

// PullAndApplyWithResolver fetches all saves and writes using the given path resolver.
// resolvePath returns the absolute path for this client for (gameID, pathKey), or "" if not applicable.
// If backupBeforeOverwrite is true, an existing file at the target path is copied to <path>.gsbs.bak before overwriting.
// If skipOverwriteWhenLocalNewer is true, a file is not overwritten when the local file is newer than the server's updated_at.
// On transient HTTP errors (5xx, timeouts) retries with exponential backoff (2s, 4s, 8s).
// If onRetryIn is non-nil, it is called with the delay before each retry wait and with 0 when done (success or final failure).
func (c *Client) PullAndApplyWithResolver(ctx context.Context, resolvePath func(gameID, pathKey string) string, backupBeforeOverwrite, skipOverwriteWhenLocalNewer bool, onRetryIn func(time.Duration)) error {
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
		if skipOverwriteWhenLocalNewer {
			if fi, err := os.Stat(absPath); err == nil && !fi.IsDir() {
				serverTime, err := time.Parse(time.RFC3339, s.UpdatedAt)
				if err == nil && fi.ModTime().After(serverTime) {
					log.Printf("pull: skip (local newer) game=%s path=%s", s.GameID, absPath)
					continue
				}
			}
		}
		if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
			log.Printf("pull: mkdir game=%s path=%s: %v", s.GameID, absPath, err)
			continue
		}
		if backupBeforeOverwrite {
			if _, err := os.Stat(absPath); err == nil {
				backupPath := absPath + ".gsbs.bak"
				if data, err := os.ReadFile(absPath); err == nil {
					if err := os.WriteFile(backupPath, data, 0644); err != nil {
						log.Printf("pull: backup game=%s path=%s: %v", s.GameID, absPath, err)
					}
				}
			}
		}
		if err := os.WriteFile(absPath, content, 0644); err != nil {
			log.Printf("pull: write game=%s path=%s: %v", s.GameID, absPath, err)
			continue
		}
		if c.verbose {
			log.Printf("pull: wrote game=%s path=%s size=%d", s.GameID, absPath, len(content))
		}
	}
	return nil
}

// Push uploads a save file.
func (c *Client) Push(ctx context.Context, gameID, pathKey, filePath string, content []byte) error {
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
		return fmt.Errorf("push: status %d", resp.StatusCode)
	}
	return nil
}
