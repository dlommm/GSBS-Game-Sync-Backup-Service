package pcgw

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGetWith5xxRetry_SucceedsAfterTransient(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable) // 503
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"parse":{"wikitext":{"*":"hello"}}}`))
	}))
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), BaseURL: srv.URL, testBackoff: time.Millisecond}
	wikitext, err := c.ParsePageWikitext("5")
	if err != nil {
		t.Fatalf("expected success after retries: %v", err)
	}
	if wikitext != "hello" {
		t.Fatalf("unexpected wikitext: %q", wikitext)
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts (2 failures + 1 success), got %d", attempts)
	}
}

func TestGetWith5xxRetry_ExhaustsRetries(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusInternalServerError) // 500 every time
	}))
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), BaseURL: srv.URL, testBackoff: time.Millisecond}
	resp, err := c.doGet(srv.URL + "/w/api.php?action=parse&format=json&pageid=5&prop=wikitext")
	if err != nil {
		t.Fatalf("expected 500 response returned (not error) after retries: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500 status, got %d", resp.StatusCode)
	}
	if attempts != max5xxRetries+1 {
		t.Fatalf("expected %d attempts, got %d", max5xxRetries+1, attempts)
	}
}

func TestParsePageWikitext_MediaWikiError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":{"code":"nosuchpageid","info":"There is no page with ID 99999."}}`))
	}))
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), BaseURL: srv.URL}
	_, err := c.ParsePageWikitext("99999")
	if err == nil {
		t.Fatal("expected error for MediaWiki error response")
	}
	if !strings.Contains(err.Error(), "nosuchpageid") && !strings.Contains(err.Error(), "There is no page") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetPageRevision_MediaWikiError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":{"code":"invalidtitle","info":"Bad title \"???\"."}}`))
	}))
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), BaseURL: srv.URL}
	_, err := c.GetPageRevision("???")
	if err == nil {
		t.Fatal("expected error for MediaWiki error response")
	}
	if !strings.Contains(err.Error(), "invalidtitle") && !strings.Contains(err.Error(), "Bad title") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCargoQueryReturnsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ua := r.Header.Get("User-Agent"); ua == "" {
			t.Error("expected User-Agent header")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":{"code":"internal_api_error","info":"No field named \"GOG_com_id\""}}`))
	}))
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), BaseURL: srv.URL}
	_, err := c.CargoQuery("Infobox_game", "Infobox_game._pageID=PageID", "", 1, 0)
	if err == nil {
		t.Fatal("expected cargo API error")
	}
	if !strings.Contains(err.Error(), "GOG_com_id") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestListGamePagesParsesRows(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"cargoquery":[{"title":{"PageID":"5","Title":"Titan Quest","SteamAppID":"4540,4550","GOGID":"123","CoverURL":"https://example/cover.jpg","Cover":"cover.jpg","Developers":"Iron Lore","AvailableOn":"Windows, Linux"}}]}`))
	}))
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), BaseURL: srv.URL}
	pages, err := c.ListGamePages(10, 0)
	if err != nil {
		t.Fatalf("ListGamePages: %v", err)
	}
	if len(pages) != 1 {
		t.Fatalf("len(pages)=%d want 1", len(pages))
	}
	if pages[0].PageID != 5 || pages[0].Title != "Titan Quest" {
		t.Fatalf("page: %+v", pages[0])
	}
	if len(pages[0].SteamAppIDs) != 2 || pages[0].SteamAppIDs[0] != "4540" {
		t.Fatalf("steam: %v", pages[0].SteamAppIDs)
	}
	if pages[0].GOGID != "123" {
		t.Fatalf("gog: %q", pages[0].GOGID)
	}
	if pages[0].CoverURL != "https://example/cover.jpg" {
		t.Fatalf("cover url: %q", pages[0].CoverURL)
	}
	if len(pages[0].AvailableOn) != 2 {
		t.Fatalf("available on: %v", pages[0].AvailableOn)
	}
}

func TestListGamePagesIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("live PCGW API")
	}
	pages, err := NewClient().ListGamePages(3, 0)
	if err != nil {
		t.Fatalf("ListGamePages: %v", err)
	}
	if len(pages) == 0 {
		t.Fatal("expected pages from live PCGW API")
	}
}

func TestCompressWikitextRoundtrip(t *testing.T) {
	in := []byte("{{Game data/saves|Windows|{{p|appdata}}\\save}}")
	compressed, err := CompressWikitext(in)
	if err != nil {
		t.Fatalf("compress: %v", err)
	}
	out, err := DecompressWikitext(compressed)
	if err != nil {
		t.Fatalf("decompress: %v", err)
	}
	if string(out) != string(in) {
		t.Fatalf("roundtrip mismatch: %q vs %q", out, in)
	}
}

func TestSplitWikiSections(t *testing.T) {
	wt := "lead text\n== Game data ==\n{{Game data/saves|Windows|path}}\n== Video ==\nsettings"
	secs := SplitWikiSections(wt)
	if _, ok := secs["lead"]; !ok {
		t.Fatal("expected lead section")
	}
	if _, ok := secs["game_data"]; !ok {
		t.Fatal("expected game_data section")
	}
	if _, ok := secs["video"]; !ok {
		t.Fatal("expected video section")
	}
}

func TestSystemToPlatform(t *testing.T) {
	if SystemToPlatform("Steam Play (Linux)") != "linux" {
		t.Fatal("expected linux")
	}
	if SystemToPlatform("macOS (OS X)") != "macos" {
		t.Fatal("expected macos")
	}
	if SystemToPlatform("GOG.com") != "windows" {
		t.Fatal("expected windows for GOG")
	}
}

func TestGetPageRevisionMock(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"query":{"pages":{"5":{"revisions":[{"revid":99,"timestamp":"2026-01-01T00:00:00Z"}]}}}}`))
	}))
	defer srv.Close()
	c := &Client{HTTP: srv.Client(), BaseURL: srv.URL}
	rev, err := c.GetPageRevision("5")
	if err != nil {
		t.Fatalf("GetPageRevision: %v", err)
	}
	if rev.RevID != 99 {
		t.Fatalf("rev: %+v", rev)
	}
}

