package webui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gsbs/gsbs/pkg/pcgw"
	"github.com/gsbs/gsbs/pkg/types"
	"github.com/gsbs/gsbs/server/job"
	"github.com/gsbs/gsbs/server/logx"
	"github.com/gsbs/gsbs/server/sse"
	"github.com/gsbs/gsbs/server/store"
)

type adminPCGWData struct {
	PageData
	Stats            PCGWStatsView
	CatalogStats     types.PCGWCatalogStats
	LatestSyncRun    *types.PCGWSyncRun
	ResumableSyncRun *types.PCGWSyncRun
	Games            []types.PCGWGame
	Query            string
	FilterStatus     string
	FilterPlatform   string
	Page             int
	PerPage          int
	Total            int
	TotalPages       int
	Start            int
	End              int
	PrevPage         int
	NextPage         int
	JobRunning       bool
	JobProgress      int
	JobProgressTotal int
	JobGamesSkipped  int
	JobPhase         string
}

type PCGWStatsView struct {
	TotalGames      int
	OK              int
	Partial         int
	Failed          int
	Pending         int
	LastSyncAt      string
	AvgParseMs      int
	DBWikitextBytes int64
	ManifestVersion int
}

type adminPCGWDetailData struct {
	PageData
	Game           *types.PCGWGame
	GameData       []types.PCGWGameData
	Availability   *types.PCGWSectionRow
	Video          *types.PCGWSectionRow
	Input          *types.PCGWSectionRow
	Audio          *types.PCGWSectionRow
	Network        *types.PCGWSectionRow
	Other          *types.PCGWSectionRow
	Notes          *types.PCGWSectionRow
	Metadata       *types.PCGWMetadata
	ParseFailures  []types.PCGWParseFailure
	ExportJSONPath string
}

func (h *WebHandler) loadPCGWStats(ctx context.Context) PCGWStatsView {
	st, _ := h.store.GetPCGWStats(ctx)
	return PCGWStatsView{
		TotalGames: st.TotalGames, OK: st.OK, Partial: st.Partial,
		Failed: st.Failed, Pending: st.Pending, LastSyncAt: st.LastSyncAt,
		AvgParseMs: st.AvgParseMs, DBWikitextBytes: st.DBWikitextBytes,
		ManifestVersion: st.ManifestVersion,
	}
}

func (h *WebHandler) loadPCGWPage(ctx context.Context, r *http.Request) (games []types.PCGWGame, q, status, platform string, page, perPage, total, totalPages, start, end, prevPage, nextPage int) {
	q = strings.TrimSpace(r.URL.Query().Get("q"))
	status = strings.TrimSpace(r.URL.Query().Get("status"))
	platform = strings.TrimSpace(r.URL.Query().Get("platform"))
	page, perPage = parseManifestPagination(r)
	offset := (page - 1) * perPage

	var err error
	if q != "" {
		games, total, err = h.store.SearchPCGWGamesFTS(ctx, q, perPage, offset)
	} else {
		games, total, err = h.store.ListPCGWGames(ctx, store.PCGWGameListFilter{
			ParseStatus: status, Platform: platform, Limit: perPage, Offset: offset,
		})
	}
	if err != nil {
		logx.Logger().Error().Err(err).Str("query", q).Msg("webui admin pcgw list failed")
		return nil, q, status, platform, page, perPage, 0, 1, 0, 0, 0, 2
	}
	totalPages = mathCeilDiv(total, perPage)
	if totalPages < 1 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
	}
	if total > 0 {
		start = offset + 1
		end = offset + len(games)
		if end > total {
			end = total
		}
	}
	prevPage = page - 1
	nextPage = page + 1
	return games, q, status, platform, page, perPage, total, totalPages, start, end, prevPage, nextPage
}

