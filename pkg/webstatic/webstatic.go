// Package webstatic serves an embedded static asset tree with the HTTP
// caching that http.FileServer cannot provide over an embed.FS: embedded
// files have a zero ModTime, so the stock file server emits no validators
// at all and every navigation re-downloads every asset uncompressed.
//
// The handler snapshots the tree once at construction: each file gets a
// strong content-hash ETag, a Cache-Control lifetime, and — for text
// assets — a precompressed gzip variant negotiated via Accept-Encoding.
package webstatic

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"
)

type asset struct {
	body        []byte
	gzipBody    []byte // nil when not worth compressing
	etag        string
	contentType string
}

// Handler serves every regular file in files at its path relative to the FS
// root. maxAge bounds browser reuse; the ETag makes revalidation after
// expiry a 304 instead of a re-download.
func Handler(files fs.FS, maxAge time.Duration) http.Handler {
	assets := map[string]*asset{}
	_ = fs.WalkDir(files, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		body, err := fs.ReadFile(files, p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(body)
		a := &asset{
			body:        body,
			etag:        `"` + hex.EncodeToString(sum[:8]) + `"`,
			contentType: mime.TypeByExtension(path.Ext(p)),
		}
		if a.contentType == "" {
			a.contentType = http.DetectContentType(body)
		}
		if compressible(path.Ext(p)) {
			var buf bytes.Buffer
			gz, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
			if _, err := gz.Write(body); err == nil && gz.Close() == nil && buf.Len() < len(body) {
				a.gzipBody = buf.Bytes()
			}
		}
		assets[p] = a
		return nil
	})

	cacheControl := "public, max-age=" + strconv.Itoa(int(maxAge/time.Second))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		a, ok := assets[strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		h := w.Header()
		h.Set("ETag", a.etag)
		h.Set("Cache-Control", cacheControl)
		if a.gzipBody != nil {
			h.Set("Vary", "Accept-Encoding")
		}
		if etagMatches(r.Header.Get("If-None-Match"), a.etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		h.Set("Content-Type", a.contentType)
		body := a.body
		if a.gzipBody != nil && acceptsGzip(r) {
			h.Set("Content-Encoding", "gzip")
			body = a.gzipBody
		}
		h.Set("Content-Length", strconv.Itoa(len(body)))
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write(body)
	})
}

// compressible reports whether an extension is text-like enough for gzip to
// pay off; fonts and images are already compressed containers.
func compressible(ext string) bool {
	switch strings.ToLower(ext) {
	case ".css", ".js", ".mjs", ".svg", ".json", ".map", ".txt", ".html":
		return true
	}
	return false
}

func acceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		enc := part
		if i := strings.IndexByte(enc, ';'); i >= 0 {
			enc = enc[:i]
		}
		if strings.TrimSpace(enc) == "gzip" {
			return true
		}
	}
	return false
}

func etagMatches(ifNoneMatch, etag string) bool {
	for _, cand := range strings.Split(ifNoneMatch, ",") {
		cand = strings.TrimSpace(cand)
		cand = strings.TrimPrefix(cand, "W/")
		if cand == etag || cand == "*" {
			return true
		}
	}
	return false
}
