package pcgw

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// PCGW renamed its core Cargo tables on 2026-08-25: Infobox_game became Game
// and VR_support became VR, while PCGW's own internal tables moved behind a
// PCGW_ prefix. Querying the old names now fails with "Table ... not found".
//
// Note that the {{Infobox game}} *wikitext template* kept its name — only the
// Cargo table was renamed. parse_infobox.go still matches the template and
// must not be renamed along with these constants.
const (
	tableGame         = "Game"
	tableAvailability = "Availability"
	tableStoreKeys    = "StoreKeys"
)

// gameFields is the Game projection backing the catalog. Every column here was
// carried over from Infobox_game unchanged; only the table name moved.
const gameFields = "Game._pageID=PageID,Game._pageName=Title," +
	"Game.Steam_AppID=SteamAppID,Game.GOGcom_ID=GOGID," +
	"Game.Cover=Cover,Game.Cover_URL=CoverURL," +
	"Game.Developers=Developers,Game.Publishers=Publishers," +
	"Game.Available_on=AvailableOn,Game.Engines=Engines," +
	"Game.Series=Series,Game.Wikipedia=Wikipedia," +
	"Game.StrategyWiki=StrategyWiki"

const listGamePagesFields = gameFields

// gameOrderBy keeps offset pagination deterministic and ascending by page ID,
// so pages created after a scan begins land at the end of the sequence. The
// tail scan depends on that: it resumes from a stored offset and assumes
// everything beyond it is new.
const gameOrderBy = "Game._pageID ASC"

// FetchGame returns the Game Cargo row for a page ID.
func (c *Client) FetchGame(ctx context.Context, pageID string) (map[string]interface{}, error) {
	rows, err := c.runCargo(ctx, cargoRequest{
		Tables: tableGame,
		Fields: gameFields,
		Where:  "Game._pageID=" + cargoQuote(pageID),
		Limit:  1,
	})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("no Game row for page %s", pageID)
	}
	return rows[0], nil
}

// FetchInfoboxGame is the pre-rename name for FetchGame.
//
// Deprecated: the Infobox_game table is now Game. Use FetchGame.
func (c *Client) FetchInfoboxGame(ctx context.Context, pageID string) (map[string]interface{}, error) {
	return c.FetchGame(ctx, pageID)
}

// FetchAvailability returns the Availability Cargo row for a page ID.
//
// The table was reshaped on 2026-08-30: the per-store ID columns it used to
// carry (Steam_AppID, GOGcom_ID, Epic_Games_Store_ID, Microsoft_Store_ID,
// Ubisoft_Connect_ID) are gone. Steam and GOG IDs now live on Game, the rest
// moved to StoreKeys, and what remains here is availability state
// (Present/Past/Future) plus the per-store DRM columns.
func (c *Client) FetchAvailability(ctx context.Context, pageID string) (map[string]interface{}, error) {
	where := "Availability._pageID=" + cargoQuote(pageID)
	fields := "Availability._pageID=PageID,Availability.Present=Present," +
		"Availability.Past=Past,Availability.Future=Future," +
		"Availability.Uses_DRM=UsesDRM,Availability.Steam_DRM=SteamDRM," +
		"Availability.GOGcom_DRM=GOGDRM,Availability.Epic_Games_Store_DRM=EpicDRM," +
		"Availability.Microsoft_Store_DRM=MicrosoftDRM,Availability.Ubisoft_Store_DRM=UbisoftDRM"
	rows, err := c.runCargo(ctx, cargoRequest{
		Tables: tableAvailability,
		Fields: fields,
		Where:  where,
		Limit:  1,
	})
	if err != nil {
		// Fall back to the bare key so a column PCGW renames again degrades to
		// "row exists" rather than failing the whole lookup.
		rows, err = c.runCargo(ctx, cargoRequest{
			Tables: tableAvailability,
			Fields: "Availability._pageID=PageID",
			Where:  where,
			Limit:  1,
		})
		if err != nil {
			return nil, err
		}
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("no Availability row for page %s", pageID)
	}
	return rows[0], nil
}

// StoreKey is one storefront identifier for a page, from the StoreKeys table
// that PCGW split out of Availability on 2026-08-30.
type StoreKey struct {
	Store string
	Keys  []string
}

// FetchStoreKeys returns the storefront identifiers recorded for a page.
// Pages with no store keys yield no rows, which is not an error.
func (c *Client) FetchStoreKeys(ctx context.Context, pageID string) ([]StoreKey, error) {
	rows, err := c.runCargo(ctx, cargoRequest{
		Tables: tableStoreKeys,
		Fields: "StoreKeys._pageID=PageID,StoreKeys.Store=Store,StoreKeys.PlatformKeys=PlatformKeys",
		Where:  "StoreKeys._pageID=" + cargoQuote(pageID),
		Limit:  50,
	})
	if err != nil {
		return nil, err
	}
	var out []StoreKey
	for _, r := range rows {
		store := parseCargoSingleValue(r["Store"])
		if store == "" {
			continue
		}
		out = append(out, StoreKey{Store: store, Keys: parseCargoMultiValue(r["PlatformKeys"])})
	}
	return out, nil
}