func (h *WebHandler) pcgwTableViewData(games []types.PCGWGame, q, status, platform string, page, perPage, total, totalPages, start, end, prevPage, nextPage int) map[string]interface{} {
	return map[string]interface{}{
		"Games": games, "Query": q, "FilterStatus": status, "FilterPlatform": platform,
		"Page": page, "PerPage": perPage, "Total": total, "TotalPages": totalPages,
		"Start": start, "End": end, "PrevPage": prevPage, "NextPage": nextPage,
	}
}

func (h *WebHandler) serveAdminPCGW(w http.ResponseWriter, r *http.Request) {
	userID, username, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	games, q, status, platform, page, perPage, total, totalPages, start, end, prevPage, nextPage := h.loadPCGWPage(ctx, r)
	jobs := h.loadJobsViewData(ctx, SetCSRFToken(w, r, h.secret))
	catalogStats, _ := h.store.GetPCGWCatalogStats(ctx)
	latestRun, _ := h.store.GetLatestPCGWSyncRun(ctx)
	resumableRun, _ := h.store.GetResumablePCGWSyncRun(ctx, "incremental")
	h.render(w, "admin_pcgw.html", adminPCGWData{
		PageData:         h.adminPageData(w, r, userID, username, "pcgw", "admin_pcgw"),
		Stats:            h.loadPCGWStats(ctx),
		CatalogStats:     catalogStats,
		LatestSyncRun:    latestRun,
		ResumableSyncRun: resumableRun,
		Games:            games,
		Query:            q,
		FilterStatus:     status,
		FilterPlatform:   platform,
		Page:             page,
		PerPage:          perPage,
		Total:            total,
		TotalPages:       totalPages,
		Start:            start,
		End:              end,
		PrevPage:         prevPage,
		NextPage:         nextPage,
		JobRunning:       jobs.JobRunning,
		JobProgress:      jobs.JobProgressPages,
		JobProgressTotal: jobs.JobProgressTotal,
		JobGamesSkipped:  jobs.JobGamesSkipped,
		JobPhase:         jobs.JobPhase,
	})
}

func (h *WebHandler) serveAdminPCGWPartial(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	games, q, status, platform, page, perPage, total, totalPages, start, end, prevPage, nextPage := h.loadPCGWPage(r.Context(), r)
	h.renderPartial(w, "partials/admin_pcgw_table.html", h.pcgwTableViewData(games, q, status, platform, page, perPage, total, totalPages, start, end, prevPage, nextPage))
}

func (h *WebHandler) serveAdminPCGWJobStatusPartial(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	data := h.loadJobsViewData(r.Context(), SetCSRFToken(w, r, h.secret))
	h.renderPartial(w, "partials/admin_pcgw_job_status.html", map[string]interface{}{
		"JobRunning":       data.JobRunning,
		"JobProgress":      data.JobProgressPages,
		"JobProgressTotal": data.JobProgressTotal,
		"JobGamesSkipped":  data.JobGamesSkipped,
		"CSRFToken":        data.CSRFToken,
	})
}

