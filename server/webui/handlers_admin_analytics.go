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
	Page                  int
	PerPage               int
	Total                 int
	TotalPages            int
	Start                 int
	End                   int
	PrevPage              int
	NextPage              int

	// Sync tab
	PCGWSyncRuns       []types.PCGWSyncRun
	SyncFilterMode     string
	SyncFilterStatus   string
	SyncRunsTotal      int
	SyncRunsSuccess    int
	SyncRunsFailed     int
	SyncRunsSuccessPct float64
}

func analyticsTabFromQuery(r *http.Request) string {
	switch strings.TrimSpace(r.URL.Query().Get("tab")) {
	case "pcgw", "sync", "overview":
		return strings.TrimSpace(r.URL.Query().Get("tab"))
	default:
		return "overview"
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
		}
	}

	if tab == "pcgw" {
		games, q, status, platform, page, perPage, total, totalPages, start, end, prevPage, nextPage := h.loadPCGWPage(ctx, r)
		data.Games = games
		data.Query = q
		data.FilterStatus = status
		data.FilterPlatform = platform
		data.Page = page
		data.PerPage = perPage
		data.Total = total
		data.TotalPages = totalPages
		data.Start = start
		data.End = end
		data.PrevPage = prevPage
		data.NextPage = nextPage

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
	games, q, status, platform, page, perPage, total, totalPages, start, end, prevPage, nextPage := h.loadPCGWPage(r.Context(), r)
	data := map[string]interface{}{
		"Games": games, "Query": q, "FilterStatus": status, "FilterPlatform": platform,
		"Page": page, "PerPage": perPage, "Total": total, "TotalPages": totalPages,
		"Start": start, "End": end, "PrevPage": prevPage, "NextPage": nextPage,
	}
	h.renderPartial(w, "partials/admin_analytics_pcgw_table.html", data)
}
