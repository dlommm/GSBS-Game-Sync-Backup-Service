package webui

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gsbs/gsbs/pkg/types"
	"github.com/gsbs/gsbs/server/store"
)

type adminAnalyticsData struct {
	PageData
	Tab string // overview, pcgw, sync

	// Overview tab
	TotalStorage     int64
	TotalUsers       int
	TotalClients     int
	TotalSaves       int
	ActiveClients24h int
	SyncVolume7d     int
	ManifestGames    int
	SaveGames        int
	PCGWCoveragePct  float64
	StatsSnapshots   []store.StatsSnapshotRow
	TopUsers         []store.UserStatRow
	TopSaveGames     []store.SaveGameStatRow

	// PCGW tab
	PCGWStats             PCGWStatsView
	CatalogStats          types.PCGWCatalogStats
	LatestSyncRun         *types.PCGWSyncRun
	ManifestMeta          *types.PCGWManifestMeta
	ManifestSaveLocations int
	ParseFailureCount     int
	ParseFailures         []store.PCGWParseFailureRow
	Games                 []types.PCGWGame
	Query                 string
	FilterStatus          string
	FilterPlatform        string
	Pager                 pagerView

	// Sync tab
	PCGWSyncRuns       []types.PCGWSyncRun
	SyncFilterMode     string
	SyncFilterStatus   string
	SyncRunsTotal      int
	SyncRunsSuccess    int
	SyncRunsFailed     int
	SyncRunsSuccessPct float64

	// Overview tab: adoption & reliability panels
	VersionDist []labelBar
	OSDist      []labelBar
	Adoption    store.AdoptionStats
	TOTPPct     int
	EncPct      int
	Signups     []store.MonthCount
	SignupTotal int
	JobStats    []store.JobStatRow
	TrendWindow int // stats-snapshot window (days): 30 / 90 / 365

	// Fleet Activity tab: real save-sync activity across all users
	FleetWindowDays  int
	FleetVolume      []store.DayCount
	FleetVolumeTotal int
	FleetBytes       []store.DayBytes
	FleetBytesTotal  int64
	ActiveUsers      []store.DayCount
	AuditActions     []labelBar
	AuditByDay       []store.DayCount
	AuditTotal       int
	FetchByDay       []store.DayCount
	FetchTotal       int
}

func labelBarsFromCounts(rows []store.LabelCount) []labelBar {
	maxN := 0
	for _, r := range rows {
		if r.Count > maxN {
			maxN = r.Count
		}
	}
	out := make([]labelBar, 0, len(rows))
	for _, r := range rows {
		out = append(out, labelBar{Label: r.Label, Count: r.Count, Pct: pctOfMax(r.Count, maxN)})
	}
	return out
}

// downsample keeps at most maxPoints evenly-spaced rows (oldest→newest order
// is preserved; the last row is always kept so "now" stays on the chart).
func downsampleSnapshots(rows []store.StatsSnapshotRow, maxPoints int) []store.StatsSnapshotRow {
	if len(rows) <= maxPoints || maxPoints <= 1 {
		return rows
	}
	out := make([]store.StatsSnapshotRow, 0, maxPoints)
	step := float64(len(rows)-1) / float64(maxPoints-1)
	for i := 0; i < maxPoints; i++ {
		out = append(out, rows[int(float64(i)*step+0.5)])
	}
	return out
}

func analyticsTabFromQuery(r *http.Request) string {
	switch strings.TrimSpace(r.URL.Query().Get("tab")) {
	case "pcgw", "sync", "overview", "fleet":
		return strings.TrimSpace(r.URL.Query().Get("tab"))
	default:
		return "overview"
	}
}

func trendWindowFromQuery(r *http.Request) int {
	switch r.URL.Query().Get("window") {
	case "90":
		return 90
	case "365":
		return 365
	default:
		return 30
	}
}

func filterPCGWSyncRuns(runs []types.PCGWSyncRun, mode, status string) []types.PCGWSyncRun {
	mode = strings.TrimSpace(mode)
	status = strings.TrimSpace(status)
	if mode == "" && status == "" {
		return runs
	}
	out := make([]types.PCGWSyncRun, 0, len(runs))
	for _, run := range runs {
		if mode != "" && !strings.EqualFold(run.Mode, mode) {
			continue
		}
		if status != "" && !strings.EqualFold(run.Status, status) {
			continue
		}
		out = append(out, run)
	}
	return out
}

func syncRunSummary(runs []types.PCGWSyncRun) (total, success, failed int, successPct float64) {
	total = len(runs)
	for _, run := range runs {
		switch strings.ToLower(run.Status) {
		case "success":
			success++
		case "failed", "interrupted":
			failed++
		}
	}
	if total > 0 {
		successPct = float64(success) / float64(total) * 100
	}
	return total, success, failed, successPct
}

