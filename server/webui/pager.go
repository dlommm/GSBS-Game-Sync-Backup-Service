package webui

import (
	"net/http"
	"net/url"
	"strconv"
)

// pagerPerPageOptions are the page sizes offered by the shared table pager.
var pagerPerPageOptions = []int{10, 25, 50, 100}

// pagerView drives partials/table_pager.html — the shared prev/next + page-size
// control under paginated admin tables. URLs are precomputed here so the
// template stays free of urlquery chains.
type pagerView struct {
	Page       int
	PerPage    int
	Total      int
	TotalPages int
	Start      int // 1-based index of the first row shown
	End        int // 1-based index of the last row shown
	PrevURL    string
	NextURL    string
	// BaseURL is the endpoint plus all filter params except page/per, ending
	// in "?" or "&…" — app.js appends per=N&page=1 for the page-size select.
	BaseURL string
	// Target is the hx-target selector whose innerHTML the endpoint replaces.
	Target  string
	Label   string // plural noun for the summary, e.g. "entries", "runs"
	Options []int
}

// newPager builds a pagerView for a partial endpoint. params must hold the
// endpoint's filter parameters only (no page/per — they are added here).
// page is 1-based and clamped into range.
func newPager(path string, params url.Values, page, perPage, total int, target, label string) pagerView {
	if perPage <= 0 {
		perPage = 25
	}
	totalPages := (total + perPage - 1) / perPage
	if totalPages < 1 {
		totalPages = 1
	}
	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}
	base := path + "?"
	if enc := params.Encode(); enc != "" {
		base += enc + "&"
	}
	pageURL := func(p int) string {
		return base + "per=" + strconv.Itoa(perPage) + "&page=" + strconv.Itoa(p)
	}
	v := pagerView{
		Page: page, PerPage: perPage, Total: total, TotalPages: totalPages,
		BaseURL: base, Target: target, Label: label, Options: pagerPerPageOptions,
	}
	if total > 0 {
		v.Start = (page-1)*perPage + 1
		v.End = page * perPage
		if v.End > total {
			v.End = total
		}
	}
	if page > 1 {
		v.PrevURL = pageURL(page - 1)
	}
	if page < totalPages {
		v.NextURL = pageURL(page + 1)
	}
	return v
}

// pagerOffset converts the pager's 1-based page into a row offset.
func (p pagerView) Offset() int {
	return (p.Page - 1) * p.PerPage
}

// parseNonNegativeInt parses s as an int >= 0, returning def when absent or invalid.
func parseNonNegativeInt(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return def
	}
	return n
}

// parsePageParams reads page/per query params for a paginated table.
// page is 1-based; per is clamped to the shared page-size options' range.
func parsePageParams(r *http.Request, defaultPer int) (page, per int) {
	page = parseNonNegativeInt(r.URL.Query().Get("page"), 1)
	if page < 1 {
		page = 1
	}
	per = parseNonNegativeInt(r.URL.Query().Get("per"), defaultPer)
	if per < 1 {
		per = defaultPer
	}
	if max := pagerPerPageOptions[len(pagerPerPageOptions)-1]; per > max {
		per = max
	}
	return page, per
}
