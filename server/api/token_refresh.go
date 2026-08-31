package api

import (
	"net/http"

	"github.com/gsbs/gsbs/server/store"
)

func (h *Handler) handleTokenRefresh(w http.ResponseWriter, r *http.Request, userID string) {
	if h.rateLimited(w, r, h.generalLimiter, userID, "general") {
		return
	}
	token := getToken(r)
	if token == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing token"})
		return
	}
	newToken, err := h.store.RefreshClientToken(r.Context(), token)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "refresh failed"})
		return
	}
	// Report the lifetime this server actually enforces. Hardcoding 90 days
	// meant an operator who shortened GSBS_TOKEN_MAX_AGE had clients schedule
	// their refresh long after the token had already expired — a hard logout
	// instead of a rotation.
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"token":      newToken,
		"expires_in": int(store.TokenMaxAge().Seconds()),
	})
}
