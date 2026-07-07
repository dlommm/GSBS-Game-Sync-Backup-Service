package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gsbs/gsbs/server/logx"
	"github.com/gsbs/gsbs/server/sse"
	"github.com/gsbs/gsbs/server/store"
)

// recordConflict persists a push 409 for the web Conflict Center (v5.2) and
// broadcasts it. Best-effort: failures only log — the 409 response to the
// client must not be affected.
func (h *Handler) recordConflict(r *http.Request, userID, gameID, pathKey, kind, incomingHash, serverHash string, serverVersion int) {
	clientID, _ := r.Context().Value(contextClientID).(string)
	id, err := h.store.RecordConflict(r.Context(), store.ConflictRecord{
		UserID: userID, GameID: gameID, PathKey: pathKey, ClientID: clientID,
		Kind: kind, IncomingHash: incomingHash, ServerHash: serverHash, ServerVersion: serverVersion,
	})
	if err != nil {
		logx.Logger().Error().Str("user_id", userID).Str("game_id", gameID).Err(err).Msg("record conflict failed")
		return
	}
	if h.hub != nil {
		payload, _ := json.Marshal(map[string]interface{}{
			"id": id, "game_id": gameID, "path_key": pathKey, "client_id": clientID,
			"at": time.Now().UTC().Format(time.RFC3339),
		})
		h.hub.BroadcastToUser(userID, sse.Event{Type: "conflict-recorded", Data: string(payload)})
	}
}

// resolveSupersededConflicts closes open conflicts on a slot after a
// successful push (the collision is over — newer content stands).
func (h *Handler) resolveSupersededConflicts(r *http.Request, userID, gameID, pathKey string) {
	n, err := h.store.ResolveConflictsForSlot(r.Context(), userID, gameID, pathKey, "superseded")
	if err != nil || n == 0 {
		return
	}
	if h.hub != nil {
		payload, _ := json.Marshal(map[string]interface{}{"game_id": gameID, "path_key": pathKey, "resolved": n})
		h.hub.BroadcastToUser(userID, sse.Event{Type: "conflict-resolved", Data: string(payload)})
	}
}

// handleListConflicts serves GET /api/conflicts: the caller's unresolved
// conflicts, newest first.
func (h *Handler) handleListConflicts(w http.ResponseWriter, r *http.Request, userID string) {
	if h.rateLimited(w, r, h.generalLimiter, userID, "general") {
		return
	}
	rows, err := h.store.ListOpenConflicts(r.Context(), userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "list conflicts failed"})
		return
	}
	out := make([]map[string]interface{}, 0, len(rows))
	for _, c := range rows {
		out = append(out, map[string]interface{}{
			"id": c.ID, "game_id": c.GameID, "game_title": c.GameTitle,
			"path_key": c.PathKey, "relative_path": c.RelativePath,
			"client_id": c.ClientID, "client_name": c.ClientName,
			"kind": c.Kind, "incoming_hash": c.IncomingHash, "server_hash": c.ServerHash,
			"server_version": c.ServerVersion, "detected_at": c.DetectedAt,
			"occurrences": c.Occurrences,
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"conflicts": out})
}

// handleResolveConflict serves POST /api/conflicts/resolve with JSON
// {"id":"...","resolution":"kept_server"}. Marks the caller's conflict
// resolved; it does not modify save content (restore/push do that).
func (h *Handler) handleResolveConflict(w http.ResponseWriter, r *http.Request, userID string) {
	if h.rateLimited(w, r, h.generalLimiter, userID, "general") {
		return
	}
	var req struct {
		ID         string `json:"id"`
		Resolution string `json:"resolution"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil || strings.TrimSpace(req.ID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id required"})
		return
	}
	resolution := strings.TrimSpace(req.Resolution)
	switch resolution {
	case "", "kept_server":
		resolution = "kept_server"
	case "kept_local", "dismissed":
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid resolution"})
		return
	}
	if err := h.store.ResolveConflict(r.Context(), userID, req.ID, resolution); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "conflict not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
