package sync

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gsbs/gsbs/pkg/crypto"
	"github.com/gsbs/gsbs/pkg/paths"
	"github.com/gsbs/gsbs/pkg/retry"
	"golang.org/x/time/rate"
)

// ErrUnauthorized is returned when the server responds with 401. Callers should
// surface a re-login prompt and stop hammering the endpoint with retries.
var ErrUnauthorized = errors.New("unauthorized")

const maxConcurrentPushes = 4

// Client talks to the GSBS server for push/pull.
type Client struct {
	baseURL           string
	token             string
	tokenMu           sync.RWMutex // guards token and authRetried
	resolver          *paths.Resolver
	currentOS         paths.OS
	http              *http.Client
	useCompression    bool
	verbose           bool
	encryptionEnabled bool
	passphrase        string
	pushMu            sync.Mutex
	lastPushedHash    map[string]string // gameID+pathKey -> content hash
	pushSem           chan struct{}
	authRetried       bool
	guardFirstPush    bool          // send X-GSBS-If-Absent on first push of a slot (conflict-aware policies)
	TokenReload       func() string // optional: reload token from config on 401
}

// SetConflictGuard enables the expect-new precondition on the first push of a
// slot (when this client has no last-pushed hash for it). With it on, the server
// rejects the push with 409 if a different save already exists, surfacing a
// conflict instead of silently overwriting another machine's save. Leave off for
// last-write-wins, where blind overwrite is the user's intended behavior.
func (c *Client) SetConflictGuard(enabled bool) {
	c.guardFirstPush = enabled
}

// HTTP timeout for sync requests (pull can return large payloads).
const syncTimeout = 5 * time.Minute

// NewClient creates a sync client. If maxKbps > 0, sync bandwidth is limited to that many KiB/s.
// If useCompression is true, push body is gzip-compressed and pull requests Accept-Encoding: gzip.
// If verbose is true, extra detail is logged (e.g. per-file sync).
// SetEncryption configures optional E2E encryption (client-side passphrase; never sent to server).
func (c *Client) SetEncryption(enabled bool, passphrase string) {
	c.encryptionEnabled = enabled
	c.passphrase = passphrase
}

// NewClient creates a sync client. If maxKbps > 0, sync bandwidth is limited to that many KiB/s.
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
		pushSem:        make(chan struct{}, maxConcurrentPushes),
	}
	loaded := loadPushHashCache()
	if loaded != nil {
		c.lastPushedHash = make(map[string]string, len(loaded))
		for k, v := range loaded {
			c.lastPushedHash[k] = v
		}
	}
	// Preserve Authorization on redirect: Go's client strips it when following redirects to another host.
	httpClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if tok := c.getToken(); tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
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
		GameTitle   string `json:"game_title"`
		UpdatedAt   string `json:"updated_at"`
		ContentHash string `json:"content_hash"`
		Encrypted   bool   `json:"encrypted,omitempty"`
	} `json:"saves"`
}

// PullResponse is the decoded pull API response.
type PullResponse struct {
	Saves []struct {
		GameID    string `json:"game_id"`
		PathKey   string `json:"path_key"`
		UpdatedAt string `json:"updated_at"`
		Content   string `json:"content"` // base64
		Encrypted bool   `json:"encrypted,omitempty"`
	} `json:"saves"`
	Total int `json:"total,omitempty"` // total saves on server (only set when paginated)
}

// fullPullPageSize bounds how many full save blobs are fetched per request in
// the full-pull fallback so neither the server nor the client has to hold the
// entire library in memory at once. The server caps limit at 500.
const fullPullPageSize = 200

// maxPullRetries is the number of retries on transient errors (5xx, timeouts).
const maxPullRetries = 4
const pushMaxRetries = 3

