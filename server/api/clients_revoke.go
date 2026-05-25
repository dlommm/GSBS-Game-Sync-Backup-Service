package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gsbs/gsbs/server/logx"
)

func (h *Handler) handleRevokeClient(w http.ResponseWriter, r *http.Request, userID string) {
	if h.rateLimited(w, r, h.generalLimiter, userID, "general") {
		return
	}
	var req struct {
		ClientID string `json:"client_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxAuthBody)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	clientID := strings.TrimSpace(req.ClientID)
	if clientID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "client_id required"})
		return
	}
	ownerID, err := h.store.ClientUserID(r.Context(), clientID)
	if err != nil || ownerID != userID {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "client not found or not owned by you"})
		return
	}
	if err := h.store.RegenerateClientToken(r.Context(), clientID); err != nil {
		logx.Logger().Error().Str("user_id", userID).Str("client_id", clientID).Err(err).Msg("api revoke client failed")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "revoke failed"})
		return
	}
	if username, _ := h.store.UsernameByID(r.Context(), userID); username != "" {
		_ = h.store.AppendAudit(r.Context(), userID, username, "revoke_client", clientID, "")
	}
	logx.Logger().Info().Str("user_id", userID).Str("client_id", clientID).Msg("api revoke client ok")
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
