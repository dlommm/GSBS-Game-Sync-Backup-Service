package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// handlePostSession serves POST /api/sessions (v5.2): a game-aware client
// reports one finished play session. Rendered as markers on the save version
// timeline. Sanity bounds keep noise out: 1 minute to 24 hours, ended in the
// past, RFC3339 timestamps.
func (h *Handler) handlePostSession(w http.ResponseWriter, r *http.Request, userID string) {
	if h.rateLimited(w, r, h.generalLimiter, userID, "general") {
		return
	}
	var req struct {
		GameID    string `json:"game_id"`
		StartedAt string `json:"started_at"`
		EndedAt   string `json:"ended_at"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil || strings.TrimSpace(req.GameID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "game_id, started_at, ended_at required"})
		return
	}
	start, err1 := time.Parse(time.RFC3339, req.StartedAt)
	end, err2 := time.Parse(time.RFC3339, req.EndedAt)
	if err1 != nil || err2 != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "timestamps must be RFC3339"})
		return
	}
	dur := end.Sub(start)
	if dur < time.Minute || dur > 24*time.Hour || end.After(time.Now().Add(5*time.Minute)) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "implausible session bounds"})
		return
	}
	clientID, _ := r.Context().Value(contextClientID).(string)
	id, err := h.store.RecordGameSession(r.Context(), userID, clientID, strings.TrimSpace(req.GameID),
		start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "record session failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "id": id})
}