func (c *Client) pullSummaries(ctx context.Context) (*SummaryResponse, error) {
	var out *SummaryResponse
	err := retry.Do(ctx, retry.DefaultBackoff(), maxPullRetries, func() error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/saves?summaries=1", nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.getToken())
		resp, err := c.http.Do(req)
		if err != nil {
			return err
		}
		defer closeIO(resp.Body)
		if resp.StatusCode != http.StatusOK {
			if resp.StatusCode == http.StatusUnauthorized {
				return fmt.Errorf("summaries: %w", ErrUnauthorized)
			}
			return fmt.Errorf("summaries: status %d", resp.StatusCode)
		}
		var decoded SummaryResponse
		if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
			return err
		}
		out = &decoded
		return nil
	})
	return out, err
}

func (c *Client) pullSingle(ctx context.Context, gameID, pathKey string) (*PullResponse, error) {
	var out *PullResponse
	url := fmt.Sprintf("%s/api/saves?game_id=%s&path_key=%s", c.baseURL, gameID, pathKey)
	err := retry.Do(ctx, retry.DefaultBackoff(), maxPullRetries, func() error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.getToken())
		if c.useCompression {
			req.Header.Set("Accept-Encoding", "gzip")
		}
		resp, err := c.http.Do(req)
		if err != nil {
			return err
		}
		defer closeIO(resp.Body)
		if resp.StatusCode != http.StatusOK {
			if resp.StatusCode == http.StatusUnauthorized {
				return fmt.Errorf("pull single: %w", ErrUnauthorized)
			}
			return fmt.Errorf("pull single: status %d", resp.StatusCode)
		}
		body := io.Reader(resp.Body)
		if strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip") {
			gr, err := gzip.NewReader(resp.Body)
			if err != nil {
				return err
			}
			defer closeIO(gr)
			body = gr
		}
		var decoded PullResponse
		if err := json.NewDecoder(body).Decode(&decoded); err != nil {
			return err
		}
		out = &decoded
		return nil
	})
	return out, err
}

func (c *Client) pullOnce(ctx context.Context) (*PullResponse, error) {
	return c.pullPage(ctx, 0, 0)
}

// pullPage fetches a page of full saves. limit<=0 fetches the whole library
// (legacy unbounded behavior); a positive limit bounds server and client memory.
func (c *Client) pullPage(ctx context.Context, limit, offset int) (*PullResponse, error) {
	url := c.baseURL + "/api/saves"
	if limit > 0 || offset > 0 {
		url = fmt.Sprintf("%s?limit=%d&offset=%d", url, limit, offset)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.getToken())
	if c.useCompression {
		req.Header.Set("Accept-Encoding", "gzip")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer closeIO(resp.Body)
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusUnauthorized {
			return nil, fmt.Errorf("pull: %w", ErrUnauthorized)
		}
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	body := io.Reader(resp.Body)
	if strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip") {
		gr, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("gzip decode: %w", err)
		}
		defer closeIO(gr)
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
		return retry.IsRetryableError(err)
	}
	return retry.IsRetryableHTTP(statusCode)
}

// PullAndApplyWithResolver fetches saves and writes using the given path resolver.
func (c *Client) PullAndApplyWithResolver(ctx context.Context, resolvePath func(gameID, pathKey string) string, opts PullOptions, onRetryIn func(time.Duration)) error {
	summaries, sumErr := c.pullSummaries(ctx)
	if sumErr == nil && len(summaries.Saves) > 0 {
		return c.applyFromSummaries(ctx, summaries, resolvePath, opts)
	}
	if sumErr != nil {
		log.Printf("pull: summaries failed, falling back to full pull: %v", sumErr)
	}
	return c.pullAndApplyFull(ctx, resolvePath, opts, onRetryIn)
}

