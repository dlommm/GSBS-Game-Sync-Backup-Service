package webstatic

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func testFS() fstest.MapFS {
	return fstest.MapFS{
		"app.css":          {Data: []byte(strings.Repeat(".x{color:red}", 200))},
		"img/logo.png":     {Data: []byte("\x89PNG\r\n\x1a\nnot-really-png")},
		"ext/bridge.js":    {Data: []byte(strings.Repeat("function f(){}", 100))},
		"fonts/sans.woff2": {Data: []byte("wOF2-binary")},
	}
}

func TestServesWithValidatorsAndCacheControl(t *testing.T) {
	h := Handler(testFS(), time.Hour)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/app.css", nil))
	res := rec.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}
	if res.Header.Get("ETag") == "" {
		t.Error("missing ETag")
	}
	if got := res.Header.Get("Cache-Control"); got != "public, max-age=3600" {
		t.Errorf("Cache-Control = %q", got)
	}
	if ct := res.Header.Get("Content-Type"); !strings.Contains(ct, "text/css") {
		t.Errorf("Content-Type = %q", ct)
	}
}

func TestETagRevalidation304(t *testing.T) {
	h := Handler(testFS(), time.Hour)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/app.css", nil))
	etag := rec.Result().Header.Get("ETag")

	req := httptest.NewRequest(http.MethodGet, "/app.css", nil)
	req.Header.Set("If-None-Match", etag)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Result().StatusCode != http.StatusNotModified {
		t.Fatalf("status = %d, want 304", rec.Result().StatusCode)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("304 carried a body (%d bytes)", rec.Body.Len())
	}
}

func TestGzipNegotiation(t *testing.T) {
	h := Handler(testFS(), time.Hour)

	req := httptest.NewRequest(http.MethodGet, "/app.css", nil)
	req.Header.Set("Accept-Encoding", "gzip, br")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	res := rec.Result()
	if res.Header.Get("Content-Encoding") != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", res.Header.Get("Content-Encoding"))
	}
	gr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	plain, _ := io.ReadAll(gr)
	if string(plain) != strings.Repeat(".x{color:red}", 200) {
		t.Error("gzip round-trip mismatch")
	}

	// Identity when the client does not accept gzip.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/app.css", nil))
	if enc := rec.Result().Header.Get("Content-Encoding"); enc != "" {
		t.Errorf("Content-Encoding = %q, want identity", enc)
	}

	// Binary assets are never compressed.
	req = httptest.NewRequest(http.MethodGet, "/img/logo.png", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if enc := rec.Result().Header.Get("Content-Encoding"); enc != "" {
		t.Errorf("png Content-Encoding = %q, want identity", enc)
	}
}

func TestNotFoundAndMethods(t *testing.T) {
	h := Handler(testFS(), time.Hour)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/missing.css", nil))
	if rec.Result().StatusCode != http.StatusNotFound {
		t.Errorf("missing: status = %d", rec.Result().StatusCode)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/../embed.go", nil))
	if rec.Result().StatusCode != http.StatusNotFound {
		t.Errorf("traversal: status = %d", rec.Result().StatusCode)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/app.css", nil))
	if rec.Result().StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST: status = %d", rec.Result().StatusCode)
	}

	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodHead, "/app.css", nil)
	h.ServeHTTP(rec, req)
	if rec.Result().StatusCode != http.StatusOK || rec.Body.Len() != 0 {
		t.Errorf("HEAD: status = %d body = %d", rec.Result().StatusCode, rec.Body.Len())
	}
}
