package pcgw

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	max429Retries     = 5
	default429Backoff = 60 * time.Second
	// maxRetryAfter caps an honored Retry-After. A hostile or misconfigured
	// upstream could otherwise park the whole sync for the job's full 24 h
	// deadline, silently.
	maxRetryAfter    = 15 * time.Minute
	max5xxRetries    = 3
	init5xxBackoff   = 1 * time.Second
	max5xxBackoffDur = 30 * time.Second
	defaultBaseURL   = "https://www.pcgamingwiki.com"
	defaultUserAgent = "GSBS/1.0 (https://github.com/gsbs/gsbs; game-save-sync)"
	defaultRateLimit = 2 * time.Second
)

// Client talks to PCGamingWiki MediaWiki API.
type Client struct {
	HTTP *http.Client
	// BaseURL overrides the API origin (for tests). Empty uses defaultBaseURL.
	BaseURL string
	// CargoBackend selects how Cargo tables are read: CargoBackendExport
	// (Special:CargoExport, the default) or CargoBackendAPI (action=cargoquery,
	// which PCGW now answers only for authenticated callers). Empty falls back
	// to GSBS_PCGW_CARGO_BACKEND, then to the default.
	CargoBackend string
	// testBackoff overrides 5xx/network retry sleep duration (zero means use real backoff).
	testBackoff time.Duration

	mu          sync.Mutex
	lastRequest time.Time
}

// NewClient returns a new PCGW API client with sensible timeouts.
func NewClient() *Client {
	return &Client{HTTP: &http.Client{
		Timeout: 30 * time.Second,
	}}
}

func (c *Client) baseURL() string {
	if c != nil && c.BaseURL != "" {
		return c.BaseURL
	}
	return defaultBaseURL
}

func (c *Client) userAgentString() string {
	if v := strings.TrimSpace(os.Getenv("GSBS_PCGW_USER_AGENT")); v != "" {
		return v
	}
	return defaultUserAgent
}

func (c *Client) rateLimitDuration() time.Duration {
	if v := strings.TrimSpace(os.Getenv("GSBS_PCGW_RATE_LIMIT")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return defaultRateLimit
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (c *Client) waitBetweenRequests(ctx context.Context) error {
	if c == nil {
		return nil
	}
	// Reserve this request's slot under the lock, then sleep WITHOUT it.
	// Sleeping while holding the mutex blocked every other caller in an
	// uninterruptible wait — they could not observe their own context being
	// canceled until the sleeper finished.
	c.mu.Lock()
	rl := c.rateLimitDuration()
	next := time.Now()
	if !c.lastRequest.IsZero() {
		if earliest := c.lastRequest.Add(rl); earliest.After(next) {
			next = earliest
		}
	}
	c.lastRequest = next
	c.mu.Unlock()
	if wait := time.Until(next); wait > 0 {
		return sleepCtx(ctx, wait)
	}
	return nil
}

func (c *Client) doGet(ctx context.Context, u string) (*http.Response, error) {
	if err := c.waitBetweenRequests(ctx); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.userAgentString())
	return c.getWith429Retry(ctx, req)
}

func (c *Client) getWith429Retry(ctx context.Context, req *http.Request) (*http.Response, error) {
	retries429 := 0
	retries5xx := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		resp, err := c.HTTP.Do(req)
		if err != nil {
			if retries5xx < max5xxRetries && isTransientNetworkError(err) {
				if sleepErr := sleepCtx(ctx, c.calc5xxBackoff(retries5xx)); sleepErr != nil {
					return nil, sleepErr
				}
				retries5xx++
				continue
			}
			return nil, err
		}
		switch {
		case resp.StatusCode == http.StatusTooManyRequests:
			if retries429 >= max429Retries {
				_, _ = io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				return nil, fmt.Errorf("rate limited (429) after %d retries", max429Retries)
			}
			backoff := default429Backoff
			if s := resp.Header.Get("Retry-After"); s != "" {
				if sec, err2 := strconv.Atoi(s); err2 == nil && sec > 0 {
					backoff = time.Duration(sec) * time.Second
					// Cap it: an honored Retry-After of hours parked the whole
					// sync for the full 24 h job deadline with nothing logged.
					if backoff > maxRetryAfter {
						log.Printf("pcgw: upstream Retry-After of %s exceeds the %s cap; using the cap", backoff, maxRetryAfter)
						backoff = maxRetryAfter
					}
				}
			}
			log.Printf("pcgw: rate limited (429), backing off %s (attempt %d/%d)", backoff, retries429+1, max429Retries)
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			retries429++
			if sleepErr := sleepCtx(ctx, backoff); sleepErr != nil {
				return nil, sleepErr
			}
		case resp.StatusCode >= 500:
			if retries5xx >= max5xxRetries {
				return resp, nil
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if sleepErr := sleepCtx(ctx, c.calc5xxBackoff(retries5xx)); sleepErr != nil {
				return nil, sleepErr
			}
			retries5xx++
		default:
			return resp, nil
		}
	}
}

// calc5xxBackoff returns the sleep duration for the given 5xx attempt index using
// exponential backoff (1s → 2s → 4s …, capped at max5xxBackoffDur).
// Tests may override by setting Client.testBackoff to a short duration.
func (c *Client) calc5xxBackoff(attempt int) time.Duration {
	if c != nil && c.testBackoff > 0 {
		return c.testBackoff
	}
	d := init5xxBackoff << uint(attempt)
	if d > max5xxBackoffDur {
		d = max5xxBackoffDur
	}
	return d
}

func isTransientNetworkError(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout()
	}
	return false
}