func (c *Client) applyFromSummaries(ctx context.Context, summaries *SummaryResponse, resolvePath func(gameID, pathKey string) string, opts PullOptions) error {
	total := len(summaries.Saves)
	for i, s := range summaries.Saves {
		if OnPullProgress != nil {
			OnPullProgress(i+1, total)
		}
		absPath := resolvePath(s.GameID, s.PathKey)
		if absPath == "" {
			continue
		}
		elig := paths.EvaluatePullEligibility(absPath, s.GameID, opts.PullContext)
		if elig == paths.SkipNotInstalled || elig == paths.SkipNoAnchor {
			continue
		}
		localHash := ""
		localExists := false
		var localMtime time.Time
		if data, err := os.ReadFile(absPath); err == nil {
			localExists = true
			localHash = FileHash(data)
			// s.ContentHash is the plaintext change hash (see ContentChangeHash),
			// so this fast-path skip now works for encrypted saves too — local
			// plaintext matches the server's recorded plaintext hash.
			if s.ContentHash != "" && localHash == s.ContentHash {
				continue
			}
			if fi, err := os.Stat(absPath); err == nil {
				localMtime = fi.ModTime()
			}
		}
		serverTime, _ := time.Parse(time.RFC3339, s.UpdatedAt)
		decision := DecidePull(localExists, localHash, localMtime, s.ContentHash, serverTime, opts.ConflictPolicy)
		if decision == PullSkip {
			continue
		}
		if decision == PullConflict {
			RecordConflict(ConflictRecord{
				GameID: s.GameID, PathKey: s.PathKey, FilePath: absPath,
				LocalHash: localHash, ServerHash: s.ContentHash,
				LocalMtime: localMtime.UTC().Format(time.RFC3339), ServerUpdatedAt: s.UpdatedAt,
				PolicyApplied: opts.ConflictPolicy,
			})
			if OnConflictDetected != nil {
				OnConflictDetected(s.GameID, s.PathKey, absPath)
			}
			log.Printf("pull: conflict game=%s path=%s", s.GameID, absPath)
			continue
		}
		out, err := c.pullSingle(ctx, s.GameID, s.PathKey)
		if err != nil {
			log.Printf("pull single: game=%s path_key=%s: %v", s.GameID, s.PathKey, err)
			continue
		}
		for _, item := range out.Saves {
			if err := c.applyOneSaveEncrypted(item.GameID, item.PathKey, item.UpdatedAt, item.Content, absPath, opts, item.Encrypted); err != nil {
				log.Printf("pull apply: %v", err)
				if OnSaveEvent != nil {
					OnSaveEvent(item.GameID, item.PathKey, s.GameTitle, SaveDirPull, err)
				}
			}
		}
	}
	return nil
}

func (c *Client) pullAndApplyFull(ctx context.Context, resolvePath func(gameID, pathKey string) string, opts PullOptions, onRetryIn func(time.Duration)) error {
	// Paginate so neither the server nor this client holds the whole library in
	// memory at once. Each page is fetched (with retry/backoff) and applied
	// before the next is requested.
	offset := 0
	for {
		page, err := c.pullPageWithRetry(ctx, fullPullPageSize, offset, onRetryIn)
		if err != nil {
			return err
		}
		for _, s := range page.Saves {
			absPath := resolvePath(s.GameID, s.PathKey)
			if absPath == "" {
				continue
			}
			elig := paths.EvaluatePullEligibility(absPath, s.GameID, opts.PullContext)
			if elig == paths.SkipNotInstalled || elig == paths.SkipNoAnchor {
				continue
			}
			if err := c.applyOneSaveEncrypted(s.GameID, s.PathKey, s.UpdatedAt, s.Content, absPath, opts, s.Encrypted); err != nil {
				log.Printf("pull apply: %v", err)
				if OnSaveEvent != nil {
					OnSaveEvent(s.GameID, s.PathKey, "", SaveDirPull, err)
				}
			}
		}
		offset += len(page.Saves)
		// Stop when a short page came back, or we've covered the reported total.
		if len(page.Saves) < fullPullPageSize || (page.Total > 0 && offset >= page.Total) {
			break
		}
	}
	return nil
}

