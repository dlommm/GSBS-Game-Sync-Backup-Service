package webui

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gsbs/gsbs/server/logx"
)

func (h *WebHandler) serveDashboardEvents(w http.ResponseWriter, r *http.Request) {
	userID, _ := h.getSessionUser(r)
	if userID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	// Rolling write deadline, refreshed on every write: healthy streams live
	// forever, dead peers are dropped after ~3 missed 30s heartbeats.
	rc := http.NewResponseController(w)
	extend := func() { _ = rc.SetWriteDeadline(time.Now().Add(90 * time.Second)) }
	extend()
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Same per-user cap as the API stream: browser tabs and reconnect loops
	// must not accumulate unbounded goroutines/channels (oldest is evicted).
	ch, unsub := h.hub.SubscribeCapped(userID, 5)
	defer unsub()

	fmt.Fprint(w, ": heartbeat\n\n")
	flusher.Flush()

	// Heartbeat keeps the stream alive through proxies that drop idle SSE
	// connections and gives the rolling deadline regular refresh points.
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case evt, ok := <-ch:
			if !ok {
				return
			}
			extend()
			fmt.Fprint(w, evt.Format())
			flusher.Flush()
		case <-ticker.C:
			extend()
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (h *WebHandler) serveDashboard(w http.ResponseWriter, r *http.Request) {
	userID, username, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	csrfToken := SetCSRFToken(w, r, h.secret)
	design, uiLayout := h.appearance(r.Context(), userID)
	success := ""
	if r.URL.Query().Get("revoked") == "1" {
		success = "Client token revoked. Run gsbs-client login to reconnect."
	}
	if r.URL.Query().Get("widgets_saved") == "1" {
		success = "Dashboard arrangement saved."
	}
	var widgets []dashWidgetView
	if uiLayout == "widgets" {
		raw, _ := h.store.GetUserPref(r.Context(), userID, widgetPrefKey)
		cfg := parseWidgetConfig(raw)
		hidden := map[string]bool{}
		for _, id := range cfg.Hidden {
			hidden[id] = true
		}
		for _, id := range cfg.Order {
			widgets = append(widgets, dashWidgetView{ID: id, Name: widgetName(id), Hidden: hidden[id]})
		}
	}
	h.render(w, "dashboard.html", dashboardData{
		Widgets: widgets,
		PageData: PageData{
			PageName:  "dashboard",
			Username:  username,
			IsAdmin:   h.isAdminUser(r.Context(), userID, username),
			CSRFToken: csrfToken,
			NavActive: "dashboard",
			Design:    design,
			Layout:    uiLayout,
			Error:     dashboardErrorMsg(r.URL.Query().Get("error")),
			Success:   success,
			Restored:  r.URL.Query().Get("restored") == "1",
			Deleted:   r.URL.Query().Get("deleted") == "1",
		},
	})
}

func (h *WebHandler) loadDashboardStats(ctx context.Context, userID string) dashboardStats {
	var stats dashboardStats
	clientCount, err := h.store.CountClientsByUser(ctx, userID)
	if err != nil {
		logx.Logger().Error().Str("user_id", userID).Err(err).Msg("dashboard stats: count clients failed")
		stats.StoreError = true
	} else {
		stats.ClientCount = clientCount
	}
	saveCount, err := h.store.CountSavesByUser(ctx, userID)
	if err != nil {
		logx.Logger().Error().Str("user_id", userID).Err(err).Msg("dashboard stats: count saves failed")
		stats.StoreError = true
	} else {
		stats.SaveCount = saveCount
	}
	totalBytes, err := h.store.UserStorageBytes(ctx, userID)
	if err != nil {
		logx.Logger().Error().Str("user_id", userID).Err(err).Msg("dashboard stats: user storage bytes failed")
		stats.StoreError = true
	} else {
		stats.TotalBytes = totalBytes
	}
	usageBytes, err := h.store.StorageUsage(ctx, userID)
	if err != nil {
		logx.Logger().Error().Str("user_id", userID).Err(err).Msg("dashboard stats: storage usage failed")
		stats.StoreError = true
	} else {
		stats.UsageBytes = usageBytes
	}
	gameCount, err := h.store.DistinctGameCount(ctx, userID)
	if err != nil {
		logx.Logger().Error().Str("user_id", userID).Err(err).Msg("dashboard stats: distinct game count failed")
		stats.StoreError = true
	} else {
		stats.GameCount = gameCount
	}
	return stats
}

func (h *WebHandler) serveDashboardStatsPartial(w http.ResponseWriter, r *http.Request) {
	userID, _, ok := h.requireSession(w, r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	stats := h.loadDashboardStats(r.Context(), userID)
	quotaBytes, _ := h.store.UserQuotaBytes(r.Context(), userID)
	h.renderPartial(w, "partials/dashboard_stats.html", map[string]interface{}{
		"Stats":      stats,
		"QuotaBytes": quotaBytes,
	})
}

func (h *WebHandler) serveDashboardClientsPartial(w http.ResponseWriter, r *http.Request) {
	userID, _, ok := h.requireSession(w, r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	clients, err := h.store.ListClientsByUserID(r.Context(), userID)
	if err != nil {
		http.Error(w, "Failed to load clients", http.StatusInternalServerError)
		return
	}
	csrfToken := SetCSRFToken(w, r, h.secret)
	h.renderPartial(w, "partials/dashboard_clients.html", map[string]interface{}{
		"Clients": clients, "CSRFToken": csrfToken, "ReadOnly": h.readOnly,
	})
}

// dashboardRecentGames caps the dashboard "Recent games" card strip.
const dashboardRecentGames = 6

// serveDashboardSavesPartial renders the dashboard "Recent games" cards: the
// most recently synced games, reusing the My Games card builder (groupSaves
// order is most-recent-first, which is the default sort).
func (h *WebHandler) serveDashboardSavesPartial(w http.ResponseWriter, r *http.Request) {
	userID, _, ok := h.requireSession(w, r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	cards, _, _, _ := h.buildGameCards(r.Context(), userID, "", "all", "recent")
	total := len(cards)
	limit := dashboardRecentGames
	if _, uiLayout := h.appearance(r.Context(), userID); uiLayout == "library" {
		// Library-first: the shelf is the dashboard's point — show more.
		limit = 2 * dashboardRecentGames
	}
	if len(cards) > limit {
		cards = cards[:limit]
	}
	h.renderPartial(w, "partials/dashboard_saves.html", map[string]interface{}{
		"Games":      cards,
		"TotalGames": total,
	})
}

const dashboardActivityPageSize = 20

func (h *WebHandler) serveDashboardActivityPartial(w http.ResponseWriter, r *http.Request) {
	userID, _, ok := h.requireSession(w, r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	offset := parseNonNegativeInt(r.URL.Query().Get("offset"), 0)
	// Fetch one extra row to know whether a "Load more" tail is needed.
	entries, err := h.store.ListAuditLogByUser(r.Context(), userID, dashboardActivityPageSize+1, offset)
	if err != nil {
		logx.Logger().Error().Str("user_id", userID).Err(err).Msg("dashboard activity: list audit log failed")
	}
	hasMore := len(entries) > dashboardActivityPageSize
	if hasMore {
		entries = entries[:dashboardActivityPageSize]
	}
	h.renderPartial(w, "partials/dashboard_activity.html", map[string]interface{}{
		"Entries":    entries,
		"StoreError": err != nil,
		"Append":     offset > 0,
		"HasMore":    hasMore,
		"NextOffset": offset + len(entries),
	})
}

func (h *WebHandler) handleDashboardRevokeClient(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	userID, username, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	clientID := r.FormValue("client_id")
	if clientID == "" {
		Redirect(w, r, "/dashboard?error=missing_client")
		return
	}
	ownerID, err := h.store.ClientUserID(r.Context(), clientID)
	if err != nil || ownerID != userID {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if err := h.store.RevokeClient(r.Context(), clientID); err != nil {
		logx.Logger().Error().Str("client_id", clientID).Err(err).Msg("webui dashboard revoke failed")
		Redirect(w, r, "/dashboard?error=revoke_failed")
		return
	}
	h.appendAuditBroadcast(r.Context(), userID, username, "revoke_client", clientID, "")
	logx.Logger().Info().Str("client_id", clientID).Str("username", username).Msg("webui dashboard revoke ok")
	Redirect(w, r, "/dashboard?revoked=1")
}

// widgetName is the human label for a dashboard widget id (the editor's
// control cluster names what it moves).
func widgetName(id string) string {
	switch id {
	case "stats":
		return "Stats"
	case "games":
		return "Recent games"
	case "activity":
		return "Recent Activity"
	case "devices":
		return "Your Devices"
	case "pulse":
		return "Live activity"
	}
	return id
}

// handleWidgetsSave stores the Custom layout's widget arrangement.
func (h *WebHandler) handleWidgetsSave(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	userID, _, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	if r.FormValue("reset") == "1" {
		if err := h.store.SetUserPref(r.Context(), userID, widgetPrefKey, ""); err != nil {
			Redirect(w, r, "/dashboard?error=widgets_save_failed")
			return
		}
		Redirect(w, r, "/dashboard?widgets_saved=1")
		return
	}
	cfg, valid := widgetConfigFromForm(r.FormValue("order"), r.FormValue("hidden"))
	if !valid {
		Redirect(w, r, "/dashboard?error=invalid_widgets")
		return
	}
	if err := h.store.SetUserPref(r.Context(), userID, widgetPrefKey, cfg.marshal()); err != nil {
		Redirect(w, r, "/dashboard?error=widgets_save_failed")
		return
	}
	Redirect(w, r, "/dashboard?widgets_saved=1")
}
