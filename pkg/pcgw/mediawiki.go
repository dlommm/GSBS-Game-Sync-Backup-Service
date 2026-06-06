package pcgw

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

// PageRevision holds the latest revision metadata for a wiki page.
type PageRevision struct {
	RevID     int64
	Timestamp string
}

// GetPageRevision returns the latest revision ID and timestamp for a page.
func (c *Client) GetPageRevision(pageID string) (*PageRevision, error) {
	u := c.baseURL() + "/w/api.php?action=query&format=json&pageids=" + url.QueryEscape(pageID) +
		"&prop=revisions&rvlimit=1&rvprop=" + url.QueryEscape("ids|timestamp")
	resp, err := c.doGet(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("get revision: HTTP %d: %s", resp.StatusCode, string(body))
	}
	var out struct {
		Query struct {
			Pages map[string]struct {
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
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if out.Error != nil {
		info := out.Error.Info
		if info == "" {
			info = out.Error.Code
		}
		return nil, fmt.Errorf("mediawiki API error: %s", info)
	}
	for _, page := range out.Query.Pages {
		if page.Missing != "" || len(page.Revisions) == 0 {
			return nil, fmt.Errorf("page %s not found", pageID)
		}
		return &PageRevision{
			RevID:     page.Revisions[0].RevID,
			Timestamp: page.Revisions[0].Timestamp,
		}, nil
	}
	return nil, fmt.Errorf("page %s not found", pageID)
}

// ResolveRedirect returns the target page ID and title when pageID is a redirect.
// If not a redirect, returns the same page ID and its title.
func (c *Client) ResolveRedirect(pageID string) (targetPageID string, targetTitle string, err error) {
	u := c.baseURL() + "/w/api.php?action=query&format=json&pageids=" + url.QueryEscape(pageID) +
		"&redirects=1&prop=info"
	resp, err := c.doGet(u)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", "", fmt.Errorf("resolve redirect: HTTP %d: %s", resp.StatusCode, string(body))
	}
	var out struct {
		Query struct {
			Redirects []struct {
				From string `json:"from"`
				To   string `json:"to"`
			} `json:"redirects"`
			Pages map[string]struct {
				PageID  int64  `json:"pageid"`
				Title   string `json:"title"`
				Missing string `json:"missing"`
			} `json:"pages"`
		} `json:"query"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", "", err
	}
	var resolvedID int64
	var resolvedTitle string
	for idStr, page := range out.Query.Pages {
		if page.Missing != "" {
			return "", "", fmt.Errorf("page %s not found", pageID)
		}
		resolvedID = page.PageID
		resolvedTitle = page.Title
		if resolvedID == 0 {
			if parsed, err := strconv.ParseInt(idStr, 10, 64); err == nil {
				resolvedID = parsed
			}
		}
		break
	}
	// With redirects=1, pages map already contains the final target after redirects.
	return strconv.FormatInt(resolvedID, 10), resolvedTitle, nil
}