// pullPageWithRetry fetches one page of full saves, retrying transient errors
// with backoff. onRetryIn (may be nil) reports the next retry delay for UI.
func (c *Client) pullPageWithRetry(ctx context.Context, limit, offset int, onRetryIn func(time.Duration)) (*PullResponse, error) {
	bo := retry.DefaultBackoff()
	var out *PullResponse
	var lastErr error
	for attempt := 0; attempt <= maxPullRetries; attempt++ {
		if attempt > 0 {
			delay := bo.Next()
			if onRetryIn != nil {
				onRetryIn(delay)
			}
			select {
			case <-ctx.Done():
				if onRetryIn != nil {
					onRetryIn(0)
				}
				return nil, ctx.Err()
			case <-time.After(delay):
			}
			log.Printf("pull: retry %d/%d after %v", attempt, maxPullRetries, delay)
		}
		out, lastErr = c.pullPage(ctx, limit, offset)
		if lastErr == nil {
			break
		}
		if !isRetryablePullError(lastErr, retry.HTTPStatusFromError(lastErr)) {
			if onRetryIn != nil {
				onRetryIn(0)
			}
			return nil, lastErr
		}
	}
	if onRetryIn != nil {
		onRetryIn(0)
	}
	if lastErr != nil {
		return nil, fmt.Errorf("pull: %w", lastErr)
	}
	return out, nil
}

func (c *Client) applyOneSave(gameID, pathKey, updatedAt, contentB64, absPath string, opts PullOptions) error {
	return c.applyOneSaveEncrypted(gameID, pathKey, updatedAt, contentB64, absPath, opts, false)
}

func (c *Client) applyOneSaveEncrypted(gameID, pathKey, updatedAt, contentB64, absPath string, opts PullOptions, encrypted bool) error {
	raw, err := base64.StdEncoding.DecodeString(contentB64)
	if err != nil {
		return fmt.Errorf("decode game=%s: %w", gameID, err)
	}
	content, err := c.decodeContent(raw, encrypted)
	if err != nil {
		return fmt.Errorf("decrypt game=%s: %w", gameID, err)
	}
	elig := paths.EvaluatePullEligibility(absPath, gameID, opts.PullContext)
	if elig == paths.SkipNotInstalled || elig == paths.SkipNoAnchor {
		return nil
	}
	localHash := ""
	localExists := false
	var localMtime time.Time
	if data, err := os.ReadFile(absPath); err == nil {
		localExists = true
		localHash = FileHash(data)
		if fi, err := os.Stat(absPath); err == nil {
			localMtime = fi.ModTime()
		}
		_ = data
	}
	serverTime, _ := time.Parse(time.RFC3339, updatedAt)
	serverHash := FileHash(content)
	decision := DecidePull(localExists, localHash, localMtime, serverHash, serverTime, opts.ConflictPolicy)
	if decision == PullSkip {
		return nil
	}
	if decision == PullConflict {
		RecordConflict(ConflictRecord{
			GameID: gameID, PathKey: pathKey, FilePath: absPath,
			LocalHash: localHash, ServerHash: serverHash,
			LocalMtime: localMtime.UTC().Format(time.RFC3339), ServerUpdatedAt: updatedAt,
			PolicyApplied: opts.ConflictPolicy,
		})
		if OnConflictDetected != nil {
			OnConflictDetected(gameID, pathKey, absPath)
		}
		return nil
	}
	if elig == paths.ApplyCreateDir {
		if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
			return err
		}
	} else if !paths.PathExists(absPath) && elig != paths.ApplyCreateDir {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		return err
	}
	if opts.BackupBeforeOverwrite {
		if _, err := os.Stat(absPath); err == nil {
			if data, err := os.ReadFile(absPath); err == nil {
				_ = atomicWriteFile(absPath+".gsbs.bak", data, 0644)
			}
		}
	}
	if opts.WatchRoot != nil {
		if err := ValidateWriteUnderRoot(absPath, opts.WatchRoot(gameID, pathKey)); err != nil {
			return err
		}
	}
	if err := atomicWriteFile(absPath, content, 0644); err != nil {
		return err
	}
	// Suppress watcher echo: mark this content as already pushed so the
	// fsnotify Write event for the file we just wrote doesn't trigger a re-upload.
	if chHash, chErr := c.ContentChangeHash(content); chErr == nil {
		c.markPushed(gameID, pathKey, chHash)
	}
	if c.verbose {
		log.Printf("pull: wrote game=%s path=%s size=%d", gameID, absPath, len(content))
	}
	if OnSaveEvent != nil {
		OnSaveEvent(gameID, pathKey, "", SaveDirPull, nil)
	}
	return nil
}

