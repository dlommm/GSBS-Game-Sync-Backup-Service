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
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gsbs/gsbs/pkg/crypto"
	"github.com/gsbs/gsbs/pkg/paths"
	"github.com/gsbs/gsbs/pkg/retry"
	"golang.org/x/time/rate"
)

// ErrUnauthorized is returned when the server responds with 401. Callers should
// surface a re-login prompt and stop hammering the endpoint with retries.
var ErrUnauthorized = errors.New("unauthorized")

// ErrQuotaExceeded marks a 413 push rejection (storage quota or size cap).
// Non-retryable: the outcome cannot change until the user frees space.
var ErrQuotaExceeded = errors.New("quota exceeded or save too large")

// ErrConflict marks a 409 push rejection (optimistic-concurrency hash
// mismatch). Non-retryable and never outboxed: the conflict record + user
// resolution is the recovery path.
var ErrConflict = errors.New("push conflict")

const maxConcurrentPushes = 4

// Client talks to the GSBS server for push/pull.
type Client struct {
	baseURL           string
	token             string
	tokenMu           sync.RWMutex // guards token
	resolver          *paths.Resolver
	currentOS         paths.OS
	http              *http.Client
	useCompression    bool
	verbose           bool
	encMu             sync.RWMutex // guards encryptionEnabled and passphrase
	encryptionEnabled bool
	passphrase        string
	pushMu            sync.Mutex
	lastPushedHash    map[string]string // gameID+pathKey -> content hash
	pushSem           chan struct{}
	guardFirstPush    bool          // send X-GSBS-If-Absent on first push of a slot (conflict-aware policies)
	TokenReload       func() string // optional: reload token from config on 401

	// Crypto-v2 fleet negotiation: the server reports crypto_v2_ready when
	// every recently-seen device on the account can read the Argon2id format;
	// only then do we write it. A config override can force either format.
	cryptoV2Ready    atomic.Bool
	cryptoV2Override *bool

	// serverClockSkew is the estimated (server clock − local clock) offset in
	// nanoseconds, measured from response Date headers on fast round-trips.
	// Pull decisions subtract it so mtime-vs-updated_at comparisons happen on
	// one clock instead of two.
	serverClockSkew atomic.Int64
}

// noteServerClock updates the clock-offset estimate from a response's Date
// header. Only fast round-trips are trusted, and offsets inside the Date
// header's ±5s noise floor (1-second granularity plus queuing) collapse to
// zero so well-synced clocks are never "corrected".
func (c *Client) noteServerClock(resp *http.Response, started time.Time) {
	rtt := time.Since(started)
	if rtt <= 0 || rtt > 30*time.Second {
		return
	}
	d, err := http.ParseTime(resp.Header.Get("Date"))
	if err != nil {
		return
	}
	offset := d.Sub(started.Add(rtt / 2)) // compare against the request midpoint
	if offset > -5*time.Second && offset < 5*time.Second {
		offset = 0
	}
	c.serverClockSkew.Store(int64(offset))
}

// serverClockOffset returns the current (server − local) offset estimate.
func (c *Client) serverClockOffset() time.Duration {
	return time.Duration(c.serverClockSkew.Load())
}

// SetCryptoV2Override pins the save-encryption write format: true forces the
// v2 (Argon2id) format, false pins legacy, nil (default) follows the server's
// fleet-readiness signal. Reading always auto-detects both formats.
func (c *Client) SetCryptoV2Override(v *bool) {
	c.cryptoV2Override = v
}

func (c *Client) useCryptoV2() bool {
	if c.cryptoV2Override != nil {
		return *c.cryptoV2Override
	}
	return c.cryptoV2Ready.Load()
}

// clientAppVersion is stamped on every API request as X-GSBS-Client-Version;
// the server uses it for the crypto-v2 fleet-readiness computation and shows
// it on the Devices page. Set once at startup by the main package.
var clientAppVersion atomic.Value // string

// SetClientAppVersion records this build's version for API requests.
func SetClientAppVersion(v string) {
	clientAppVersion.Store(strings.TrimSpace(v))
}