// ListGamePages returns game pages from the Game Cargo table (only actual game
// pages, unlike the allpages enumeration).
func (c *Client) ListGamePages(ctx context.Context, limit, offset int) ([]PageInfo, error) {
	if limit <= 0 || limit > maxCargoExportLimit {
		limit = maxCargoExportLimit
	}
	rows, err := c.runCargo(ctx, cargoRequest{
		Tables:  tableGame,
		Fields:  listGamePagesFields,
		OrderBy: gameOrderBy,
		Limit:   limit,
		Offset:  offset,
	})
	if err != nil {
		return nil, err
	}
	var pages []PageInfo
	for _, r := range rows {
		pageID := parseCargoSingleValue(r["PageID"])
		title := parseCargoSingleValue(r["Title"])
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
			CoverURL:    parseCargoSingleValue(r["CoverURL"]),
			CoverImage:  parseCargoSingleValue(r["Cover"]),
			Developers:  parseCargoMultiValue(r["Developers"]),
			Publishers:  parseCargoMultiValue(r["Publishers"]),
			AvailableOn: parseCargoMultiValue(r["AvailableOn"]),
			Engines:     parseCargoMultiValue(r["Engines"]),
			// EpicID, UbisoftID, HLTBID, and IGDBID are deliberately NOT read
			// here. Game carries none of them, and listGamePagesFields never
			// requested them, so these reads always produced "" while looking
			// like they populated the launcher-matching maps. HLTB and IGDB
			// come from the infobox and Epic/Ubisoft from StoreKeys or the
			// availability rows — all extracted during wikitext ingest.
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

// parseCargoSingleValue normalises one Cargo cell to a string.
//
// It has to span two response shapes. Special:CargoExport emits real JSON
// types — numbers for page IDs, arrays for List-of columns — while
// action=cargoquery emits everything as strings, sometimes wrapped in a
// {fulltext, value} object.
func parseCargoSingleValue(v interface{}) string {
	switch val := v.(type) {
	case string:
		return strings.TrimSpace(val)
	case json.Number:
		return val.String()
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case int:
		return strconv.Itoa(val)
	case int64:
		return strconv.FormatInt(val, 10)
	case bool:
		return strconv.FormatBool(val)
	case []interface{}:
		// A single-valued read of a List-of column: take the first entry.
		for _, item := range val {
			if s := parseCargoSingleValue(item); s != "" {
				return s
			}
		}
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

// parseCargoMultiValue reads a Cargo multi-value field.
//
// Special:CargoExport returns List-of columns as JSON arrays, so each value
// arrives intact. That is the path the default backend takes, and it removes a
// long-standing data-quality bug: the cargoquery fallback below can only
// recover values by splitting on commas, which corrupts any value containing
// one (e.g. "Bandai Namco, Inc."). Cargo offers no per-query delimiter or
// quoting, so that split is unfixable on the string path — it is avoided
// rather than repaired.
func parseCargoMultiValue(v interface{}) []string {
	if list, ok := v.([]interface{}); ok {
		var out []string
		for _, item := range list {
			if s := parseCargoSingleValue(item); s != "" {
				out = append(out, s)
			}
		}
		return out
	}
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
//
// Since 2026-08-23 PCGW rejects anonymous cargoquery requests with
// "permissiondenied"; reaching it needs a MediaWiki bot login. Internal
// callers go through runCargo, which defaults to Special:CargoExport instead.
func (c *Client) CargoQuery(ctx context.Context, tables, fields, where string, limit, offset int) ([]map[string]interface{}, error) {
	return c.cargoQueryOrdered(ctx, cargoRequest{
		Tables: tables,
		Fields: fields,
		Where:  where,
		Limit:  limit,
		Offset: offset,
	})
}

func (c *Client) cargoQueryOrdered(ctx context.Context, q cargoRequest) ([]map[string]interface{}, error) {
	u := c.baseURL() + "/w/api.php?action=cargoquery&format=json"
	u += "&tables=" + url.QueryEscape(q.Tables)
	u += "&fields=" + url.QueryEscape(q.Fields)
	if q.Where != "" {
		u += "&where=" + url.QueryEscape(q.Where)
	}
	if q.OrderBy != "" {
		u += "&order_by=" + url.QueryEscape(q.OrderBy)
	}
	if q.Limit > 0 {
		limit := q.Limit
		// action=cargoquery caps at 500 regardless of what the caller asks
		// for; Special:CargoExport allows ten times that.
		if limit > 500 {
			limit = 500
		}
		u += "&limit=" + strconv.Itoa(limit)
	}
	if q.Offset > 0 {
		u += "&offset=" + strconv.Itoa(q.Offset)
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