func (c *Client) pushSlotKey(gameID, pathKey string) string {
	return gameID + "\x00" + pathKey
}

// ContentChangeHash returns the SHA256 hex of the PLAINTEXT content. This is the
// canonical change-detection / dedup key used by the push-skip cache, the
// X-Content-Hash header, optimistic-concurrency (X-GSBS-If-Hash), watcher
// echo-suppression, and reconcile.
//
// It deliberately hashes plaintext, not the encrypted wire bytes: AES-GCM uses a
// fresh random salt+nonce per encryption, so the ciphertext of identical content
// differs every time. Hashing the wire bytes (the old behavior) meant encrypted
// saves were NEVER detected as unchanged — every sync cycle re-uploaded the full
// save and minted a new server version. Hashing plaintext makes change detection
// stable and identical to the unencrypted case (where wire == plaintext). The
// error return is retained for call-site compatibility and is always nil.
func (c *Client) ContentChangeHash(content []byte) (string, error) {
	return FileHash(content), nil
}

// ShouldSkipPush reports whether content with hash was already pushed for the slot.
func (c *Client) ShouldSkipPush(gameID, pathKey, hash string) bool {
	c.pushMu.Lock()
	defer c.pushMu.Unlock()
	if c.lastPushedHash == nil {
		return false
	}
	return c.lastPushedHash[c.pushSlotKey(gameID, pathKey)] == hash
}

func (c *Client) markPushed(gameID, pathKey, hash string) {
	c.pushMu.Lock()
	if c.lastPushedHash == nil {
		c.lastPushedHash = make(map[string]string)
	}
	c.lastPushedHash[c.pushSlotKey(gameID, pathKey)] = hash
	snapshot := make(map[string]string, len(c.lastPushedHash))
	for k, v := range c.lastPushedHash {
		snapshot[k] = v
	}
	c.pushMu.Unlock()
	// Debounced: marks dirty; background flusher writes at most once per 5 s.
	markHashCacheDirty(snapshot)
}

