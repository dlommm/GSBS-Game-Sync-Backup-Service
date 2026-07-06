package api

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"sync"

	"github.com/gsbs/gsbs/server/netutil"
)

//go:embed openapi.json
var openapiSpec []byte

var (
	openapiOnce     sync.Once
	openapiRendered []byte
)

// renderedOpenAPI returns the embedded spec with info.version replaced by the
// running server's version, so the served document never lags releases (the
// embedded file's version string is only a fallback).
func (h *Handler) renderedOpenAPI() []byte {
	openapiOnce.Do(func() {
		openapiRendered = openapiSpec
		var doc map[string]interface{}
		if err := json.Unmarshal(openapiSpec, &doc); err != nil {
			return
		}
		info, ok := doc["info"].(map[string]interface{})
		if !ok {
			return
		}
		info["version"] = h.version
		if out, err := json.MarshalIndent(doc, "", "  "); err == nil {
			openapiRendered = out
		}
	})
	return openapiRendered
}

// handleOpenAPI serves the hand-maintained OpenAPI 3.1 specification. Public
// (no auth) so tooling can discover the API. openapi_test.go asserts the spec
// stays in sync with Routes().
func (h *Handler) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	if h.rateLimited(w, r, h.generalLimiter, netutil.ClientIP(r), "general") {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write(h.renderedOpenAPI())
}
