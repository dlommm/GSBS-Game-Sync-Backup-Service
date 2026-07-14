package pcgw

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const infoboxGameFields = "Infobox_game._pageID=PageID,Infobox_game._pageName=Title," +
	"Infobox_game.Steam_AppID=SteamAppID,Infobox_game.GOGcom_ID=GOGID," +
	"Infobox_game.Cover=Cover,Infobox_game.Cover_URL=CoverURL," +
	"Infobox_game.Developers=Developers,Infobox_game.Publishers=Publishers," +
	"Infobox_game.Available_on=AvailableOn,Infobox_game.Engines=Engines," +
	"Infobox_game.Series=Series,Infobox_game.Wikipedia=Wikipedia," +
	"Infobox_game.StrategyWiki=StrategyWiki"

const listGamePagesFields = infoboxGameFields

// FetchInfoboxGame returns Infobox_game Cargo rows for a page ID.
func (c *Client) FetchInfoboxGame(ctx context.Context, pageID string) (map[string]interface{}, error) {
	where := fmt.Sprintf("Infobox_game._pageID=\"%s\"", pageID)
	rows, err := c.CargoQuery(ctx, "Infobox_game", infoboxGameFields, where, 1, 0)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("no Infobox_game row for page %s", pageID)
	}
	return rows[0], nil
}

// FetchAvailability returns Availability Cargo rows for a page ID when present.
func (c *Client) FetchAvailability(ctx context.Context, pageID string) (map[string]interface{}, error) {
	where := fmt.Sprintf("Availability._pageID=\"%s\"", pageID)
	fields := "Availability._pageID=PageID,Availability.Steam_AppID=SteamAppID," +
		"Availability.GOGcom_ID=GOGID,Availability.Epic_Games_Store_ID=EpicID," +
		"Availability.Microsoft_Store_ID=MicrosoftID,Availability.Ubisoft_Connect_ID=UbisoftID"
	rows, err := c.CargoQuery(ctx, "Availability", fields, where, 1, 0)
	if err != nil {
		rows, err = c.CargoQuery(ctx, "Availability", "Availability._pageID=PageID", where, 1, 0)
		if err != nil {
			return nil, err
		}
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("no Availability row for page %s", pageID)
	}
	return rows[0], nil
}

// ListGamePages returns game pages from the Infobox_game Cargo table (only actual game pages).
func (c *Client) ListGamePages(ctx context.Context, limit, offset int) ([]PageInfo, error) {
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	rows, err := c.CargoQuery(ctx, "Infobox_game", listGamePagesFields, "", limit, offset)
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
			CoverURL:    parseCargoSingleValue(r["CoverURL"]),
			CoverImage:  parseCargoSingleValue(r["Cover"]),
			Developers:  parseCargoMultiValue(r["Developers"]),
			Publishers:  parseCargoMultiValue(r["Publishers"]),
			AvailableOn: parseCargoMultiValue(r["AvailableOn"]),
			Engines:     parseCargoMultiValue(r["Engines"]),
			HLTBID:      parseInfoboxID(r, "HLTB", "HowLongToBeat"),
			IGDBID:      parseInfoboxID(r, "IGDB"),
		})
	}
	return pages, nil
}

func parseInfoboxID(row map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if v := parseCargoSingleValue(row[k]); v != "" {
			return v
		}
	}
	return ""
}

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

// parseCargoMultiValue splits a Cargo multi-value field. Cargo's fulltext
// format joins values with a plain comma and offers no per-query delimiter or
// quoting, so a value that itself contains ", " (e.g. "Bandai Namco, Inc.")
// is genuinely ambiguous at this layer and will split — accepted data-quality
// limitation, not fixable client-side.
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

// CargoQuery runs a cargoquery action.
func (c *Client) CargoQuery(ctx context.Context, tables, fields, where string, limit, offset int) ([]map[string]interface{}, error) {
	u := c.baseURL() + "/w/api.php?action=cargoquery&format=json"
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
	resp, err := c.doGet(ctx, u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("cargo query: HTTP %d: %s", resp.StatusCode, string(body))
	}
	return decodeCargoResponse(resp.Body)
}