func appVersionHeader() string {
	if s, ok := clientAppVersion.Load().(string); ok {
		return s
	}
	return ""
}

// versionHeaderTransport adds X-GSBS-Client-Version to every request without
// touching the ~10 call sites that build requests individually.
type versionHeaderTransport struct {
	base http.RoundTripper
}

func (t *versionHeaderTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if v := appVersionHeader(); v != "" && req.Header.Get("X-GSBS-Client-Version") == "" {
		req = req.Clone(req.Context())
		req.Header.Set("X-GSBS-Client-Version", v)
	}
	return t.base.RoundTrip(req)
}

// SetConflictGuard enables the expect-new precondition on the first push of a
// slot (when this client has no last-pushed hash for it). With it on, the server
// rejects the push with 409 if a different save already exists, surfacing a
// conflict instead of silently overwriting another machine's save. Since 4.0.0
// the client enables this under every conflict policy — last_write_wins still
// governs subsequent pushes and pulls, but never a blind first overwrite. The
// setter remains for tests.
func (c *Client) SetConflictGuard(enabled bool) {
	c.guardFirstPush = enabled
}

// HTTP timeout for sync requests (pull can return large payloads).
const syncTimeout = 5 * time.Minute

// NewClient creates a sync client. If maxKbps > 0, sync bandwidth is limited to that many KiB/s.
// If useCompression is true, push body is gzip-compressed and pull requests Accept-Encoding: gzip.
// If verbose is true, extra detail is logged (e.g. per-file sync).
// SetEncryption configures optional E2E encryption (client-side passphrase; never sent to server).
// Safe to call while sync workers are running (a background account-settings
// refresh may update it mid-session).
func (c *Client) SetEncryption(enabled bool, passphrase string) {
	c.encMu.Lock()
	c.encryptionEnabled = enabled
	c.passphrase = passphrase
	c.encMu.Unlock()
}

