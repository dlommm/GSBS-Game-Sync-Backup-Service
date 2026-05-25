package webui

import (
	"net/http"
	"time"

	"github.com/gsbs/gsbs/server/store"
)

type adminAnalyticsData struct {
	PageData
	TotalStorage       int64
	ActiveClients24h   int
	SyncVolume7d       int
	ManifestGames      int
	SaveGames          int
	PCGWCoveragePct    float64
	StatsSnapshots     []store.StatsSnapshotRow
}

func (h *WebHandler) serveAdminAnalytics(w http.ResponseWriter, r *http.Request) {
	userID, username, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	ctx := r.Context()

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

	h.render(w, "admin_analytics.html", adminAnalyticsData{
		PageData:         h.adminPageData(w, r, userID, username, "analytics", "admin_analytics"),
		TotalStorage:     totalBytes,
		ActiveClients24h: activeClients,
		SyncVolume7d:     syncVol,
		ManifestGames:    manifestGames,
		SaveGames:        saveGames,
		PCGWCoveragePct:  coverage,
		StatsSnapshots:   snapshots,
	})
}