func (h *WebHandler) serveAdminPCGWDetail(w http.ResponseWriter, r *http.Request, pageID int64) {
	userID, username, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	game, err := h.store.GetPCGWGame(ctx, pageID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	gameData, _ := h.store.ListPCGWGameData(ctx, pageID)
	avail, _ := h.store.GetPCGWSection(ctx, pageID, "availability")
	video, _ := h.store.GetPCGWSection(ctx, pageID, "video")
	input, _ := h.store.GetPCGWSection(ctx, pageID, "input")
	audio, _ := h.store.GetPCGWSection(ctx, pageID, "audio")
	network, _ := h.store.GetPCGWSection(ctx, pageID, "network")
	other, _ := h.store.GetPCGWSection(ctx, pageID, "other")
	notes, _ := h.store.GetPCGWSection(ctx, pageID, "notes")
	meta, _ := h.store.GetPCGWMetadata(ctx, pageID)
	failures, _ := h.store.ListPCGWParseFailures(ctx, pageID, 20)

	h.render(w, "admin_pcgw_detail.html", adminPCGWDetailData{
		PageData:       h.adminPageData(w, r, userID, username, "pcgw", "admin_pcgw_detail"),
		Game:           game,
		GameData:       gameData,
		Availability:   avail,
		Video:          video,
		Input:          input,
		Audio:          audio,
		Network:        network,
		Other:          other,
		Notes:          notes,
		Metadata:       meta,
		ParseFailures:  failures,
		ExportJSONPath: fmt.Sprintf("/admin/pcgw/export/%d.json", pageID),
	})
}

func (h *WebHandler) handleAdminPCGWRefresh(w http.ResponseWriter, r *http.Request, pageID int64) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	userID, username, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	go func() {
		client := pcgw.NewClient()
		_, _ = job.PCGWSyncPage(context.Background(), h.store, client, pageID)
	}()
	h.appendAuditBroadcast(r.Context(), userID, username, "pcgw_refresh", strconv.FormatInt(pageID, 10), "")
	Redirect(w, r, fmt.Sprintf("/admin/pcgw/%d?refreshed=1", pageID))
}

func (h *WebHandler) handleAdminPCGWSync(w http.ResponseWriter, r *http.Request, full bool) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	userID, username, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	if h.jobRunner != nil {
		var started bool
		var err error
		if full {
			started, err = h.jobRunner.RunPCGWSyncFull(context.Background())
		} else {
			started, err = h.jobRunner.RunPCGWSync(context.Background())
		}
		if err != nil {
			if errors.Is(err, job.ErrJobAlreadyRunning) {
				Redirect(w, r, "/admin/pcgw?error=job_already_running")
				return
			}
			Redirect(w, r, "/admin/pcgw?error=job_start_failed")
			return
		}
		if !started {
			Redirect(w, r, "/admin/pcgw?error=job_already_running")
			return
		}
	}
	action := "pcgw_sync"
	if full {
		action = "pcgw_sync_full"
	}
	h.appendAuditBroadcast(r.Context(), userID, username, "run_job", action, "")
	Redirect(w, r, "/admin/pcgw?job_started=1")
}

func (h *WebHandler) handleAdminPCGWPurgeWikitext(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	userID, username, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	n, _ := h.store.PurgePCGWFullWikitext(r.Context())
	h.appendAuditBroadcast(r.Context(), userID, username, "pcgw_purge_wikitext", "", fmt.Sprintf("rows=%d", n))
	Redirect(w, r, fmt.Sprintf("/admin/pcgw?purged=%d", n))
}

func (h *WebHandler) serveAdminPCGWExport(w http.ResponseWriter, r *http.Request, pageID int64) {
	if _, _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	data, err := h.store.ExportPCGWGameJSON(r.Context(), pageID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="pcgw-%d.json"`, pageID))
	_, _ = w.Write(data)
}

func (h *WebHandler) serveAdminPCGWExportBundle(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	data, err := h.store.ExportPCGWManifestBundle(r.Context(), h.gsbsVersion)
	if err != nil {
		logx.Logger().Error().Err(err).Msg("pcgw export bundle failed")
		http.Error(w, "Export failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="gsbs-pcgw-manifest.json.gz"`)
	_, _ = w.Write(data)
}

