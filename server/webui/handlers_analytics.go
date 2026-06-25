package webui

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/gsbs/gsbs/server/logx"
	"github.com/gsbs/gsbs/server/store"
)

// analyticsWindowDays is the trailing window for the sync-volume chart.
const analyticsWindowDays = 30

// staleClientDays: a device with no activity for this many days raises a health alert.
const staleClientDays = 5

type topGame struct {
	GameID     string
	Title      string
	TotalBytes int64
	FileCount  int
	Pct        int // bar width relative to the largest game
}

type healthAlert struct {
	Tone string // ok, warn, danger
	Text string
}

// userInsights is the per-user analytics view model shared by the user-facing
// Insights page and the admin per-user drill-down (both embed it).
type userInsights struct {
	GameCount   int
	SaveCount   int
	TotalBytes  int64
	DeviceCount int
	OnlineCount int
	SyncByDay   []store.DayCount
	SyncTotal   int
	TopGames    []topGame
	Devices     []clientRow
	Alerts      []healthAlert
	// LinkGames controls whether top-game rows link to the user-facing game
	// detail page. False in the admin drill-down (those routes are the admin's
	// own saves, not the target user's).
	LinkGames bool
}

type analyticsData struct {
	PageData
	userInsights
}

func topGamesFromGroups(groups []saveGameGroup, limit int) []topGame {
	sorted := make([]saveGameGroup, len(groups))
	copy(sorted, groups)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].TotalBytes > sorted[j].TotalBytes })
	if len(sorted) > limit {
		sorted = sorted[:limit]
	}
	var maxBytes int64 = 1
	for _, g := range sorted {
		if g.TotalBytes > maxBytes {
			maxBytes = g.TotalBytes
		}
	}
	out := make([]topGame, 0, len(sorted))
	for _, g := range sorted {
		out = append(out, topGame{
			GameID: g.GameID, Title: g.Title, TotalBytes: g.TotalBytes, FileCount: g.FileCount,
			Pct: int(g.TotalBytes * 100 / maxBytes),
		})
	}
	return out
}

// backupHealthAlerts derives human-readable health notices from device activity.
func backupHealthAlerts(devices []clientRow, saveCount int) []healthAlert {
	var alerts []healthAlert
	if len(devices) == 0 {
		alerts = append(alerts, healthAlert{Tone: "warn", Text: "No devices connected — install the GSBS client to start backing up your saves."})
		return alerts
	}
	if saveCount == 0 {
		alerts = append(alerts, healthAlert{Tone: "warn", Text: "No saves backed up yet. Add a game in the GSBS client and it will sync automatically."})
	}
	stale := 0
	for _, d := range devices {
		t, err := time.Parse(time.RFC3339, d.LastSeen)
		if err != nil {
			continue
		}
		days := int(time.Since(t).Hours() / 24)
		if days >= staleClientDays {
			stale++
			alerts = append(alerts, healthAlert{
				Tone: "danger",
				Text: fmt.Sprintf("%s hasn't synced in %d days.", d.Name, days),
			})
		}
	}
	if stale == 0 && saveCount > 0 {
		alerts = append(alerts, healthAlert{Tone: "ok", Text: "All devices have synced recently. Your backups are healthy."})
	}
	return alerts
}

// buildUserInsights computes the per-user analytics for a given user. It is used
// both by the user's own Insights page and the admin drill-down.
func (h *WebHandler) buildUserInsights(ctx context.Context, userID string) userInsights {
	saves, err := h.store.ListSaveSummaries(ctx, userID)
	if err != nil {
		logx.Logger().Error().Str("user_id", userID).Err(err).Msg("insights: list saves failed")
	}
	groups := groupSaves(saves)
	var totalBytes int64
	for _, g := range groups {
		totalBytes += g.TotalBytes
	}
	devices, online := h.loadClientRows(ctx, userID)
	syncByDay, err := h.store.SyncVolumeByDay(ctx, userID, analyticsWindowDays)
	if err != nil {
		logx.Logger().Error().Str("user_id", userID).Err(err).Msg("insights: sync volume by day failed")
	}
	syncTotal := 0
	for _, d := range syncByDay {
		syncTotal += d.Count
	}
	return userInsights{
		GameCount:   len(groups),
		SaveCount:   len(saves),
		TotalBytes:  totalBytes,
		DeviceCount: len(devices),
		OnlineCount: online,
		SyncByDay:   syncByDay,
		SyncTotal:   syncTotal,
		TopGames:    topGamesFromGroups(groups, 5),
		Devices:     devices,
		Alerts:      backupHealthAlerts(devices, len(saves)),
		LinkGames:   true,
	}
}

func (h *WebHandler) serveDashboardAnalytics(w http.ResponseWriter, r *http.Request) {
	userID, username, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	csrfToken := SetCSRFToken(w, r, h.secret)
	h.render(w, "dashboard_analytics.html", analyticsData{
		PageData: PageData{
			PageName: "dashboard_analytics", Username: username,
			IsAdmin:   h.isAdminUser(r.Context(), userID, username),
			CSRFToken: csrfToken, NavActive: "analytics",
		},
		userInsights: h.buildUserInsights(r.Context(), userID),
	})
}