// encryptionState snapshots the encryption settings for one operation.
func (c *Client) encryptionState() (enabled bool, passphrase string) {
	c.encMu.RLock()
	defer c.encMu.RUnlock()
	return c.encryptionEnabled, c.passphrase
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
	transport = &versionHeaderTransport{base: transport}
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
	// Preserve Authorization on redirect: Go's client strips it when following
	// redirects to another host. Re-add it ONLY when the redirect stays on the
	// configured server's host — a redirect elsewhere must never receive the
	// bearer token.
	baseHost := ""
	if u, err := url.Parse(baseURL); err == nil {
		baseHost = u.Host
	}
	httpClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if tok := c.getToken(); tok != "" && baseHost != "" && req.URL.Host == baseHost {
			req.Header.Set("Authorization", "Bearer "+tok) //nolint:gosec // G119: re-added only when the redirect stays on the configured server host
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

// DownloadedSave is one decrypted save fetched for local export.
type DownloadedSave struct {
	GameID       string
	PathKey      string
	RelativePath string // may be empty on very old server rows
	Content      []byte // decrypted plaintext (the local passphrase is applied)
	UpdatedAt    string
}

// DownloadAll fetches every save (optionally filtered to one game) and
// decrypts encrypted payloads locally — the basis of `gsbs-client export`.
func (c *Client) DownloadAll(ctx context.Context, gameID string) ([]DownloadedSave, error) {
	summaries, err := c.pullSummaries(ctx)
	if err != nil {
		return nil, err
	}
	var out []DownloadedSave
	for _, s := range summaries.Saves {
		if gameID != "" && s.GameID != gameID {
			continue
		}
		resp, err := c.pullSingle(ctx, s.GameID, s.PathKey)
		if err != nil {
			return nil, fmt.Errorf("download %s/%s: %w", s.GameID, s.PathKey, err)
		}
		for _, entry := range resp.Saves {
			raw, err := base64.StdEncoding.DecodeString(entry.Content)
			if err != nil {
				return nil, fmt.Errorf("decode %s/%s: %w", s.GameID, s.PathKey, err)
			}
			content, err := c.decodeContent(raw, entry.Encrypted)
			if err != nil {
				return nil, fmt.Errorf("decrypt %s/%s: %w (is the encryption passphrase configured?)", s.GameID, s.PathKey, err)
			}
			out = append(out, DownloadedSave{
				GameID: s.GameID, PathKey: s.PathKey, RelativePath: s.RelativePath,
				Content: content, UpdatedAt: entry.UpdatedAt,
			})
		}
	}
	return out, nil
}

// FileHash returns SHA256 hex of file content.
func FileHash(content []byte) string {
	h := sha256.Sum256(content)
	return hex.EncodeToString(h[:])
}

// SummaryResponse is the decoded summaries API response.
type SummaryResponse struct {
	Saves []struct {
		GameID       string `json:"game_id"`
		PathKey      string `json:"path_key"`
		GameTitle    string `json:"game_title"`
		UpdatedAt    string `json:"updated_at"`
		ContentHash  string `json:"content_hash"`
		Encrypted    bool   `json:"encrypted,omitempty"`
		RelativePath string `json:"relative_path,omitempty"`
	} `json:"saves"`
	// CryptoV2Ready is the server's fleet signal: every recently-seen device
	// on the account can read the v2 save-encryption format. Absent on old
	// servers (zero value keeps writing the legacy format).
	CryptoV2Ready bool `json:"crypto_v2_ready,omitempty"`
}

// PullResponse is the decoded pull API response.
type PullResponse struct {
	Saves []struct {
		GameID    string `json:"game_id"`
		PathKey   string `json:"path_key"`
		UpdatedAt string `json:"updated_at"`
		Content   string `json:"content"` // base64
		Encrypted bool   `json:"encrypted,omitempty"`
		// ContentHash is the plaintext SHA-256 the server recorded at push time.
		// Absent on pre-5.3 servers (empty value skips pull verification).
		ContentHash string `json:"content_hash,omitempty"`
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
		// Two iterations max: a 401 gets one chance to pick up a rotated
		// token (the monthly rotation otherwise wedges pull-only devices).
		for attempt := 0; ; attempt++ {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/saves?summaries=1", nil)
			if err != nil {
				return err
			}
			req.Header.Set("Authorization", "Bearer "+c.getToken())
			started := time.Now()
			resp, err := c.http.Do(req)
			if err != nil {
				return err
			}
			if resp.StatusCode != http.StatusOK {
				status := resp.StatusCode
				closeIO(resp.Body)
				if status == http.StatusUnauthorized {
					if attempt == 0 && c.tryReloadToken() {
						continue
					}
					return fmt.Errorf("summaries: %w (%w)", ErrUnauthorized, &retry.HTTPError{Status: status})
				}
				return fmt.Errorf("summaries: status %d", status)
			}
			c.noteServerClock(resp, started)
			var decoded SummaryResponse
			decodeErr := json.NewDecoder(resp.Body).Decode(&decoded)
			closeIO(resp.Body)
			if decodeErr != nil {
				return decodeErr
			}
			c.cryptoV2Ready.Store(decoded.CryptoV2Ready)
			out = &decoded
			return nil
		}
	})
	return out, err
}

func (c *Client) pullSingle(ctx context.Context, gameID, pathKey string) (*PullResponse, error) {
	var out *PullResponse
	url := fmt.Sprintf("%s/api/saves?game_id=%s&path_key=%s", c.baseURL, gameID, pathKey)
	err := retry.Do(ctx, retry.DefaultBackoff(), maxPullRetries, func() error {
		doGet := func() (*http.Response, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				return nil, err
			}
			req.Header.Set("Authorization", "Bearer "+c.getToken())
			if c.useCompression {
				req.Header.Set("Accept-Encoding", "gzip")
			}
			return c.http.Do(req)
		}
		resp, err := doGet()
		if err != nil {
			return err
		}
		if resp.StatusCode == http.StatusUnauthorized && c.tryReloadToken() {
			// One in-place retry with the rotated token.
			closeIO(resp.Body)
			resp, err = doGet()
			if err != nil {
				return err
			}
		}
		defer closeIO(resp.Body)
		if resp.StatusCode != http.StatusOK {
			if resp.StatusCode == http.StatusUnauthorized {
				return fmt.Errorf("pull single: %w (%w)", ErrUnauthorized, &retry.HTTPError{Status: http.StatusUnauthorized})
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
	doGet := func() (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.getToken())
		if c.useCompression {
			req.Header.Set("Accept-Encoding", "gzip")
		}
		return c.http.Do(req)
	}
	resp, err := doGet()
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized && c.tryReloadToken() {
		// One in-place retry with the rotated token.
		closeIO(resp.Body)
		resp, err = doGet()
		if err != nil {
			return nil, err
		}
	}
	defer closeIO(resp.Body)
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusUnauthorized {
			return nil, fmt.Errorf("pull: %w (%w)", ErrUnauthorized, &retry.HTTPError{Status: http.StatusUnauthorized})
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

// PullStats summarizes one pull pass so the tray/UI can report real numbers
// (they previously always showed zero — SyncEndStats{} was hardcoded).
type PullStats struct {
	Applied   int // saves actually written to disk
	Skipped   int // unchanged / policy-skipped
	Conflicts int // conflicts recorded this pass
	Errors    int // per-slot apply failures
	// Games are the IDs with at least one applied save this pass.
	Games map[string]bool
}

func (s *PullStats) noteGame(gameID string) {
	if s.Games == nil {
		s.Games = make(map[string]bool)
	}
	s.Games[gameID] = true
}

// PullAndApplyWithResolver fetches saves and writes using the given path resolver.
func (c *Client) PullAndApplyWithResolver(ctx context.Context, resolvePath func(gameID, pathKey string) string, opts PullOptions, onRetryIn func(time.Duration)) (PullStats, error) {
	summaries, sumErr := c.pullSummaries(ctx)
	if sumErr == nil && len(summaries.Saves) > 0 {
		return c.applyFromSummaries(ctx, summaries, resolvePath, opts)
	}
	if sumErr != nil {
		log.Printf("pull: summaries failed, falling back to full pull: %v", sumErr)
	}
	return c.pullAndApplyFull(ctx, resolvePath, opts, onRetryIn)
}

func (c *Client) applyFromSummaries(ctx context.Context, summaries *SummaryResponse, resolvePath func(gameID, pathKey string) string, opts PullOptions) (PullStats, error) {
	var stats PullStats
	total := len(summaries.Saves)
	for i, s := range summaries.Saves {
		if OnPullProgress != nil {
			OnPullProgress(i+1, total)
		}
		absPath := resolvePath(s.GameID, s.PathKey)
		if absPath == "" {
			continue
		}
		// Slots pushed by pre-5.3 clients can point at our own backup files;
		// never restore them (and never fetch their content).
		if IsGSBSArtifact(absPath) {
			logSyncDebug("pull_skip_gsbs_artifact", "game_id", s.GameID, "path_key", s.PathKey, "path", absPath)
			continue
		}
		if opts.SkipGame != nil && opts.SkipGame(s.GameID) {
			logSyncDebug("pull_deferred_game_running", "game_id", s.GameID, "path_key", s.PathKey)
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
				stats.Skipped++
				continue
			}
			if fi, err := os.Stat(absPath); err == nil {
				localMtime = fi.ModTime()
			}
		}
		serverTime, _ := time.Parse(time.RFC3339, s.UpdatedAt)
		// Translate the server timestamp onto the local clock before the
		// mtime comparison; DecidePull's skew window absorbs the residue.
		serverTime = serverTime.Add(-c.serverClockOffset())
		decision := DecidePull(localExists, localHash, localMtime, s.ContentHash, serverTime, opts.policyFor(s.GameID))
		if decision == PullSkip {
			stats.Skipped++
			continue
		}
		if decision == PullConflict {
			stats.Conflicts++
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
			stats.Errors++
			continue
		}
		for _, item := range out.Saves {
			applied, err := c.applyOneSaveEncrypted(item.GameID, item.PathKey, item.UpdatedAt, item.Content, absPath, opts, item.Encrypted, s.ContentHash)
			if err != nil {
				log.Printf("pull apply: %v", err)
				stats.Errors++
				if OnSaveEvent != nil {
					OnSaveEvent(item.GameID, item.PathKey, s.GameTitle, SaveDirPull, err)
				}
				continue
			}
			if applied {
				stats.Applied++
				stats.noteGame(item.GameID)
			} else {
				stats.Skipped++
			}
		}
	}
	return stats, nil
}

func (c *Client) pullAndApplyFull(ctx context.Context, resolvePath func(gameID, pathKey string) string, opts PullOptions, onRetryIn func(time.Duration)) (PullStats, error) {
	var stats PullStats
	// Paginate so neither the server nor this client holds the whole library in
	// memory at once. Each page is fetched (with retry/backoff) and applied
	// before the next is requested.
	offset := 0
	for {
		page, err := c.pullPageWithRetry(ctx, fullPullPageSize, offset, onRetryIn)
		if err != nil {
			return stats, err
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
			applied, err := c.applyOneSaveEncrypted(s.GameID, s.PathKey, s.UpdatedAt, s.Content, absPath, opts, s.Encrypted, s.ContentHash)
			if err != nil {
				log.Printf("pull apply: %v", err)
				stats.Errors++
				if OnSaveEvent != nil {
					OnSaveEvent(s.GameID, s.PathKey, "", SaveDirPull, err)
				}
				continue
			}
			if applied {
				stats.Applied++
				stats.noteGame(s.GameID)
			} else {
				stats.Skipped++
			}
		}
		offset += len(page.Saves)
		// Stop when a short page came back, or we've covered the reported total.
		if len(page.Saves) < fullPullPageSize || (page.Total > 0 && offset >= page.Total) {
			break
		}
	}
	return stats, nil
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
	_, err := c.applyOneSaveEncrypted(gameID, pathKey, updatedAt, contentB64, absPath, opts, false, "")
	return err
}

// applyOneSaveEncrypted writes one pulled save to disk. expectedHash, when
// non-empty, is the server-advertised plaintext SHA-256; the downloaded
// content must match it or nothing is written (end-to-end pull integrity;
// empty on pre-5.3 servers). applied reports whether the file was written
// (false = skipped by policy/eligibility/guards), so pull stats stay honest.
func (c *Client) applyOneSaveEncrypted(gameID, pathKey, updatedAt, contentB64, absPath string, opts PullOptions, encrypted bool, expectedHash string) (applied bool, err error) {
	if opts.SkipGame != nil && opts.SkipGame(gameID) {
		logSyncDebug("pull_deferred_game_running", "game_id", gameID, "path_key", pathKey)
		return false, nil
	}
	// Slots pushed by pre-5.3 clients can point at our own backup files;
	// never restore them over the local backup.
	if IsGSBSArtifact(absPath) {
		logSyncDebug("pull_skip_gsbs_artifact", "game_id", gameID, "path_key", pathKey, "path", absPath)
		return false, nil
	}
	raw, err := base64.StdEncoding.DecodeString(contentB64)
	if err != nil {
		return false, fmt.Errorf("decode game=%s: %w", gameID, err)
	}
	content, err := c.decodeContent(raw, encrypted)
	if err != nil {
		return false, fmt.Errorf("decrypt game=%s: %w", gameID, err)
	}
	serverHash := FileHash(content)
	if expectedHash != "" && serverHash != expectedHash {
		return false, fmt.Errorf("pull integrity: content hash mismatch game=%s path_key=%s (server advertised %s, downloaded %s)", gameID, pathKey, expectedHash, serverHash)
	}
	elig := paths.EvaluatePullEligibility(absPath, gameID, opts.PullContext)
	if elig == paths.SkipNotInstalled || elig == paths.SkipNoAnchor {
		return false, nil
	}
	localHash := ""
	localExists := false
	var localData []byte
	var localMtime time.Time
	if data, err := os.ReadFile(absPath); err == nil {
		localExists = true
		localData = data
		localHash = FileHash(data)
		if fi, err := os.Stat(absPath); err == nil {
			localMtime = fi.ModTime()
		}
	}
	serverTime, _ := time.Parse(time.RFC3339, updatedAt)
	// Same one-clock translation as the summaries path.
	serverTime = serverTime.Add(-c.serverClockOffset())
	decision := DecidePull(localExists, localHash, localMtime, serverHash, serverTime, opts.policyFor(gameID))
	if decision == PullSkip {
		return false, nil
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
		return false, nil
	}
	if len(content) == 0 && localExists {
		// The push paths never upload empty files, so an empty server blob is
		// suspect — refuse to clobber existing local data with it.
		logSyncWarn("pull_skip_empty_server_content", "game_id", gameID, "path_key", pathKey, "path", absPath)
		return false, nil
	}
	if elig != paths.ApplyCreateDir && !paths.PathExists(absPath) {
		return false, nil
	}
	// Validate the target BEFORE any filesystem mutation (MkdirAll/backup):
	// a save whose resolved path escapes its watch root must not create
	// directories or drop backup files outside the root either.
	if opts.WatchRoot != nil {
		root := opts.WatchRoot(gameID, pathKey)
		if root == "" {
			if !localExists {
				// Fail closed for new files: without a resolved root we cannot
				// prove the write stays inside the game's save area. Heals on a
				// later pull once install roots/manifest resolution catch up.
				logSyncWarn("pull_blocked_no_watch_root", "game_id", gameID, "path_key", pathKey, "path", absPath)
				return false, nil
			}
			// Overwrite-in-place of a file our own resolver located is safe
			// even when the root anchor is unavailable.
			logSyncDebug("pull_overwrite_no_watch_root", "game_id", gameID, "path_key", pathKey, "path", absPath)
		} else if err := ValidateWriteUnderRoot(absPath, root); err != nil {
			return false, err
		}
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		return false, err
	}
	if opts.BackupBeforeOverwrite && localExists {
		// A failed backup must abort the overwrite: the option promises the
		// previous local state survives every pull.
		if err := atomicWriteFile(absPath+".gsbs.bak", localData, 0644); err != nil {
			return false, fmt.Errorf("backup before overwrite game=%s path=%s: %w", gameID, absPath, err)
		}
	}
	if err := atomicWriteFile(absPath, content, 0644); err != nil {
		return false, err
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
	return true, nil
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

// tryReloadToken picks up a rotated token from TokenReload. Returns false when
// the reloaded token is unchanged — that is the recursion/loop stopper for the
// retry-once-per-request auth pattern, so no cross-request latch is needed
// (a latch here once wedged every long-running install after its second
// monthly rotation).
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
	logSyncInfo("push_token_reload", "success", true)
	return true
}

// SetToken updates the bearer token in the live client (the monthly rotation
// ticker rewrites config; without this the running client keeps the old token
// until the next 401).
func (c *Client) SetToken(tok string) {
	tok = strings.TrimSpace(tok)
	if tok == "" {
		return
	}
	c.tokenMu.Lock()
	c.token = tok
	c.tokenMu.Unlock()
}

func (c *Client) getToken() string {
	c.tokenMu.RLock()
	defer c.tokenMu.RUnlock()
	return c.token
}

func (c *Client) encodeContent(plaintext []byte) (wire []byte, encrypted bool, err error) {
	enabled, passphrase := c.encryptionState()
	if enabled && passphrase != "" {
		var enc string
		var err error
		if c.useCryptoV2() {
			enc, err = crypto.EncryptV2(passphrase, plaintext)
		} else {
			enc, err = crypto.Encrypt(passphrase, plaintext)
		}
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
	_, passphrase := c.encryptionState()
	if passphrase == "" {
		return nil, fmt.Errorf("encrypted save but no passphrase configured")
	}
	return crypto.Decrypt(passphrase, string(wire))
}

func (c *Client) pushOnce(ctx context.Context, gameID, pathKey, filePath, relativePath string, content []byte) error {
	return c.pushOnceAttempt(ctx, gameID, pathKey, filePath, relativePath, content, true)
}

// pushOnceAttempt performs one push. allowAuthRetry permits a single in-place
// retry after a successful token reload on 401 — per REQUEST, not per client
// lifetime, so every rotation gets its own chance to heal.
func (c *Client) pushOnceAttempt(ctx context.Context, gameID, pathKey, filePath, relativePath string, content []byte, allowAuthRetry bool) error {
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
			if allowAuthRetry && c.tryReloadToken() {
				return c.pushOnceAttempt(ctx, gameID, pathKey, filePath, relativePath, content, false)
			}
			msg := "push: 401 Unauthorized — token may be invalid or expired; try logging in again from the tray"
			if OnAuthError != nil {
				OnAuthError(msg)
			}
			logSyncError("push_auth_failed", "game_id", gameID, "path_key", pathKey, "relative_path", relativePath, "error", msg)
			// Wrap the typed status so retry classification never mistakes an
			// auth failure for a retryable transport error (the message alone
			// carries no "status N" for the legacy parser to find).
			return fmt.Errorf("push: %w (%w)", ErrUnauthorized, &retry.HTTPError{Status: http.StatusUnauthorized})
		}
		if resp.StatusCode == http.StatusRequestEntityTooLarge {
			msg := readAPIError(resp.Body)
			if msg == "" {
				msg = "storage quota exceeded or save too large"
			}
			if OnQuotaError != nil {
				OnQuotaError(msg)
			}
			return fmt.Errorf("push: %w: %s (%w)", ErrQuotaExceeded, msg, &retry.HTTPError{Status: http.StatusRequestEntityTooLarge, Msg: msg})
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
				GameID:        gameID,
				PathKey:       pathKey,
				FilePath:      filePath,
				LocalHash:     hash,
				ServerHash:    conflictResp.CurrentHash,
				PolicyApplied: "push_conflict",
			})
			if OnConflictDetected != nil {
				OnConflictDetected(gameID, pathKey, filePath)
			}
			logSyncWarn("push_conflict_409", "game_id", gameID, "path_key", pathKey, "server_hash", conflictResp.CurrentHash, "local_hash", hash)
			return fmt.Errorf("push: %w for game=%s path=%s (server hash differs; resolve via tray or web UI) (%w)",
				ErrConflict, gameID, pathKey, &retry.HTTPError{Status: http.StatusConflict})
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
	state, err := c.FetchServerState(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(state))
	for k, v := range state {
		out[k] = v.Hash
	}
	return out, nil
}

// ServerSaveInfo is one slot's server-side state as seen by reconcile.
type ServerSaveInfo struct {
	Hash string
	// UpdatedAt is the server timestamp translated onto the LOCAL clock
	// (serverClockOffset already subtracted), so callers can compare it to
	// file mtimes directly. Zero when the server row carried no timestamp.
	UpdatedAt time.Time
}

// FetchServerState returns "gameID\x00pathKey" -> {hash, updated_at} for every
// slot with a recorded hash. Reconcile uses the timestamp to detect a local
// file that is NEWER than the server copy (e.g. its failed push aged out of
// the outbox) — hash alone can't distinguish "local newer" from "local stale".
func (c *Client) FetchServerState(ctx context.Context) (map[string]ServerSaveInfo, error) {
	summaries, err := c.pullSummaries(ctx)
	if err != nil {
		return nil, err
	}
	offset := c.serverClockOffset()
	out := make(map[string]ServerSaveInfo, len(summaries.Saves))
	for _, s := range summaries.Saves {
		if s.ContentHash == "" {
			continue
		}
		info := ServerSaveInfo{Hash: s.ContentHash}
		if t, perr := time.Parse(time.RFC3339, s.UpdatedAt); perr == nil {
			info.UpdatedAt = t.Add(-offset)
		}
		out[s.GameID+"\x00"+s.PathKey] = info
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
