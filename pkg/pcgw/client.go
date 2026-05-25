package pcgw

import (
	"encoding/json"
	"fmt"
	"io"
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
	defaultBaseURL    = "https://www.pcgamingwiki.com"
	defaultUserAgent  = "GSBS/1.0 (https://github.com/gsbs/gsbs; game-save-sync)"
	defaultRateLimit  = 2 * time.Second
)

// Client talks to PCGamingWiki MediaWiki API.
type Client struct {
	HTTP *http.Client
	// BaseURL overrides the API origin (for tests). Empty uses defaultBaseURL.
	BaseURL string

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

func (c *Client) waitBetweenRequests() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	rl := c.rateLimitDuration()
	if !c.lastRequest.IsZero() {
		if wait := rl - time.Since(c.lastRequest); wait > 0 {
			time.Sleep(wait)
		}
	}
	c.lastRequest = time.Now()
}

func (c *Client) doGet(u string) (*http.Response, error) {
	c.waitBetweenRequests()
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.userAgentString())
	return c.getWith429Retry(req)
}

func (c *Client) getWith429Retry(req *http.Request) (*http.Response, error) {
	var lastResp *http.Response
	for attempt := 0; attempt <= max429Retries; attempt++ {
		reqCopy := req.Clone(req.Context())
		resp, err := c.HTTP.Do(reqCopy)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusTooManyRequests {
			return resp, nil
		}
		backoff := default429Backoff
		if s := resp.Header.Get("Retry-After"); s != "" {
			if sec, err := strconv.Atoi(s); err == nil && sec > 0 {
				backoff = time.Duration(sec) * time.Second
			}
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		lastResp = resp
		if attempt < max429Retries {
			time.Sleep(backoff)
		}
	}
	if lastResp != nil {
		lastResp.Body.Close()
		return nil, fmt.Errorf("rate limited (429) after %d retries", max429Retries)
	}
	return nil, fmt.Errorf("rate limited (429)")
}

// GetPageIDBySteamAppID returns the PCGW page ID for a Steam App ID.
func (c *Client) GetPageIDBySteamAppID(steamAppID string) (string, error) {
	rows, err := c.CargoQuery(
		"Infobox_game",
		"Infobox_game._pageID=PageID",
		"Infobox_game.Steam_AppID HOLDS \""+steamAppID+"\"",
		0, 0,
	)
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "", fmt.Errorf("no page for Steam AppID %s", steamAppID)
	}
	if id, ok := rows[0]["PageID"].(string); ok {
		return id, nil
	}
	return "", fmt.Errorf("invalid PageID for Steam AppID %s", steamAppID)
}

// RedirectBySteamAppID returns the wiki page URL for a Steam App ID (uses PCGW redirect API).
func (c *Client) RedirectBySteamAppID(steamAppID string) (string, error) {
	u := c.baseURL() + "/api/appid.php?appid=" + url.QueryEscape(steamAppID)
	resp, err := c.doGet(u)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("redirect API returned %d", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "" {
		return loc, nil
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
func (c *Client) ListAllPages(apcontinue string, aplimit int) ([]PageInfo, string, error) {
	if aplimit <= 0 || aplimit > 500 {
		aplimit = 500
	}
	u := c.baseURL() + "/w/api.php?action=query&list=allpages&format=json&aplimit=" + strconv.Itoa(aplimit)
	if apcontinue != "" {
		u += "&apcontinue=" + url.QueryEscape(apcontinue)
	}
	resp, err := c.doGet(u)
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

// ParsePageWikitext returns the raw wikitext of a page.
func (c *Client) ParsePageWikitext(pageID string) (string, error) {
	u := c.baseURL() + "/w/api.php?action=parse&format=json&pageid=" + url.QueryEscape(pageID) + "&prop=wikitext"
	resp, err := c.doGet(u)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("parse wikitext: HTTP %d: %s", resp.StatusCode, string(body))
	}
	var out struct {
		Parse struct {
			Wikitext struct {
				Content string `json:"*"`
			} `json:"wikitext"`
		} `json:"parse"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.Parse.Wikitext.Content, nil
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