func (h *WebHandler) handleAdminPCGWImport(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	userID, username, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	if err := r.ParseMultipartForm(256 << 20); err != nil {
		Redirect(w, r, "/admin/pcgw?error=import_parse_failed")
		return
	}
	mode := strings.TrimSpace(r.FormValue("mode"))
	if mode != "merge" && mode != "full_replace" {
		Redirect(w, r, "/admin/pcgw?error=import_invalid_mode")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		Redirect(w, r, "/admin/pcgw?error=import_missing_file")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 256<<20))
	if err != nil {
		Redirect(w, r, "/admin/pcgw?error=import_read_failed")
		return
	}

	result, err := h.store.ImportPCGWManifestBundle(r.Context(), data, mode)
	if err != nil {
		logx.Logger().Error().Err(err).Msg("pcgw import bundle failed")
		Redirect(w, r, "/admin/pcgw?error=import_failed")
		return
	}

	validation, _ := h.store.ValidatePCGWImport(r.Context())
	details := fmt.Sprintf("mode=%s locations=%d games=%d validation_ok=%v",
		result.Mode, result.GameSaveLocations, result.PCGWGames, validation.SampleOK)
	h.appendAuditBroadcast(r.Context(), userID, username, "pcgw_import", mode, details)

	if h.apiHandler != nil {
		h.apiHandler.InvalidateManifestCache()
	}
	if h.hub != nil {
		h.hub.Broadcast(sse.Event{Type: "manifest-updated", Data: "{}"})
	}

	Redirect(w, r, fmt.Sprintf("/admin/pcgw?imported=1&locations=%d&games=%d", result.GameSaveLocations, result.PCGWGames))
}

