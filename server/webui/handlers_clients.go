package webui

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gsbs/gsbs/server/logx"
	"github.com/gsbs/gsbs/server/store"
)

// clientOnlineWindow: a device is shown "online" when its last_seen is within
// this window. Pushes/pulls bump last_seen, so an actively-syncing device shows
// online; the clients list polls every 30s to keep this fresh.
const clientOnlineWindow = 5 * time.Minute

type clientRow struct {
	store.ClientInfo
	Online bool
}

type dashboardClientsData struct {
	PageData
	Clients  []clientRow
	Online   int
	Total    int
	ReadOnly bool
}

func clientIsOnline(lastSeen string) bool {
	t, err := time.Parse(time.RFC3339, lastSeen)
	if err != nil {
		return false
	}
	return time.Since(t) <= clientOnlineWindow
}

func (h *WebHandler) loadClientRows(ctx context.Context, userID string) (rows []clientRow, online int) {
	clients, err := h.store.ListClientsByUserID(ctx, userID)
	if err != nil {
		logx.Logger().Error().Str("user_id", userID).Err(err).Msg("clients page: list clients failed")
		return nil, 0
	}
	rows = make([]clientRow, 0, len(clients))
	for _, c := range clients {
		on := clientIsOnline(c.LastSeen)
		if on {
			online++
		}
		rows = append(rows, clientRow{ClientInfo: c, Online: on})
	}
	return rows, online
}

func (h *WebHandler) serveDashboardClientsPage(w http.ResponseWriter, r *http.Request) {
	userID, username, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	csrfToken := SetCSRFToken(w, r, h.secret)
	success := ""
	switch r.URL.Query().Get("ok") {
	case "renamed":
		success = "Device renamed."
	case "revoked":
		success = "Device revoked. Run gsbs-client login to reconnect it."
	}
	rows, online := h.loadClientRows(r.Context(), userID)
	h.render(w, "dashboard_clients.html", dashboardClientsData{
		PageData: PageData{
			PageName: "dashboard_clients", Username: username,
			IsAdmin:   h.isAdminUser(r.Context(), userID, username),
			CSRFToken: csrfToken, NavActive: "clients",
			Error:   dashboardErrorMsg(r.URL.Query().Get("error")),
			Success: success,
		},
		Clients:  rows,
		Online:   online,
		Total:    len(rows),
		ReadOnly: h.readOnly,
	})
}

func (h *WebHandler) serveDashboardClientsListPartial(w http.ResponseWriter, r *http.Request) {
	userID, _, ok := h.requireSession(w, r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	csrfToken := SetCSRFToken(w, r, h.secret)
	rows, online := h.loadClientRows(r.Context(), userID)
	h.renderPartial(w, "partials/clients_list.html", dashboardClientsData{
		PageData: PageData{CSRFToken: csrfToken},
		Clients:  rows,
		Online:   online,
		Total:    len(rows),
		ReadOnly: h.readOnly,
	})
}

func (h *WebHandler) handleDashboardRenameClient(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	if h.readOnly {
		Redirect(w, r, "/dashboard/clients?error=read_only")
		return
	}
	userID, username, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	clientID := strings.TrimSpace(r.FormValue("client_id"))
	name := strings.TrimSpace(r.FormValue("name"))
	if clientID == "" || name == "" {
		Redirect(w, r, "/dashboard/clients?error=missing_client")
		return
	}
	if len(name) > 64 {
		name = name[:64]
	}
	owner, err := h.store.ClientUserID(r.Context(), clientID)
	if err != nil || owner != userID {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if err := h.store.RenameClient(r.Context(), userID, clientID, name); err != nil {
		logx.Logger().Error().Str("client_id", clientID).Err(err).Msg("webui rename client failed")
		Redirect(w, r, "/dashboard/clients?error=rename_failed")
		return
	}
	h.appendAuditBroadcast(r.Context(), userID, username, "rename_client", clientID, "name="+name)
	Redirect(w, r, "/dashboard/clients?ok=renamed")
}