func (h *WebHandler) serveAdminAnalytics(w http.ResponseWriter, r *http.Request) {
	userID, username, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	tab := analyticsTabFromQuery(r)

	data := adminAnalyticsData{
		PageData: h.adminPageData(w, r, userID, username, "analytics", "admin_analytics"),
		Tab:      tab,
	}

	if tab == "overview" || tab == "pcgw" {
		totalBytes, _ := h.store.TotalStorageBytes(ctx)
		totalUsers, _ := h.store.CountUsers(ctx)
		totalClients, _ := h.store.CountClients(ctx)
		totalSaves, _ := h.store.CountTotalSaves(ctx)
		activeClients, _ := h.store.CountActiveClientsSince(ctx, time.Now().UTC().Add(-24*time.Hour))
		syncVol, _ := h.store.CountSyncVolume7d(ctx)
		manifestGames, _ := h.store.CountDistinctManifestGames(ctx)
		saveGames, _ := h.store.CountDistinctSaveGames(ctx)
		snapshots, _ := h.store.ListStatsSnapshots(ctx, 30)

		coverage := 0.0
		if manifestGames > 0 {
			coverage = float64(saveGames) / float64(manifestGames) * 100
			if coverage > 100 {
				coverage = 100
			}
		}

		data.TotalStorage = totalBytes
		data.TotalUsers = totalUsers
		data.TotalClients = totalClients
		data.TotalSaves = totalSaves
		data.ActiveClients24h = activeClients
		data.SyncVolume7d = syncVol
		data.ManifestGames = manifestGames
		data.SaveGames = saveGames
		data.PCGWCoveragePct = coverage
		data.StatsSnapshots = snapshots
		data.PCGWStats = h.loadPCGWStats(ctx)

		if tab == "overview" {
			topGames, _ := h.store.ListTopSaveGames(ctx, 8)
			data.TopSaveGames = topGames

			users, _ := h.store.ListUserStats(ctx)
			sort.Slice(users, func(i, j int) bool {
				if users[i].StorageBytes == users[j].StorageBytes {
					return users[i].SaveCount > users[j].SaveCount
				}
				return users[i].StorageBytes > users[j].StorageBytes
			})
			if len(users) > 5 {
				users = users[:5]
			}
			data.TopUsers = users

			// Adoption & reliability panels.
			if vc, err := h.store.ClientVersionCounts(ctx); err == nil {
				data.VersionDist = labelBarsFromCounts(vc)
			}
			if oc, err := h.store.ClientOSCounts(ctx); err == nil {
				data.OSDist = labelBarsFromCounts(oc)
			}
			if ad, err := h.store.UserAdoptionStats(ctx); err == nil {
				data.Adoption = ad
				if ad.Users > 0 {
					data.TOTPPct = ad.TOTPEnabled * 100 / ad.Users
					data.EncPct = ad.EncryptionEnabled * 100 / ad.Users
				}
			}
			if su, err := h.store.SignupsByMonth(ctx, 12); err == nil {
				data.Signups = su
				for _, m := range su {
					data.SignupTotal += m.Count
				}
			}
			data.JobStats, _ = h.store.JobRunStats(ctx)
		}
	}

	if tab == "fleet" {
		days := 30
		switch r.URL.Query().Get("days") {
		case "7":
			days = 7
		case "90":
			days = 90
		}
		data.FleetWindowDays = days
		if v, err := h.store.SyncVolumeByDayAll(ctx, days); err == nil {
			data.FleetVolume = v
			for _, d := range v {
				data.FleetVolumeTotal += d.Count
			}
		}
		if b, err := h.store.SyncBytesByDay(ctx, "", days); err == nil {
			data.FleetBytes = b
			for _, d := range b {
				data.FleetBytesTotal += d.Bytes
			}
		}
		data.ActiveUsers, _ = h.store.ActiveUsersByDay(ctx, days)
		if ac, err := h.store.AuditActionCounts(ctx, days, 8); err == nil {
			data.AuditActions = labelBarsFromCounts(ac)
		}
		if av, err := h.store.AuditVolumeByDay(ctx, days); err == nil {
			data.AuditByDay = av
			for _, d := range av {
				data.AuditTotal += d.Count
			}
		}
		if mf, err := h.store.ManifestFetchByDay(ctx, days); err == nil {
			data.FetchByDay = mf
			for _, d := range mf {
				data.FetchTotal += d.Count
			}
		}
	}

	if tab == "pcgw" {
		games, q, status, platform, page, per, total := h.loadPCGWPage(ctx, r)
		data.Games = games
		data.Query = q
		data.FilterStatus = status
		data.FilterPlatform = platform
		data.Pager = newPager("/admin/partial/analytics-pcgw", pcgwFilterParams(q, status, platform), page, per, total, "#analytics-pcgw-table", "games")

		data.ManifestMeta, _ = h.store.GetPCGWManifestMeta(ctx)
		data.ManifestSaveLocations, _ = h.store.CountGameSaveLocations(ctx)
		data.ParseFailureCount, _ = h.store.CountPCGWParseFailures(ctx)
		data.ParseFailures, _ = h.store.ListRecentPCGWParseFailures(ctx, 15)
		data.CatalogStats, _ = h.store.GetPCGWCatalogStats(ctx)
		data.LatestSyncRun, _ = h.store.GetLatestPCGWSyncRun(ctx)
	}

	if tab == "sync" {
		data.PCGWStats = h.loadPCGWStats(ctx)
		data.SyncFilterMode = strings.TrimSpace(r.URL.Query().Get("mode"))
		data.SyncFilterStatus = strings.TrimSpace(r.URL.Query().Get("status"))

		runs, _ := h.store.ListPCGWSyncRuns(ctx, 100)
		filtered := filterPCGWSyncRuns(runs, data.SyncFilterMode, data.SyncFilterStatus)
		if len(filtered) > 50 {
			filtered = filtered[:50]
		}
		data.PCGWSyncRuns = filtered
		data.SyncRunsTotal, data.SyncRunsSuccess, data.SyncRunsFailed, data.SyncRunsSuccessPct = syncRunSummary(runs)
	}

	h.render(w, "admin_analytics.html", data)
}

func (h *WebHandler) serveAdminAnalyticsPCGWPartial(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	games, q, status, platform, page, per, total := h.loadPCGWPage(r.Context(), r)
	pager := newPager("/admin/partial/analytics-pcgw", pcgwFilterParams(q, status, platform), page, per, total, "#analytics-pcgw-table", "games")
	h.renderPartial(w, "partials/admin_analytics_pcgw_table.html", h.pcgwTableViewData(games, q, status, platform, pager))
}
