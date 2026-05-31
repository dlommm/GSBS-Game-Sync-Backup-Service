package webui

import (
	"net/http"
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
	ActiveClients24h int
	SyncVolume7d     int
	ManifestGames    int
	SaveGames        int
	PCGWCoveragePct  float64
	StatsSnapshots   []store.StatsSnapshotRow

	// PCGW tab
	PCGWStats              PCGWStatsView
	ManifestMeta           *types.PCGWManifestMeta
	ManifestSaveLocations  int
	ParseFailureCount      int

	// Sync tab
	PCGWSyncRuns []types.PCGWSyncRun
}

func analyticsTabFromQuery(r *http.Request) string {
	switch strings.TrimSpace(r.URL.Query().Get("tab")) {
	case "pcgw", "sync", "overview":
		return strings.TrimSpace(r.URL.Query().Get("tab"))
	default:
		return "overview"
	}
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
		data.ActiveClients24h = activeClients
		data.SyncVolume7d = syncVol
		data.ManifestGames = manifestGames
		data.SaveGames = saveGames
		data.PCGWCoveragePct = coverage
		data.StatsSnapshots = snapshots
	}

	if tab == "pcgw" || tab == "sync" {
		data.PCGWStats = h.loadPCGWStats(ctx)
	}

	if tab == "pcgw" {
		data.ManifestMeta, _ = h.store.GetPCGWManifestMeta(ctx)
		data.ManifestSaveLocations, _ = h.store.CountGameSaveLocations(ctx)
		data.ParseFailureCount, _ = h.store.CountPCGWParseFailures(ctx)
	}

	if tab == "sync" {
		data.PCGWSyncRuns, _ = h.store.ListPCGWSyncRuns(ctx, 50)
	}

	h.render(w, "admin_analytics.html", data)
}
