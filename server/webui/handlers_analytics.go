package webui

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gsbs/gsbs/server/job"
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

	// Deep-insights additions (window-scoped unless noted).
	WindowDays    int
	BytesByDay    []store.DayBytes
	BytesTotal    int64
	ByClient      []labelBar       // device attribution (version writes per device)
	WeekdaySeries []store.DayCount // Sun..Sat activity rhythm (rendered via barsSVG)
	HoursSeries   []store.DayCount // 0..23 UTC activity rhythm (rendered via barsSVG)
	MostActive    []activeGameRow
	Depth         store.SlotDepthStats // all-time
	CategoryBars  []labelBar           // Saves/Config/Other storage split (all-time)
	Protection    protectionInfo       // all-time
	Storage       []storageRow         // Storage Explorer (v5.1): per-game history footprint
	ReadOnly      bool                 // hides the prune action
	HasActivity   bool                 // any version writes in the window
}

// labelBar is a generic labelled horizontal/vertical bar (Pct of the max).
type labelBar struct {
	Label string
	Count int
	Bytes int64
	Pct   int
}

// activeGameRow ranks a game by version writes in the window.
type activeGameRow struct {
	GameID   string
	Title    string
	Versions int
	Bytes    int64
	Pct      int
}

// storageRow is one game's storage footprint in the Storage Explorer panel.
type storageRow struct {
	GameID       string
	Title        string
	CurrentBytes int64 // live save files
	Versions     int   // retained history versions
	HistoryBytes int64 // bytes held by version history
	Keep         int   // effective retention (versions kept per file)
	Prunable     bool  // more versions retained than policy keeps
}

// protectionInfo backs the "how safe are my saves" panel.
type protectionInfo struct {
	EncryptedSaves int
	TotalSaves     int
	EncryptedPct   int
	TOTPEnabled    bool
	DevicesOnline  int
	DeviceCount    int
	BackupsHealthy bool
	// Restore confidence (v5.1): server-wide backup + integrity truth,
	// shown to every user as reassurance (admins manage both).
	ServerBackupConfigured bool
	ServerBackupAt         string // finished_at of the last backup run; "" = never
	ServerBackupOK         bool
	IntegrityAt            string // finished_at of the last integrity check; "" = never
	IntegrityOK            bool
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

// insightsWindow parses the ?days selector (7/30/90; default 30).
func insightsWindow(r *http.Request) int {
	switch r.URL.Query().Get("days") {
	case "7":
		return 7
	case "90":
		return 90
	default:
		return analyticsWindowDays
	}
}

func pctOfMax(v, maxV int) int {
	if maxV <= 0 {
		return 0
	}
	return v * 100 / maxV
}

// buildUserInsights computes the per-user analytics for a given user. It is used
// both by the user's own Insights page and the admin drill-down.
func (h *WebHandler) buildUserInsights(ctx context.Context, userID string, days int) userInsights {
	if days <= 0 {
		days = analyticsWindowDays
	}
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
	syncByDay, err := h.store.SyncVolumeByDay(ctx, userID, days)
	if err != nil {
		logx.Logger().Error().Str("user_id", userID).Err(err).Msg("insights: sync volume by day failed")
	}
	syncTotal := 0
	for _, d := range syncByDay {
		syncTotal += d.Count
	}

	bytesByDay, _ := h.store.SyncBytesByDay(ctx, userID, days)
	var bytesTotal int64
	for _, d := range bytesByDay {
		bytesTotal += d.Bytes
	}

	// Device attribution.
	var byClient []labelBar
	if rows, err := h.store.VersionsByClient(ctx, userID, days); err == nil {
		maxN := 0
		for _, r := range rows {
			if r.Versions > maxN {
				maxN = r.Versions
			}
		}
		for _, r := range rows {
			byClient = append(byClient, labelBar{Label: r.ClientName, Count: r.Versions, Bytes: r.Bytes, Pct: pctOfMax(r.Versions, maxN)})
		}
	}

	// Activity rhythm (weekday + hour, UTC), reusing the barsSVG day-series shape.
	var weekdaySeries, hoursSeries []store.DayCount
	if wd, err := h.store.ActivityByWeekday(ctx, userID, days); err == nil {
		names := []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}
		for i, n := range wd {
			weekdaySeries = append(weekdaySeries, store.DayCount{Day: names[i], Count: n})
		}
	}
	if hh, err := h.store.ActivityByHour(ctx, userID, days); err == nil {
		for i, n := range hh {
			hoursSeries = append(hoursSeries, store.DayCount{Day: fmt.Sprintf("%02d:00 UTC", i), Count: n})
		}
	}

	// Most active games.
	var mostActive []activeGameRow
	if rows, err := h.store.MostActiveGames(ctx, userID, days, 5); err == nil {
		maxN := 0
		for _, r := range rows {
			if r.SaveCount > maxN {
				maxN = r.SaveCount
			}
		}
		for _, r := range rows {
			mostActive = append(mostActive, activeGameRow{
				GameID: r.GameID, Title: r.GameTitle, Versions: r.SaveCount, Bytes: r.StorageBytes,
				Pct: pctOfMax(r.SaveCount, maxN),
			})
		}
	}

	depth, _ := h.store.VersionDepth(ctx, userID)

	// Save vs config storage split from the existing category grouping.
	catBytes := map[string]int64{}
	for _, g := range groups {
		for _, c := range g.Categories {
			catBytes[c.Name] += c.TotalBytes
		}
	}
	var categoryBars []labelBar
	var maxCat int64 = 1
	for _, name := range []string{"Saves", "Config", "Other"} {
		if catBytes[name] > maxCat {
			maxCat = catBytes[name]
		}
	}
	for _, name := range []string{"Saves", "Config", "Other"} {
		if b, ok := catBytes[name]; ok && b > 0 {
			categoryBars = append(categoryBars, labelBar{Label: name, Bytes: b, Pct: int(b * 100 / maxCat)})
		}
	}

	// Protection panel.
	encSaves, totalSaves, _ := h.store.CountEncryptedSaves(ctx, userID)
	totpOn, _ := h.store.IsTOTPEnabled(ctx, userID)
	alerts := backupHealthAlerts(devices, len(saves))
	healthy := true
	for _, a := range alerts {
		if a.Tone == "danger" {
			healthy = false
		}
	}
	encPct := 0
	if totalSaves > 0 {
		encPct = encSaves * 100 / totalSaves
	}

	backupConfigured, backupAt, backupOK, integrityAt, integrityOK := h.restoreConfidence(ctx)

	// Storage Explorer (v5.1): per-game history footprint, biggest first.
	var storageRows []storageRow
	if perGame, err := h.store.VersionStorageByGame(ctx, userID); err == nil && len(perGame) > 0 {
		type gameRef struct {
			Title string
			Bytes int64
		}
		titles := make(map[string]gameRef, len(groups))
		for _, g := range groups {
			titles[g.GameID] = gameRef{Title: g.Title, Bytes: g.TotalBytes}
		}
		for _, vs := range perGame {
			if vs.Versions == 0 {
				continue
			}
			ref := titles[vs.GameID]
			title := ref.Title
			if title == "" {
				title = vs.GameID
			}
			keep := h.store.RetentionForGame(ctx, vs.GameID)
			storageRows = append(storageRows, storageRow{
				GameID: vs.GameID, Title: title, CurrentBytes: ref.Bytes,
				Versions: vs.Versions, HistoryBytes: vs.Bytes, Keep: keep,
				Prunable: vs.Versions > keep,
			})
		}
		sort.SliceStable(storageRows, func(i, j int) bool { return storageRows[i].HistoryBytes > storageRows[j].HistoryBytes })
		if len(storageRows) > 12 {
			storageRows = storageRows[:12]
		}
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
		Alerts:      alerts,
		LinkGames:   true,

		WindowDays:    days,
		BytesByDay:    bytesByDay,
		BytesTotal:    bytesTotal,
		ByClient:      byClient,
		WeekdaySeries: weekdaySeries,
		HoursSeries:   hoursSeries,
		MostActive:    mostActive,
		Depth:         depth,
		CategoryBars:  categoryBars,
		Protection: protectionInfo{
			EncryptedSaves: encSaves, TotalSaves: totalSaves, EncryptedPct: encPct,
			TOTPEnabled: totpOn, DevicesOnline: online, DeviceCount: len(devices),
			BackupsHealthy:         healthy,
			ServerBackupConfigured: backupConfigured,
			ServerBackupAt:         backupAt, ServerBackupOK: backupOK,
			IntegrityAt: integrityAt, IntegrityOK: integrityOK,
		},
		Storage:     storageRows,
		ReadOnly:    h.readOnly,
		HasActivity: syncTotal > 0,
	}
}

