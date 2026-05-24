package pcgw

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const max429Retries = 5
const default429Backoff = 60 * time.Second

const baseURL = "https://www.pcgamingwiki.com"

// Client talks to PCGamingWiki MediaWiki API.
type Client struct {
	HTTP *http.Client
}

// NewClient returns a new PCGW API client with sensible timeouts.
func NewClient() *Client {
	return &Client{HTTP: &http.Client{
		Timeout: 30 * time.Second,
	}}
}

// getWith429Retry performs GET and on HTTP 429 retries with backoff (Retry-After header or default 60s).
func (c *Client) getWith429Retry(u string) (*http.Response, error) {
	var lastResp *http.Response
	for attempt := 0; attempt <= max429Retries; attempt++ {
		resp, err := c.HTTP.Get(u)
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

// CargoQuery runs a cargoquery action. See https://www.pcgamingwiki.com/wiki/PCGamingWiki:API
// limit and offset 0 mean default (50). Use limit 500 max per request.
func (c *Client) CargoQuery(tables, fields, where string, limit, offset int) ([]map[string]interface{}, error) {
	u := baseURL + "/w/api.php?action=cargoquery&format=json"
	u += "&tables=" + url.QueryEscape(tables)
	u += "&fields=" + url.QueryEscape(fields)
	if where != "" {
		u += "&where=" + url.QueryEscape(where)
	}
	if limit > 0 {
		if limit > 500 {
			limit = 500
		}
		u += "&limit=" + strconv.Itoa(limit)
	}
	if offset > 0 {
		u += "&offset=" + strconv.Itoa(offset)
	}
	resp, err := c.getWith429Retry(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("cargo query: HTTP %d: %s", resp.StatusCode, string(body))
	}
	var out struct {
		CargoQuery []struct {
			Title map[string]interface{} `json:"title"`
		} `json:"cargoquery"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	var rows []map[string]interface{}
	for _, r := range out.CargoQuery {
		rows = append(rows, r.Title)
	}
	return rows, nil
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
	u := baseURL + "/api/appid.php?appid=" + url.QueryEscape(steamAppID)
	resp, err := c.HTTP.Get(u)
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

// PageInfo is a single page from ListAllPages.
type PageInfo struct {
	PageID      int64
	Title       string
	SteamAppIDs []string
	GOGID       string
	EpicID      string
	UbisoftID   string
}

// ListAllPages returns a chunk of wiki pages. Use apcontinue to paginate.
// aplimit is max 500. Returns nextContinue empty when no more pages.
func (c *Client) ListAllPages(apcontinue string, aplimit int) ([]PageInfo, string, error) {
	if aplimit <= 0 || aplimit > 500 {
		aplimit = 500
	}
	u := baseURL + "/w/api.php?action=query&list=allpages&format=json&aplimit=" + strconv.Itoa(aplimit)
	if apcontinue != "" {
		u += "&apcontinue=" + url.QueryEscape(apcontinue)
	}
	resp, err := c.HTTP.Get(u)
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

// ListGamePages returns game pages from the Infobox_game Cargo table (only actual game pages).
// limit max 500. offset is for pagination (0, 500, 1000, ...).
func (c *Client) ListGamePages(limit, offset int) ([]PageInfo, error) {
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	rows, err := c.CargoQuery(
		"Infobox_game",
		"Infobox_game._pageID=PageID,Infobox_game._pageName=Title,Infobox_game.Steam_AppID=SteamAppID,Infobox_game.GOG_com_id=GOGID,Infobox_game.Epic_Games_Store=EpicID,Infobox_game.Ubisoft_Connect=UbisoftID",
		"",
		limit, offset,
	)
	if err != nil {
		return nil, err
	}
	var pages []PageInfo
	for _, r := range rows {
		pageID, _ := r["PageID"].(string)
		title, _ := r["Title"].(string)
		if pageID == "" || title == "" {
			continue
		}
		id, err := strconv.ParseInt(pageID, 10, 64)
		if err != nil {
			continue
		}
		pages = append(pages, PageInfo{
			PageID:      id,
			Title:       title,
			SteamAppIDs: parseCargoMultiValue(r["SteamAppID"]),
			GOGID:       parseCargoSingleValue(r["GOGID"]),
			EpicID:      parseCargoSingleValue(r["EpicID"]),
			UbisoftID:   parseCargoSingleValue(r["UbisoftID"]),
		})
	}
	return pages, nil
}

// parseCargoSingleValue extracts a string from a Cargo field (string or nested map).
func parseCargoSingleValue(v interface{}) string {
	switch val := v.(type) {
	case string:
		return strings.TrimSpace(val)
	case map[string]interface{}:
		if s, ok := val["fulltext"].(string); ok {
			return strings.TrimSpace(s)
		}
		if s, ok := val["value"].(string); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

// parseCargoMultiValue extracts multiple IDs (comma-separated or array) from Cargo.
func parseCargoMultiValue(v interface{}) []string {
	s := parseCargoSingleValue(v)
	if s == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// ParsePageWikitext returns the raw wikitext of a page (for parsing save locations).
// pageID is the numeric page ID from GetPageIDBySteamAppID or similar.
func (c *Client) ParsePageWikitext(pageID string) (string, error) {
	u := baseURL + "/w/api.php?action=parse&format=json&pageid=" + url.QueryEscape(pageID) + "&prop=wikitext"
	resp, err := c.getWith429Retry(u)
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
// PCGW "Save game data location" and "Configuration file(s) location" are typically
// in wikitext templates; this struct is what we cache after parsing.
type SaveLocationTemplate struct {
	GameID   string   // PCGW page name or Steam App ID
	System   string   // e.g. "Windows", "Steam Play (Linux)"
	Paths    []string // path templates (with placeholders)
	IsConfig bool     // config file vs save file
}
