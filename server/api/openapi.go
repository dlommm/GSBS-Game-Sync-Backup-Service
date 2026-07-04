package api

import (
	_ "embed"
	"net/http"

	"github.com/gsbs/gsbs/server/netutil"
)

//go:embed openapi.json
var openapiSpec []byte

// handleOpenAPI serves the hand-maintained OpenAPI 3.1 specification. Public
// (no auth) so tooling can discover the API. openapi_test.go asserts the spec
// stays in sync with Routes().
func (h *Handler) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	if h.rateLimited(w, r, h.generalLimiter, netutil.ClientIP(r), "general") {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write(openapiSpec)
}
