package webui

import (
	"net/http"
	"strings"

	"github.com/gsbs/gsbs/server/logx"
	"github.com/gsbs/gsbs/server/store"
)

type conflictsPageData struct {
	PageData
	Conflicts []store.ConflictRow
	ReadOnly  bool
}

// serveConflictsPage renders the web Conflict Center (v5.2): unresolved push
// conflicts across all of the user's devices, with guided resolution.
func (h *WebHandler) serveConflictsPage(w http.ResponseWriter, r *http.Request) {
	userID, username, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	rows, err := h.store.ListOpenConflicts(r.Context(), userID)
	if err != nil {
		logx.Logger().Error().Str("user_id", userID).Err(err).Msg("conflicts page: list failed")
	}
	success := ""
	if r.URL.Query().Get("ok") == "resolved" {
		success = "Conflict marked resolved. The server's copy stands."
	}
	h.render(w, "dashboard_conflicts.html", conflictsPageData{
		PageData: pageDataWithAppearance(h, r, userID, PageData{
			PageName: "dashboard_conflicts", Username: username,
			IsAdmin:   h.isAdminUser(r.Context(), userID, username),
			CSRFToken: SetCSRFToken(w, r, h.secret), NavActive: "conflicts",
			Success: success,
			Error:   dashboardErrorMsg(r.URL.Query().Get("error")),
		}),
		Conflicts: rows,
		ReadOnly:  h.readOnly,
	})
}

// serveConflictsPartial re-renders the conflict list (SSE-triggered refresh).
func (h *WebHandler) serveConflictsPartial(w http.ResponseWriter, r *http.Request) {
	userID, _, ok := h.requireSession(w, r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	rows, _ := h.store.ListOpenConflicts(r.Context(), userID)
	h.renderPartial(w, "partials/conflicts_list.html", conflictsPageData{
		PageData:  PageData{CSRFToken: SetCSRFToken(w, r, h.secret)},
		Conflicts: rows,
		ReadOnly:  h.readOnly,
	})
}

// serveConflictBadge renders the sidebar badge fragment: the open-conflict
// count, or nothing when the user has none.
func (h *WebHandler) serveConflictBadge(w http.ResponseWriter, r *http.Request) {
	userID, _, ok := h.requireSession(w, r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	n, err := h.store.CountOpenConflicts(r.Context(), userID)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err != nil || n == 0 {
		return // empty fragment: badge disappears
	}
	h.renderPartial(w, "partials/conflict_badge.html", n)
}

// handleResolveConflictWeb marks one conflict resolved from the web UI.
func (h *WebHandler) handleResolveConflictWeb(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	userID, username, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	id := strings.TrimSpace(r.FormValue("conflict_id"))
	if id == "" {
		Redirect(w, r, "/dashboard/conflicts")
		return
	}
	if err := h.store.ResolveConflict(r.Context(), userID, id, "kept_server"); err != nil {
		Redirect(w, r, "/dashboard/conflicts?error=not_found")
		return
	}
	h.appendAuditBroadcast(r.Context(), userID, username, "resolve_conflict", id, "kept_server")
	Redirect(w, r, "/dashboard/conflicts?ok=resolved")
}
