package webui

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/gsbs/gsbs/pkg/pcgw"
	"github.com/gsbs/gsbs/pkg/types"
	"github.com/gsbs/gsbs/server/job"
	"github.com/gsbs/gsbs/server/store"
)

type adminPCGWData struct {
	PageData
	Stats          PCGWStatsView
	Games          []types.PCGWGame
	Query          string
	FilterStatus   string
	FilterPlatform string
	Page           int
	PerPage        int
	Total          int
	TotalPages     int
	JobRunning     bool
	JobProgress    int
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

func (h *WebHandler) serveAdminPCGW(w http.ResponseWriter, r *http.Request) {
	userID, username, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	platform := strings.TrimSpace(r.URL.Query().Get("platform"))
	page, perPage := parseManifestPagination(r)
	offset := (page - 1) * perPage

	var games []types.PCGWGame
	var total int
	var err error
	if q != "" {
		games, total, err = h.store.SearchPCGWGamesFTS(ctx, q, perPage, offset)
	} else {
		games, total, err = h.store.ListPCGWGames(ctx, store.PCGWGameListFilter{
			ParseStatus: status, Platform: platform, Limit: perPage, Offset: offset,
		})
	}
	if err != nil {
		log.Printf("admin pcgw list: %v", err)
	}
	totalPages := mathCeilDiv(total, perPage)
	jobRunning, jobProgress := h.jobStatus()
	h.render(w, "admin_pcgw.html", adminPCGWData{
		PageData:       h.adminPageData(w, r, userID, username, "pcgw", "admin_pcgw"),
		Stats:          h.loadPCGWStats(ctx),
		Games:          games,
		Query:          q,
		FilterStatus:   status,
		FilterPlatform: platform,
		Page:           page,
		PerPage:        perPage,
		Total:          total,
		TotalPages:     totalPages,
		JobRunning:     jobRunning,
		JobProgress:    jobProgress,
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
		if full {
			h.jobRunner.RunPCGWSyncFull(context.Background())
		} else {
			h.jobRunner.RunPCGWSync(context.Background())
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

func (h *WebHandler) routeAdminPCGW(w http.ResponseWriter, r *http.Request) bool {
	path := r.URL.Path
	if path == "/admin/pcgw" && r.Method == http.MethodGet {
		h.serveAdminPCGW(w, r)
		return true
	}
	if path == "/admin/pcgw/sync" && r.Method == http.MethodPost {
		h.handleAdminPCGWSync(w, r, r.FormValue("full") == "1")
		return true
	}
	if path == "/admin/pcgw/purge-wikitext" && r.Method == http.MethodPost {
		h.handleAdminPCGWPurgeWikitext(w, r)
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