func (c *Client) acquirePushSlot(ctx context.Context) error {
	select {
	case c.pushSem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Client) releasePushSlot() {
	select {
	case <-c.pushSem:
	default:
	}
}

func (c *Client) tryReloadToken() bool {
	if c.TokenReload == nil {
		return false
	}
	newTok := strings.TrimSpace(c.TokenReload())
	if newTok == "" {
		return false
	}
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	if newTok == c.token {
		return false
	}
	c.token = newTok
	c.authRetried = true
	logSyncInfo("push_token_reload", "success", true)
	return true
}

func (c *Client) getToken() string {
	c.tokenMu.RLock()
	defer c.tokenMu.RUnlock()
	return c.token
}

func (c *Client) getAuthRetried() bool {
	c.tokenMu.RLock()
	defer c.tokenMu.RUnlock()
	return c.authRetried
}

func (c *Client) encodeContent(plaintext []byte) (wire []byte, encrypted bool, err error) {
	if c.encryptionEnabled && c.passphrase != "" {
		enc, err := crypto.Encrypt(c.passphrase, plaintext)
		if err != nil {
			return nil, false, err
		}
		return []byte(enc), true, nil
	}
	return plaintext, false, nil
}

func (c *Client) decodeContent(wire []byte, encrypted bool) ([]byte, error) {
	if !encrypted {
		return wire, nil
	}
	if c.passphrase == "" {
		return nil, fmt.Errorf("encrypted save but no passphrase configured")
	}
	return crypto.Decrypt(c.passphrase, string(wire))
}

func (c *Client) pushOnce(ctx context.Context, gameID, pathKey, filePath, relativePath string, content []byte) error {
	if err := c.acquirePushSlot(ctx); err != nil {
		return err
	}
	defer c.releasePushSlot()

	wire, encrypted, err := c.encodeContent(content)
	if err != nil {
		return err
	}
	// Change-detection / dedup keys off the PLAINTEXT hash so encrypted saves
	// dedup correctly (encryption is non-deterministic; see ContentChangeHash).
	// X-Content-Size below still reports the encrypted wire length so quotas and
	// the dashboard reflect actual stored bytes.
	hash := FileHash(content)
	if c.ShouldSkipPush(gameID, pathKey, hash) {
		logSyncDebug("push_skip_unchanged", "game_id", gameID, "path_key", pathKey, "relative_path", relativePath, "file", filePath)
		return nil
	}
	var body io.Reader = bytes.NewReader(wire)
	if c.useCompression {
		var buf bytes.Buffer
		gw := gzip.NewWriter(&buf)
		if _, err := gw.Write(wire); err != nil {
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
	req.Header.Set("Authorization", "Bearer "+c.getToken())
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-Content-Hash", hash)
	req.Header.Set("X-Content-Size", fmt.Sprintf("%d", len(wire)))
	if encrypted {
		req.Header.Set("X-Encrypted", "1")
	}
	if c.useCompression {
		req.Header.Set("Content-Encoding", "gzip")
	}
	req.Header.Set("X-Game-ID", gameID)
	req.Header.Set("X-Path-Key", pathKey)
	req.Header.Set("X-File-Path", filePath)
	if relativePath != "" {
		req.Header.Set("X-Relative-Path", filepath.ToSlash(relativePath))
	}
	// Send the last-known hash for optimistic-concurrency conflict detection (HTTP 409).
	c.pushMu.Lock()
	lastHash := c.lastPushedHash[c.pushSlotKey(gameID, pathKey)]
	c.pushMu.Unlock()
	if lastHash != "" {
		req.Header.Set("X-GSBS-If-Hash", lastHash)
	} else if c.guardFirstPush {
		// No known hash for this slot: tell the server we expect it to be new so
		// it rejects (409) rather than blindly overwriting a different existing save.
		req.Header.Set("X-GSBS-If-Absent", "1")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer closeIO(resp.Body)
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusUnauthorized {
			if !c.getAuthRetried() && c.tryReloadToken() {
				return c.pushOnce(ctx, gameID, pathKey, filePath, relativePath, content)
			}
			msg := "push: 401 Unauthorized — token may be invalid or expired; try logging in again from the tray"
			if OnAuthError != nil {
				OnAuthError(msg)
			}
			logSyncError("push_auth_failed", "game_id", gameID, "path_key", pathKey, "relative_path", relativePath, "error", msg)
			return fmt.Errorf("push: %w", ErrUnauthorized)
		}
		if resp.StatusCode == http.StatusRequestEntityTooLarge {
			msg := readAPIError(resp.Body)
			if msg == "" {
				msg = "storage quota exceeded or save too large"
			}
			if OnQuotaError != nil {
				OnQuotaError(msg)
			}
			return fmt.Errorf("push: 413 %s", msg)
		}
		if resp.StatusCode == http.StatusConflict {
			// Server rejected push due to hash mismatch (optimistic concurrency).
			// Do NOT retry; record the conflict and surface it to the user.
			var conflictResp struct {
				Error          string `json:"error"`
				CurrentHash    string `json:"current_hash"`
				CurrentVersion int    `json:"current_version"`
			}
			respBody, _ := io.ReadAll(resp.Body)
			_ = json.Unmarshal(respBody, &conflictResp)
			log.Printf("WARNING: push conflict detected for game=%s path=%s; server has hash=%s, local has hash=%s. Use 'gsbs conflicts' or the web UI to resolve.",
				gameID, pathKey, conflictResp.CurrentHash, hash)
			RecordConflict(ConflictRecord{
				GameID:    gameID,
				PathKey:   pathKey,
				FilePath:  filePath,
				LocalHash: hash,
				ServerHash: conflictResp.CurrentHash,
				PolicyApplied: "push_conflict",
			})
			if OnConflictDetected != nil {
				OnConflictDetected(gameID, pathKey, filePath)
			}
			logSyncWarn("push_conflict_409", "game_id", gameID, "path_key", pathKey, "server_hash", conflictResp.CurrentHash, "local_hash", hash)
			return fmt.Errorf("push: conflict for game=%s path=%s (server hash differs; resolve via tray or web UI)", gameID, pathKey)
		}
		bodySnip, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		logSyncWarn("push_http_error", "game_id", gameID, "path_key", pathKey, "status", resp.StatusCode, "body_snippet", strings.TrimSpace(string(bodySnip)))
		return fmt.Errorf("push: status %d", resp.StatusCode)
	}
	respBody, _ := io.ReadAll(resp.Body)
	var status struct {
		Status string `json:"status"`
	}
	_ = json.Unmarshal(respBody, &status)
	c.markPushed(gameID, pathKey, hash)
	if status.Status == "unchanged" {
		logSyncDebug("push_skip_server_unchanged", "game_id", gameID, "path_key", pathKey, "relative_path", relativePath, "file", filePath)
	} else {
		logSyncInfo("push_ok", "game_id", gameID, "path_key", pathKey, "relative_path", relativePath, "bytes", len(wire), "file", filePath)
	}
	return nil
}

// Push uploads a save file with content hash metadata.
func (c *Client) Push(ctx context.Context, gameID, pathKey, filePath, relativePath string, content []byte) error {
	return retry.Do(ctx, retry.DefaultBackoff(), pushMaxRetries, func() error {
		return c.pushOnce(ctx, gameID, pathKey, filePath, relativePath, content)
	})
}

// FetchAccountSettings returns server-side account flags (e.g. encryption_enabled).
func (c *Client) FetchAccountSettings(ctx context.Context) (encryptionEnabled bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/account", nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+c.getToken())
	resp, err := c.http.Do(req)
	if err != nil {
		return false, err
	}
	defer closeIO(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("account: status %d", resp.StatusCode)
	}
	var out struct {
		EncryptionEnabled bool `json:"encryption_enabled"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return false, err
	}
	return out.EncryptionEnabled, nil
}

// ListVersions returns version history for a save slot.
func (c *Client) ListVersions(ctx context.Context, gameID, pathKey string) ([]map[string]interface{}, error) {
	url := fmt.Sprintf("%s/api/saves/versions?game_id=%s&path_key=%s", c.baseURL, gameID, pathKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.getToken())
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer closeIO(resp.Body)
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
	req.Header.Set("Authorization", "Bearer "+c.getToken())
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer closeIO(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("restore: status %d", resp.StatusCode)
	}
	return nil
}

// FetchServerHashes returns a map of "gameID\x00pathKey" -> content change hash
// (plaintext SHA-256; see ContentChangeHash) for all saves on the server.
// Used by startup reconciliation to find saves missing on or differing from the server.
func (c *Client) FetchServerHashes(ctx context.Context) (map[string]string, error) {
	summaries, err := c.pullSummaries(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(summaries.Saves))
	for _, s := range summaries.Saves {
		if s.ContentHash != "" {
			out[s.GameID+"\x00"+s.PathKey] = s.ContentHash
		}
	}
	return out, nil
}

func readAPIError(body io.Reader) string {
	var out struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(body).Decode(&out); err != nil {
		return ""
	}
	return strings.TrimSpace(out.Error)
}
