package webui

import (
	"net/http"

	"github.com/gsbs/gsbs/server/sse"
	"github.com/gsbs/gsbs/server/store"
)

type inboxPanelData struct {
	PageData
	Items []store.InboxItem
}

// serveInboxPanel renders the bell dropdown content (v5.2).
func (h *WebHandler) serveInboxPanel(w http.ResponseWriter, r *http.Request) {
	userID, _, ok := h.requireSession(w, r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	items, _ := h.store.ListInbox(r.Context(), userID, 30)
	h.renderPartial(w, "partials/inbox_panel.html", inboxPanelData{
		PageData: PageData{CSRFToken: SetCSRFToken(w, r, h.secret)},
		Items:    items,
	})
}

// serveInboxBadge renders the bell's unread-count fragment (empty = hidden).
func (h *WebHandler) serveInboxBadge(w http.ResponseWriter, r *http.Request) {
	userID, _, ok := h.requireSession(w, r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	n, err := h.store.CountUnreadInbox(r.Context(), userID)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err != nil || n == 0 {
		return
	}
	h.renderPartial(w, "partials/conflict_badge.html", n)
}

// handleInboxReadAll marks everything read and re-renders the panel (the
// badge refreshes itself via the inbox-updated SSE broadcast).
func (h *WebHandler) handleInboxReadAll(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	userID, _, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	_ = h.store.MarkInboxRead(r.Context(), userID, "all")
	if h.hub != nil {
		h.hub.BroadcastToUser(userID, sse.Event{Type: "inbox-updated", Data: `{}`})
	}
	items, _ := h.store.ListInbox(r.Context(), userID, 30)
	h.renderPartial(w, "partials/inbox_panel.html", inboxPanelData{
		PageData: PageData{CSRFToken: SetCSRFToken(w, r, h.secret)},
		Items:    items,
	})
}
