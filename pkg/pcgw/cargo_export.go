package pcgw

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// maxCargoExportLimit is the largest limit Special:CargoExport honours. It is
// ten times the 500-row ceiling on action=cargoquery, so a full catalog scan
// needs an order of magnitude fewer round trips.
const maxCargoExportLimit = 5000

// maxCargoExportBytes caps a single export response. A 5000-row catalog page
// is roughly 1.5 MB; the cap exists so a redirected or malfunctioning endpoint
// cannot stream unbounded data into memory.
const maxCargoExportBytes = 64 << 20

// exportBackend reads Cargo tables through Special:CargoExport, which PCGW
// still serves to anonymous callers.
//
// Beyond being reachable, its JSON is better typed than cargoquery's: page IDs
// arrive as numbers and List-of fields as real JSON arrays, so multi-valued
// columns no longer have to be recovered by splitting a comma-joined string.
type exportBackend struct{}

func (exportBackend) name() string { return CargoBackendExport }

func (exportBackend) run(ctx context.Context, c *Client, q cargoRequest) ([]map[string]interface{}, error) {
	u := c.baseURL() + "/wiki/Special:CargoExport?format=json"
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
		if limit > maxCargoExportLimit {
			limit = maxCargoExportLimit
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
		return nil, fmt.Errorf("cargo export: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return decodeCargoExportResponse(resp.Body)
}

// decodeCargoExportResponse parses a Special:CargoExport JSON array.
//
// The endpoint reports failures as a human-readable page under HTTP 200 — an
// unknown table yields the body "Error: Table Infobox_game not found." — so
// the payload has to be sniffed rather than trusted.
func decodeCargoExportResponse(body io.Reader) ([]map[string]interface{}, error) {
	br := bufio.NewReader(io.LimitReader(body, maxCargoExportBytes))

	// Skip leading whitespace to find the first meaningful byte.
	for {
		b, err := br.ReadByte()
		if err != nil {
			if err == io.EOF {
				return nil, nil
			}
			return nil, err
		}
		if b == ' ' || b == '\t' || b == '\n' || b == '\r' {
			continue
		}
		if err := br.UnreadByte(); err != nil {
			return nil, err
		}
		if b != '[' {
			rest, _ := io.ReadAll(io.LimitReader(br, 4096))
			return nil, fmt.Errorf("cargo export: %s", summarizeExportError(rest))
		}
		break
	}

	var rows []map[string]interface{}
	if err := json.NewDecoder(br).Decode(&rows); err != nil {
		return nil, fmt.Errorf("cargo export: decode: %w", err)
	}
	return rows, nil
}

var exportTagRE = regexp.MustCompile(`<[^>]*>`)

// summarizeExportError reduces an HTML or plain-text error page to one line.
func summarizeExportError(body []byte) string {
	text := exportTagRE.ReplaceAllString(string(body), " ")
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return "empty response"
	}
	if len(text) > 200 {
		text = text[:200] + "…"
	}
	return text
}
