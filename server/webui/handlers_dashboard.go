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
	h.render(w, "dashboard.html", dashboardData{
		Stats: h.dashboardStatsData(r.Context(), userID),
		Saves: h.dashboardSavesData(r.Context(), userID),
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
	h.renderPartial(w, "partials/dashboard_stats.html", h.dashboardStatsData(r.Context(), userID))
}

// dashboardStatsData feeds partials/dashboard_stats.html (the Continue hero)
// for both the first page render and its live HTMX refreshes.
func (h *WebHandler) dashboardStatsData(ctx context.Context, userID string) map[string]interface{} {
	stats := h.loadDashboardStats(ctx, userID)
	quotaBytes, _ := h.store.UserQuotaBytes(ctx, userID)
	return map[string]interface{}{
		"Stats":      stats,
		"QuotaBytes": quotaBytes,
		"Protection": h.loadProtection(ctx, userID, stats),
	}
}

// protectionSummary drives the dashboard's protection hero: one answer to
// "are my saves safe and in sync?", built only from data the server holds.
// It never claims more than it knows — e.g. "online" means seen in the last
// clientOnlineWindow, not "fully synced".
type protectionSummary struct {
	Tone          string    // ok | warn | attention | empty
	LatestGame    *gameCard // most recently synced game (nil when none)
	GamesTotal    int
	StaleGames    int // no sync for 30+ days (gameSyncStatus "stale")
	OpenConflicts int
	DevicesTotal  int
	DevicesOnline int
	StaleDevices  int // not seen for staleAfter (14 days)
	// The latest save itself (the Continue hero): which version it is, which
	// device wrote it, and how many earlier versions of that slot are kept.
	LatestVersion int
	LatestDevice  string
	LatestOlder   int
	VersionsKept  int // every stored version across all slots (current ones included)
}

func (h *WebHandler) loadProtection(ctx context.Context, userID string, stats dashboardStats) protectionSummary {
	p := protectionSummary{Tone: "ok"}
	cards, _, _, _ := h.buildGameCards(ctx, userID, "", "all", "recent")
	p.GamesTotal = len(cards)
	if len(cards) > 0 {
		latest := cards[0]
		p.LatestGame = &latest
		if slot, ok := h.latestSlot(ctx, userID, latest.GameID); ok {
			if vers, err := h.store.ListSaveVersions(ctx, userID, latest.GameID, slot, 100); err == nil && len(vers) > 0 {
				p.LatestVersion, p.LatestDevice, p.LatestOlder = vers[0].Version, vers[0].ClientName, len(vers)-1
			}
		}
	}
	if depth, err := h.store.VersionDepth(ctx, userID); err == nil {
		p.VersionsKept = depth.Versions
	}
	for _, c := range cards {
		if c.Status == "stale" {
			p.StaleGames++
		}
	}
	if n, err := h.store.CountOpenConflicts(ctx, userID); err == nil {
		p.OpenConflicts = n
	}
	rows, online := h.loadClientRows(ctx, userID)
	p.DevicesTotal, p.DevicesOnline = len(rows), online
	for _, row := range rows {
		if row.StaleDays > 0 {
			p.StaleDevices++
		}
	}
	switch {
	case p.GamesTotal == 0 && !stats.StoreError:
		p.Tone = "empty"
	case p.OpenConflicts > 0:
		p.Tone = "attention"
	case p.StaleGames > 0 || p.StaleDevices > 0:
		p.Tone = "warn"
	}
	return p
}

func (h *WebHandler) serveDashboardClientsPartial(w http.ResponseWriter, r *http.Request) {
	userID, _, ok := h.requireSession(w, r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	rows, online := h.loadClientRows(r.Context(), userID)
	h.renderPartial(w, "partials/dashboard_clients.html", map[string]interface{}{
		"Clients": rows, "Online": online,
	})
}

// dashboardRecentGames caps the dashboard "Recent games" card strip.
const dashboardRecentGames = 6

// serveDashboardSavesPartial renders the Continue screen's save slots: the
// most recently synced games, reusing the My Games card builder (groupSaves
// order is most-recent-first, which is the default sort).
func (h *WebHandler) serveDashboardSavesPartial(w http.ResponseWriter, r *http.Request) {
	userID, _, ok := h.requireSession(w, r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	h.renderPartial(w, "partials/dashboard_saves.html", h.dashboardSavesData(r.Context(), userID))
}

func (h *WebHandler) dashboardSavesData(ctx context.Context, userID string) map[string]interface{} {
	cards, _, _, _ := h.buildGameCards(ctx, userID, "", "all", "recent")
	total := len(cards)
	if len(cards) > dashboardRecentGames {
		cards = cards[:dashboardRecentGames]
	}
	// Save-slot rows name the version and the device that wrote it — one
	// indexed lookup per row, capped at dashboardRecentGames rows.
	for i := range cards {
		if slot, ok := h.latestSlot(ctx, userID, cards[i].GameID); ok {
			if vers, err := h.store.ListSaveVersions(ctx, userID, cards[i].GameID, slot, 1); err == nil && len(vers) > 0 {
				cards[i].LatestVersion, cards[i].LatestDevice = vers[0].Version, vers[0].ClientName
			}
		}
	}
	return map[string]interface{}{"Games": cards, "TotalGames": total}
}

// latestSlot returns the path_key of a game's most recently updated save.
func (h *WebHandler) latestSlot(ctx context.Context, userID, gameID string) (string, bool) {
	saves, err := h.store.ListSaveSummariesForGame(ctx, userID, gameID)
	if err != nil || len(saves) == 0 {
		return "", false
	}
	best := saves[0]
	for _, sv := range saves[1:] {
		if sv.UpdatedAt > best.UpdatedAt {
			best = sv
		}
	}
	return best.PathKey, true
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
