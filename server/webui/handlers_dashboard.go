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

	ch, unsub := h.hub.Subscribe(userID)
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
	quotaBytes, _ := h.store.UserQuotaBytes(r.Context(), userID)
	success := ""
	if r.URL.Query().Get("revoked") == "1" {
		success = "Client token revoked. Run gsbs-client login to reconnect."
	}
	h.render(w, "dashboard.html", dashboardData{
		PageData: PageData{
			PageName:  "dashboard",
			Username:  username,
			IsAdmin:   h.isAdminUser(r.Context(), userID, username),
			CSRFToken: csrfToken,
			NavActive: "dashboard",
			Error:     dashboardErrorMsg(r.URL.Query().Get("error")),
			Success:   success,
			Restored:  r.URL.Query().Get("restored") == "1",
			Deleted:   r.URL.Query().Get("deleted") == "1",
		},
		QuotaBytes: quotaBytes,
	})
}

func (h *WebHandler) loadDashboardStats(ctx context.Context, userID string) dashboardStats {
	var stats dashboardStats
	clients, err := h.store.ListClientsByUserID(ctx, userID)
	if err != nil {
		logx.Logger().Error().Str("user_id", userID).Err(err).Msg("dashboard stats: list clients failed")
		stats.StoreError = true
	} else {
		stats.ClientCount = len(clients)
	}
	saves, err := h.store.ListSaveSummaries(ctx, userID)
	if err != nil {
		logx.Logger().Error().Str("user_id", userID).Err(err).Msg("dashboard stats: list saves failed")
		stats.StoreError = true
	} else {
		stats.SaveCount = len(saves)
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
	if len(cards) > dashboardRecentGames {
		cards = cards[:dashboardRecentGames]
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