// cargoQuote renders a value as a Cargo where-clause string literal with the
// quoting characters escaped.
//
// The where clauses were built by plain string interpolation, so a quote in
// operator input — reachable through pcgw-fetch -steam-appid — changed the
// remote query's shape. Corruption rather than exposure, but an injection point
// in a public method all the same.
func cargoQuote(v string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(v) + `"`
}

// isNumericID reports whether s is a non-empty run of ASCII digits.
func isNumericID(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// GetPageIDBySteamAppID returns the PCGW page ID for a Steam App ID.
func (c *Client) GetPageIDBySteamAppID(ctx context.Context, steamAppID string) (string, error) {
	// Steam App IDs are always numeric; rejecting anything else is stricter
	// than escaping and keeps malformed operator input from reaching the wiki.
	if !isNumericID(steamAppID) {
		return "", fmt.Errorf("invalid Steam AppID %q: expected digits", steamAppID)
	}
	rows, err := c.runCargo(ctx, cargoRequest{
		Tables: tableGame,
		Fields: "Game._pageID=PageID",
		Where:  "Game.Steam_AppID HOLDS " + cargoQuote(steamAppID),
		Limit:  1,
	})
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "", fmt.Errorf("no page for Steam AppID %s", steamAppID)
	}
	// Special:CargoExport returns _pageID as a JSON number, cargoquery as a
	// string, so this cannot assert a concrete type.
	if id := parseCargoSingleValue(rows[0]["PageID"]); id != "" {
		return id, nil
	}
	return "", fmt.Errorf("invalid PageID for Steam AppID %s", steamAppID)
}

// RedirectBySteamAppID returns the wiki page URL for a Steam App ID (uses PCGW redirect API).
func (c *Client) RedirectBySteamAppID(ctx context.Context, steamAppID string) (string, error) {
	u := c.baseURL() + "/api/appid.php?appid=" + url.QueryEscape(steamAppID)
	resp, err := c.doGet(ctx, u)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("redirect API returned %d", resp.StatusCode)
	}
	// doGet FOLLOWS redirects, so by the time we hold the response the Location
	// header is long gone — reading it here could never succeed. The final URL
	// of the redirect chain is the page we actually want.
	if resp.Request != nil && resp.Request.URL != nil {
		return resp.Request.URL.String(), nil
	}
	return "", fmt.Errorf("no redirect for appid %s", steamAppID)
}

// PageInfo is a single page from ListGamePages.
type PageInfo struct {
	PageID      int64
	Title       string
	SteamAppIDs []string
	GOGID       string
	EpicID      string
	UbisoftID   string
	CoverURL    string
	CoverImage  string
	HLTBID      string
	IGDBID      string
	Developers  []string
	Publishers  []string
	AvailableOn []string
	Engines     []string
}

// ListAllPages returns a chunk of wiki pages. Use apcontinue to paginate.
func (c *Client) ListAllPages(ctx context.Context, apcontinue string, aplimit int) ([]PageInfo, string, error) {
	if aplimit <= 0 || aplimit > 500 {
		aplimit = 500
	}
	u := c.baseURL() + "/w/api.php?action=query&list=allpages&format=json&aplimit=" + strconv.Itoa(aplimit)
	if apcontinue != "" {
		u += "&apcontinue=" + url.QueryEscape(apcontinue)
	}
	resp, err := c.doGet(ctx, u)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, "", fmt.Errorf("list pages: HTTP %d: %s", resp.StatusCode, string(body))
	}
	var out struct {
		Query struct {
			AllPages []struct {
				PageID int64  `json:"pageid"`
				Title  string `json:"title"`
			} `json:"allpages"`
		} `json:"query"`
		Continue struct {
			APContinue string `json:"apcontinue"`
		} `json:"continue"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, "", err
	}
	var pages []PageInfo
	for _, p := range out.Query.AllPages {
		pages = append(pages, PageInfo{PageID: p.PageID, Title: p.Title})
	}
	return pages, out.Continue.APContinue, nil
}

// ParsePageWikitext returns the raw wikitext and page title.
func (c *Client) ParsePageWikitext(ctx context.Context, pageID string) (wikitext string, title string, err error) {
	u := c.baseURL() + "/w/api.php?action=parse&format=json&pageid=" + url.QueryEscape(pageID) + "&prop=wikitext"
	resp, err := c.doGet(ctx, u)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", "", fmt.Errorf("parse wikitext: HTTP %d: %s", resp.StatusCode, string(body))
	}
	var out struct {
		Parse struct {
			Title    string `json:"title"`
			Wikitext struct {
				Content string `json:"*"`
			} `json:"wikitext"`
		} `json:"parse"`
		Error *struct {
			Code string `json:"code"`
			Info string `json:"info"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", "", err
	}
	if out.Error != nil {
		info := out.Error.Info
		if info == "" {
			info = out.Error.Code
		}
		return "", "", fmt.Errorf("mediawiki API error: %s", info)
	}
	return out.Parse.Wikitext.Content, out.Parse.Title, nil
}

// SaveLocationTemplate represents a single save/config path template per system.
type SaveLocationTemplate struct {
	GameID   string
	System   string
	Paths    []string
	IsConfig bool
}

func decodeCargoResponse(body io.Reader) ([]map[string]interface{}, error) {
	var out struct {
		CargoQuery []struct {
			Title map[string]interface{} `json:"title"`
		} `json:"cargoquery"`
		Error *struct {
			Code    string `json:"code"`
			Info    string `json:"info"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(body).Decode(&out); err != nil {
		return nil, err
	}
	if out.Error != nil {
		msg := out.Error.Info
		if msg == "" {
			msg = out.Error.Message
		}
		if msg == "" {
			msg = out.Error.Code
		}
		return nil, fmt.Errorf("cargo query: %s", msg)
	}
	var rows []map[string]interface{}
	for _, r := range out.CargoQuery {
		rows = append(rows, r.Title)
	}
	return rows, nil
}
