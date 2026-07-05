package pcgw

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// recentChangesMaxEntries bounds a single catch-up window. A week of PCGW
// activity is a few thousand entries; hitting this cap means the window is
// suspect (or the wiki is misbehaving) and the caller should fall back to the
// batched revision sweep, which is correct for any gap size.
const recentChangesMaxEntries = 100000

// RecentChange is one entry from the MediaWiki recentchanges feed.
type RecentChange struct {
	Type      string // "edit", "new", "log"
	PageID    int64
	Title     string
	RevID     int64
	Timestamp string
	LogType   string // for Type=="log": "delete", "move", ...
	LogAction string
}

// RecentChangesSince returns all main-namespace edits, page creations, and log
// events (deletions, moves) since the given time, oldest first. This is the
// cheap change-detection path: one paginated query stream (500 entries per
// request) replaces a per-page revision sweep of the whole catalog.
//
// The result can only be as old as the wiki's recent-changes retention
// ($wgRCMaxAge, typically 30–90 days); callers must not use this for windows
// older than that — use the batched revision sweep instead.
func (c *Client) RecentChangesSince(ctx context.Context, since time.Time) ([]RecentChange, error) {
	var out []RecentChange
	cont := ""
	for {
		u := c.baseURL() + "/w/api.php?action=query&format=json&list=recentchanges" +
			"&rcdir=newer&rcnamespace=0&rclimit=500" +
			"&rctype=" + url.QueryEscape("edit|new|log") +
			"&rcprop=" + url.QueryEscape("title|ids|timestamp|loginfo") +
			"&rcstart=" + url.QueryEscape(since.UTC().Format(time.RFC3339))
		if cont != "" {
			u += "&rccontinue=" + url.QueryEscape(cont) + "&continue=" + url.QueryEscape("-||")
		}
		resp, err := c.doGet(ctx, u)
		if err != nil {
			return nil, err
		}
		var payload struct {
			Continue struct {
				RCContinue string `json:"rccontinue"`
			} `json:"continue"`
			Query struct {
				RecentChanges []struct {
					Type      string `json:"type"`
					PageID    int64  `json:"pageid"`
					Title     string `json:"title"`
					RevID     int64  `json:"revid"`
					Timestamp string `json:"timestamp"`
					LogType   string `json:"logtype"`
					LogAction string `json:"logaction"`
				} `json:"recentchanges"`
			} `json:"query"`
			Error *struct {
				Code string `json:"code"`
				Info string `json:"info"`
			} `json:"error"`
		}
		decodeErr := func() error {
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
				return fmt.Errorf("recentchanges: HTTP %d: %s", resp.StatusCode, string(body))
			}
			return json.NewDecoder(resp.Body).Decode(&payload)
		}()
		if decodeErr != nil {
			return nil, decodeErr
		}
		if payload.Error != nil {
			info := payload.Error.Info
			if info == "" {
				info = payload.Error.Code
			}
			return nil, fmt.Errorf("recentchanges: mediawiki API error: %s", info)
		}
		for _, rc := range payload.Query.RecentChanges {
			out = append(out, RecentChange{
				Type:      rc.Type,
				PageID:    rc.PageID,
				Title:     strings.TrimSpace(rc.Title),
				RevID:     rc.RevID,
				Timestamp: rc.Timestamp,
				LogType:   rc.LogType,
				LogAction: rc.LogAction,
			})
		}
		if len(out) > recentChangesMaxEntries {
			return nil, fmt.Errorf("recentchanges: window too large (>%d entries) — use the revision sweep", recentChangesMaxEntries)
		}
		if payload.Continue.RCContinue == "" {
			return out, nil
		}
		cont = payload.Continue.RCContinue
	}
}

// revisionBatchSize is the MediaWiki limit on pageids per prop=revisions query.
const revisionBatchSize = 50

// GetPageRevisionsBatch returns the latest revision for each of the given
// pages in batches of 50 per request (the MediaWiki maximum), ~50x cheaper
// than per-page GetPageRevision calls. Pages missing on the wiki are simply
// absent from the result map.
func (c *Client) GetPageRevisionsBatch(ctx context.Context, pageIDs []int64) (map[int64]PageRevision, error) {
	result := make(map[int64]PageRevision, len(pageIDs))
	for start := 0; start < len(pageIDs); start += revisionBatchSize {
		end := start + revisionBatchSize
		if end > len(pageIDs) {
			end = len(pageIDs)
		}
		ids := make([]string, 0, end-start)
		for _, id := range pageIDs[start:end] {
			ids = append(ids, fmt.Sprintf("%d", id))
		}
		u := c.baseURL() + "/w/api.php?action=query&format=json" +
			"&pageids=" + url.QueryEscape(strings.Join(ids, "|")) +
			"&prop=revisions&rvprop=" + url.QueryEscape("ids|timestamp")
		resp, err := c.doGet(ctx, u)
		if err != nil {
			return result, err
		}
		var payload struct {
			Query struct {
				Pages map[string]struct {
					PageID    int64  `json:"pageid"`
					Missing   string `json:"missing"`
					Revisions []struct {
						RevID     int64  `json:"revid"`
						Timestamp string `json:"timestamp"`
					} `json:"revisions"`
				} `json:"pages"`
			} `json:"query"`
			Error *struct {
				Code string `json:"code"`
				Info string `json:"info"`
			} `json:"error"`
		}
		decodeErr := func() error {
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
				return fmt.Errorf("revisions batch: HTTP %d: %s", resp.StatusCode, string(body))
			}
			return json.NewDecoder(resp.Body).Decode(&payload)
		}()
		if decodeErr != nil {
			return result, decodeErr
		}
		if payload.Error != nil {
			info := payload.Error.Info
			if info == "" {
				info = payload.Error.Code
			}
			return result, fmt.Errorf("revisions batch: mediawiki API error: %s", info)
		}
		for _, page := range payload.Query.Pages {
			if page.Missing != "" || page.PageID == 0 || len(page.Revisions) == 0 {
				continue
			}
			result[page.PageID] = PageRevision{
				RevID:     page.Revisions[0].RevID,
				Timestamp: page.Revisions[0].Timestamp,
			}
		}
	}
	return result, nil
}