func (h *WebHandler) handleAdminPCGWWipePreflight(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	counts, err := h.store.GetPCGWWipePreflightCounts(r.Context())
	if err != nil {
		http.Error(w, "preflight failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(counts)
}

func (h *WebHandler) handleAdminPCGWWipe(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	userID, username, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	if h.store.HasRunningPCGWSync(r.Context()) {
		Redirect(w, r, "/admin/pcgw?error=sync_running_cannot_wipe")
		return
	}
	mode := strings.TrimSpace(r.FormValue("mode"))
	if mode != "mirror_only" && mode != "mirror_and_manifest" {
		http.Error(w, "Invalid wipe mode.", http.StatusBadRequest)
		return
	}

	// Log wipe as a job_run entry for audit.
	jobRunID, _ := h.store.LogJobStart(r.Context(), "pcgw_wipe")
	var wipeErr error
	if mode == "mirror_only" {
		wipeErr = h.store.WipePCGWMirrorOnly(r.Context())
	} else {
		wipeErr = h.store.WipePCGWMirrorAndManifest(r.Context())
	}
	errMsg := ""
	status := "success"
	if wipeErr != nil {
		errMsg = wipeErr.Error()
		status = "failed"
	}
	_ = h.store.LogJobFinish(r.Context(), jobRunID, status, errMsg, 0)

	if wipeErr != nil {
		Redirect(w, r, "/admin/pcgw?error=wipe_failed")
		return
	}

	if h.apiHandler != nil {
		h.apiHandler.InvalidateManifestCache()
	}
	if h.hub != nil {
		h.hub.Broadcast(sse.Event{Type: "manifest-updated", Data: "{}"})
	}
	h.appendAuditBroadcast(r.Context(), userID, username, "pcgw_wipe", mode, "")
	Redirect(w, r, "/admin/pcgw?wiped=1")
}

func (h *WebHandler) handleAdminPCGWSyncCatalogOnly(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	userID, username, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	if h.jobRunner != nil {
		started, err := h.jobRunner.RunPCGWSyncCatalogOnly(context.Background())
		if err != nil || !started {
			Redirect(w, r, "/admin/pcgw?error=job_already_running")
			return
		}
	}
	h.appendAuditBroadcast(r.Context(), userID, username, "run_job", "pcgw_catalog_only", "")
	Redirect(w, r, "/admin/pcgw?job_started=1")
}

func (h *WebHandler) handleAdminPCGWSyncRetryFailed(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	userID, username, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	if h.jobRunner != nil {
		started, err := h.jobRunner.RunPCGWSyncRetryFailed(context.Background())
		if err != nil || !started {
			Redirect(w, r, "/admin/pcgw?error=job_already_running")
			return
		}
	}
	h.appendAuditBroadcast(r.Context(), userID, username, "run_job", "pcgw_retry_failed", "")
	Redirect(w, r, "/admin/pcgw?job_started=1")
}

func (h *WebHandler) handleAdminPCGWRebuildManifest(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	userID, username, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	if h.jobRunner != nil {
		started, err := h.jobRunner.RunPCGWSyncRebuildManifest(context.Background())
		if err != nil || !started {
			Redirect(w, r, "/admin/pcgw?error=job_already_running")
			return
		}
	}
	h.appendAuditBroadcast(r.Context(), userID, username, "run_job", "pcgw_rebuild_manifest", "")
	Redirect(w, r, "/admin/pcgw?job_started=1")
}

func (h *WebHandler) routeAdminPCGW(w http.ResponseWriter, r *http.Request) bool {
	path := r.URL.Path
	if path == "/admin/pcgw" && r.Method == http.MethodGet {
		h.serveAdminPCGW(w, r)
		return true
	}
	if path == "/admin/partial/pcgw" && r.Method == http.MethodGet {
		h.serveAdminPCGWPartial(w, r)
		return true
	}
	if path == "/admin/partial/pcgw-job-status" && r.Method == http.MethodGet {
		h.serveAdminPCGWJobStatusPartial(w, r)
		return true
	}
	if path == "/admin/pcgw/sync" && r.Method == http.MethodPost {
		h.handleAdminPCGWSync(w, r, r.FormValue("full") == "1")
		return true
	}
	if path == "/admin/pcgw/sync/catalog-only" && r.Method == http.MethodPost {
		h.handleAdminPCGWSyncCatalogOnly(w, r)
		return true
	}
	if path == "/admin/pcgw/sync/retry-failed" && r.Method == http.MethodPost {
		h.handleAdminPCGWSyncRetryFailed(w, r)
		return true
	}
	if path == "/admin/pcgw/rebuild-manifest" && r.Method == http.MethodPost {
		h.handleAdminPCGWRebuildManifest(w, r)
		return true
	}
	if path == "/admin/pcgw/wipe-preflight" && r.Method == http.MethodGet {
		h.handleAdminPCGWWipePreflight(w, r)
		return true
	}
	if path == "/admin/pcgw/wipe" && r.Method == http.MethodPost {
		h.handleAdminPCGWWipe(w, r)
		return true
	}
	if path == "/admin/pcgw/purge-wikitext" && r.Method == http.MethodPost {
		h.handleAdminPCGWPurgeWikitext(w, r)
		return true
	}
	if path == "/admin/pcgw/export/manifest.json.gz" && r.Method == http.MethodGet {
		h.serveAdminPCGWExportBundle(w, r)
		return true
	}
	if path == "/admin/pcgw/import" && r.Method == http.MethodPost {
		h.handleAdminPCGWImport(w, r)
		return true
	}
	if strings.HasPrefix(path, "/admin/pcgw/export/") && strings.HasSuffix(path, ".json") && r.Method == http.MethodGet {
		idStr := strings.TrimSuffix(strings.TrimPrefix(path, "/admin/pcgw/export/"), ".json")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			http.NotFound(w, r)
			return true
		}
		h.serveAdminPCGWExport(w, r, id)
		return true
	}
	if strings.HasPrefix(path, "/admin/pcgw/") && r.Method == http.MethodGet {
		idStr := strings.TrimPrefix(path, "/admin/pcgw/")
		if id, err := strconv.ParseInt(idStr, 10, 64); err == nil {
			h.serveAdminPCGWDetail(w, r, id)
			return true
		}
	}
	if strings.HasPrefix(path, "/admin/pcgw/") && strings.HasSuffix(path, "/refresh") && r.Method == http.MethodPost {
		idStr := strings.TrimSuffix(strings.TrimPrefix(path, "/admin/pcgw/"), "/refresh")
		if id, err := strconv.ParseInt(idStr, 10, 64); err == nil {
			h.handleAdminPCGWRefresh(w, r, id)
			return true
		}
	}
	return false
}