// handlePruneVersions trims one game's version history down to its effective
// retention (Storage Explorer, v5.1). Session-authenticated, CSRF-checked,
// audited; blocked in read-only mode.
func (h *WebHandler) handlePruneVersions(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	if h.readOnly {
		Redirect(w, r, "/dashboard/analytics?error=read_only")
		return
	}
	userID, username, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	gameID := strings.TrimSpace(r.FormValue("game_id"))
	if gameID == "" {
		Redirect(w, r, "/dashboard/analytics")
		return
	}
	deleted, freed, err := h.store.PruneVersionsForGame(r.Context(), userID, gameID)
	if err != nil {
		logx.Logger().Error().Str("user_id", userID).Str("game_id", gameID).Err(err).Msg("prune versions failed")
		Redirect(w, r, "/dashboard/analytics?error=prune_failed")
		return
	}
	h.appendAuditBroadcast(r.Context(), userID, username, "prune_versions", gameID,
		fmt.Sprintf("versions=%d freed=%d", deleted, freed))
	Redirect(w, r, "/dashboard/analytics?ok=pruned")
}

// restoreConfidence gathers the server-wide backup + integrity status shown
// in the user-facing "Restore confidence" panel (v5.1).
func (h *WebHandler) restoreConfidence(ctx context.Context) (configured bool, backupAt string, backupOK bool, integrityAt string, integrityOK bool) {
	if settings, err := h.store.ListAdminSettings(ctx); err == nil {
		configured = job.BackupEnabled(settings)
	}
	if runs, err := h.store.ListJobRuns(ctx, "backup", 1); err == nil && len(runs) > 0 && runs[0].FinishedAt != "" {
		backupAt = runs[0].FinishedAt
		backupOK = runs[0].Status == "success"
	}
	if runs, err := h.store.ListJobRuns(ctx, "integrity_check", 1); err == nil && len(runs) > 0 && runs[0].FinishedAt != "" {
		integrityAt = runs[0].FinishedAt
		integrityOK = runs[0].Status == "success"
	}
	return
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
		userInsights: h.buildUserInsights(r.Context(), userID, insightsWindow(r)),
	})
}
