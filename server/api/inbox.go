package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gsbs/gsbs/server/store"
)

// handleListInbox serves GET /api/inbox: the caller's newest in-app
// notifications (v5.2), with the unread count.
func (h *Handler) handleListInbox(w http.ResponseWriter, r *http.Request, userID string) {
	if h.rateLimited(w, r, h.generalLimiter, userID, "general") {
		return
	}
	items, err := h.store.ListInbox(r.Context(), userID, 50)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "list inbox failed"})
		return
	}
	unread, _ := h.store.CountUnreadInbox(r.Context(), userID)
	out := make([]map[string]interface{}, 0, len(items))
	for _, it := range items {
		out = append(out, map[string]interface{}{
			"id": it.ID, "event_type": it.EventType, "title": it.Title,
			"body": it.Body, "link": it.Link, "created_at": it.CreatedAt, "read": it.Read,
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"items": out, "unread": unread})
}

// handleMarkInboxRead serves POST /api/inbox/read with JSON {"id":"...|all"}.
func (h *Handler) handleMarkInboxRead(w http.ResponseWriter, r *http.Request, userID string) {
	if h.rateLimited(w, r, h.generalLimiter, userID, "general") {
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if err := h.store.MarkInboxRead(r.Context(), userID, strings.TrimSpace(req.ID)); err != nil {
		if err == store.ErrNotFound {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "item not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "mark read failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
