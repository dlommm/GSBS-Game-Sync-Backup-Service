package api

import (
	"net/http"
	"time"
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
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"token":      newToken,
		"expires_in": int((90 * 24 * time.Hour).Seconds()),
	})
}