func TestExtractAllTemplates_SkipsMalformed(t *testing.T) {
	// An unterminated {{ must not prevent extraction of valid templates that follow.
	input := `Some text {{malformed and never closed` +
		"\n" + `{{Game data/saves|Windows|%APPDATA%/Game}}`
	got := ExtractAllTemplates(input)
	if len(got) == 0 {
		t.Fatal("expected at least one template after the malformed one")
	}
	found := false
	for _, tmpl := range got {
		if strings.Contains(tmpl, "Game data/saves") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("valid template not found in %v", got)
	}
}

func TestExtractAllTemplates_MalformedMidPage(t *testing.T) {
	// Multiple templates: first valid, then malformed, then valid again.
	input := `{{Infobox game|title=Foo}} {{broken` + "\n" + `{{Game data/saves|Linux|/home/user/.config/game}}`
	got := ExtractAllTemplates(input)
	if len(got) < 2 {
		t.Fatalf("expected at least 2 templates, got %d: %v", len(got), got)
	}
}

func TestIngestPageMock(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		action := r.URL.Query().Get("action")
		prop := r.URL.Query().Get("prop")
		switch {
		case action == "parse":
			_, _ = w.Write([]byte(`{"parse":{"wikitext":{"*":"{{Infobox game|title=Test}}\n== Game data ==\n{{Game data/saves|Windows|{{p|appdata}}\\save}}"}}}`))
		case action == "query" && prop == "revisions":
			_, _ = w.Write([]byte(`{"query":{"pages":{"5":{"revisions":[{"revid":99,"timestamp":"2026-01-01T00:00:00Z"}]}}}}`))
		default:
			t.Logf("unexpected request: %s", r.URL.RawQuery)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), BaseURL: srv.URL}
	result, err := IngestPage(c, 5, PageInfo{PageID: 5, Title: "Test"})
	if err != nil {
		t.Fatalf("IngestPage: %v", err)
	}
	if result.Bundle.ParseStatus != "ok" {
		t.Fatalf("parse status: %s errors=%v failed=%v", result.Bundle.ParseStatus, result.Errors, result.FailedSections)
	}
	if len(result.Bundle.SaveLocations) == 0 {
		t.Fatal("expected save locations")
	}
	if result.Bundle.RevisionID != 99 {
		t.Fatalf("rev id: %d errors=%v", result.Bundle.RevisionID, result.Errors)
	}
}
