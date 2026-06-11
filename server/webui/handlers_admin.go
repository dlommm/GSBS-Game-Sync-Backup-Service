package webui

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/gsbs/gsbs/pkg/types"
	"github.com/gsbs/gsbs/server/job"
	"github.com/gsbs/gsbs/server/logx"
	"github.com/gsbs/gsbs/server/sse"
	"github.com/gsbs/gsbs/server/store"
)

func (h *WebHandler) loadAdminStats(ctx context.Context) adminStats {
	userCount, _ := h.store.CountUsers(ctx)
	clientCount, _ := h.store.CountClients(ctx)
	saveCount, _ := h.store.CountSaves(ctx)
	manifestCount, _ := h.store.CountGameSaveLocations(ctx)
	totalBytes, _ := h.store.TotalStorageBytes(ctx)
	return adminStats{
		UserCount: userCount, ClientCount: clientCount, SaveCount: saveCount,
		ManifestCount: manifestCount, TotalBytes: totalBytes,
	}
}

func (h *WebHandler) adminPageData(w http.ResponseWriter, r *http.Request, userID, username, adminNav, pageName string) PageData {
	return PageData{
		PageName: pageName, Username: username, IsAdmin: true, CSRFToken: SetCSRFToken(w, r, h.secret),
		NavActive: "admin-" + adminNav, AdminNav: adminNav,
		Success: adminQuerySuccess(r), Error: adminQueryError(r),
	}
}

func (h *WebHandler) jobStatus() (running bool, progressPages int) {
	if h.jobRunner != nil {
		return h.jobRunner.IsRunning("pcgw_sync"), h.jobRunner.ProgressPages("pcgw_sync")
	}
	return false, 0
}

type jobsViewData struct {
	RecentJobs            []store.JobRun
	CatalogStats          types.PCGWCatalogStats
	LatestSyncRun         *types.PCGWSyncRun
	JobRunning            bool
	JobProgressPages      int
	JobProgressTotal      int
	JobGamesSkipped       int
	JobPhase              string
	JobAutoCatchUp        bool
	LastSuccessfulSyncAt  string
	MaxPagesPerRun        int
	MaxPagesPerRunFromEnv bool
	MaxPagesPerRunSource  string
	CapReached            bool
	CapStatusText         string
	ShowPCGWControls      bool
	CSRFToken             string
	ResumableSyncRun      *types.PCGWSyncRun
	JobElapsedSec         int
	JobPagesPerSec        float64
	JobETAMin             int
	JobETASec             int    // ETA in seconds from runner; -1 = unknown
	JobCatalogScanMode    string // catalog_scan_mode from LatestSyncRun ("full", "fast_probe", "tail", "skipped")
	JobPhaseLabel         string
	AvgHistPagesPerSec    float64
	// Idle ETA: how long to clear the remaining backlog when no job is running
	IdleRunsNeeded   int
	IdleTotalETASec  int
	IdlePerRunETASec int
}

func (h *WebHandler) loadJobsViewData(ctx context.Context, csrf string, showPCGWControls bool) jobsViewData {
	recentJobs, _ := h.store.ListJobRuns(ctx, "pcgw_sync", 10)
	jobRunning, jobProgress := h.jobStatus()
	catalogStats, _ := h.store.GetPCGWCatalogStats(ctx)
	latestRun, _ := h.store.GetLatestPCGWSyncRun(ctx)
	maxPagesPerRun, maxPagesSource := job.MaxPagesPerRunWithSource()
	data := jobsViewData{
		RecentJobs:            recentJobs,
		CatalogStats:          catalogStats,
		LatestSyncRun:         latestRun,
		JobRunning:            jobRunning,
		JobProgressPages:      jobProgress,
		MaxPagesPerRun:        maxPagesPerRun,
		MaxPagesPerRunFromEnv: maxPagesSource == job.MaxPagesPerRunSourceEnv,
		MaxPagesPerRunSource:  maxPagesSource,
		ShowPCGWControls:      showPCGWControls,
		CSRFToken:             csrf,
	}
	data.CapStatusText = fmt.Sprintf("Phase 2 parse/store cap: %d pages per run (%s). Phase 1 uses a fast catalog probe on incremental syncs; full scan only when needed.", data.MaxPagesPerRun, data.MaxPagesPerRunSource)
	if last, _ := h.store.GetLatestSuccessfulJobRun(ctx, "pcgw_sync"); last != nil {
		data.LastSuccessfulSyncAt = last.FinishedAt
		if data.LastSuccessfulSyncAt == "" {
			data.LastSuccessfulSyncAt = last.StartedAt
		}
	}
	if data.LatestSyncRun != nil {
		if data.LatestSyncRun.GamesTotal > 0 {
			data.JobProgressTotal = data.LatestSyncRun.GamesTotal
		}
		data.JobCatalogScanMode = data.LatestSyncRun.CatalogScanMode
		data.JobGamesSkipped = data.LatestSyncRun.GamesSkipped
		if jobRunning && jobProgress == 0 && data.LatestSyncRun.CheckpointOffset > 0 {
			data.JobProgressPages = data.LatestSyncRun.CheckpointOffset
		}
		errLower := strings.ToLower(strings.TrimSpace(data.LatestSyncRun.ErrorMessage))
		data.CapReached = strings.Contains(errLower, "budget exhausted")
		if !data.CapReached && data.MaxPagesPerRun > 0 && data.LatestSyncRun.Status == "interrupted" && data.LatestSyncRun.TargetedProcessed >= data.MaxPagesPerRun {
			data.CapReached = true
		}
		if data.CapReached {
			remaining := data.LatestSyncRun.TargetedQueueSize - data.LatestSyncRun.TargetedProcessed
			if remaining < 0 {
				remaining = 0
			}
			if remaining > 0 {
				data.CapStatusText = fmt.Sprintf("Last Phase 2 run hit the cap (%d pages from %s). About %d queued pages remained for follow-up runs.", data.MaxPagesPerRun, data.MaxPagesPerRunSource, remaining)
			} else {
				data.CapStatusText = fmt.Sprintf("Last Phase 2 run hit the cap (%d pages from %s). Run parse/store again to continue from current backlog.", data.MaxPagesPerRun, data.MaxPagesPerRunSource)
			}
		}
	}
	if h.jobRunner != nil && h.jobRunner.IsRunning("pcgw_sync") {
		data.JobPhase = h.jobRunner.ProgressPhase()
		data.JobAutoCatchUp = h.jobRunner.ProgressMode() == "auto-catch-up"
		// Prefer live runner total over stale DB value.
		if t := h.jobRunner.ProgressTotal(); t > 0 {
			data.JobProgressTotal = t
		} else if data.JobProgressTotal == 0 && data.CatalogStats.RemoteTotal > 0 {
			data.JobProgressTotal = data.CatalogStats.RemoteTotal
		}
	}
	data.ResumableSyncRun, _ = h.store.GetResumablePCGWSyncRun(ctx, "incremental")
	// Elapsed time and current throughput
	if data.JobRunning && data.LatestSyncRun != nil {
		startedAt, err := time.Parse(time.RFC3339, data.LatestSyncRun.StartedAt)
		if err == nil {
			elapsed := time.Since(startedAt)
			data.JobElapsedSec = int(elapsed.Seconds())
			if elapsed.Seconds() > 5 && data.JobProgressPages > 0 {
				data.JobPagesPerSec = float64(data.JobProgressPages) / elapsed.Seconds()
			}
		}
	}
	// Historical average from last 3 successful runs
	var totalRate float64
	var rateCount int
	for _, r := range data.RecentJobs {
		if r.Status == "success" && r.EntriesCount > 0 && r.FinishedAt != "" && r.StartedAt != "" {
			s, err1 := time.Parse(time.RFC3339, r.StartedAt)
			f, err2 := time.Parse(time.RFC3339, r.FinishedAt)
			if err1 == nil && err2 == nil && f.After(s) {
				dur := f.Sub(s).Seconds()
				if dur > 0 {
					totalRate += float64(r.EntriesCount) / dur
					rateCount++
					if rateCount >= 3 {
						break
					}
				}
			}
		}
	}
	if rateCount > 0 {
		data.AvgHistPagesPerSec = totalRate / float64(rateCount)
	}
	// ETA calculation — prefer live runner ETA, fall back to rate-based computation.
	data.JobETASec = -1
	data.JobETAMin = -1
	if data.JobRunning {
		// Try live runner ETA first.
		if h.jobRunner != nil {
			if etaSec := h.jobRunner.ProgressETASec(); etaSec >= 0 {
				data.JobETASec = etaSec
				data.JobETAMin = int(math.Ceil(float64(etaSec) / 60))
			}
		}
		// Fall back to rate-based ETA when runner hasn't warmed up yet.
		if data.JobETASec < 0 && data.JobProgressTotal > 0 {
			remaining := data.JobProgressTotal - data.JobProgressPages
			if remaining > 0 {
				rate := data.JobPagesPerSec
				if rate < 0.01 && data.AvgHistPagesPerSec > 0 {
					rate = data.AvgHistPagesPerSec
				}
				if rate > 0.01 {
					etaSec := float64(remaining) / rate
					data.JobETASec = int(math.Ceil(etaSec))
					data.JobETAMin = int(math.Ceil(etaSec / 60))
				}
			} else {
				data.JobETASec = 0
				data.JobETAMin = 0
			}
		}
	}
	// Idle ETA: estimate time to clear MissingLocal backlog (shown when no job is running)
	if !data.JobRunning && data.CatalogStats.MissingLocal > 0 {
		remaining := data.CatalogStats.MissingLocal
		budget := data.MaxPagesPerRun
		if budget <= 0 {
			budget = 5000
		}
		data.IdleRunsNeeded = int(math.Ceil(float64(remaining) / float64(budget)))
		if data.AvgHistPagesPerSec > 0.01 {
			data.IdleTotalETASec = int(float64(remaining) / data.AvgHistPagesPerSec)
			perRunPages := remaining
			if budget < perRunPages {
				perRunPages = budget
			}
			data.IdlePerRunETASec = int(float64(perRunPages) / data.AvgHistPagesPerSec)
		}
	}

	// Human-readable phase label
	switch data.JobPhase {
	case "listing", "catalog":
		data.JobPhaseLabel = "Phase 1: Listing game catalog"
	case "queueing":
		data.JobPhaseLabel = "Phase 2: Building queue"
	case "parsing", "ingest":
		data.JobPhaseLabel = "Phase 2: Parsing game data"
	default:
		if data.JobPhase != "" {
			data.JobPhaseLabel = "Phase: " + data.JobPhase
		} else {
			data.JobPhaseLabel = "Starting\u2026"
		}
	}
	return data
}

func (h *WebHandler) serveAdminOverview(w http.ResponseWriter, r *http.Request) {
	userID, username, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	statsSnapshots, _ := h.store.ListStatsSnapshots(ctx, 30)
	recentJobs, _ := h.store.ListJobRuns(ctx, "pcgw_sync", 10)
	jobsData := h.loadJobsViewData(ctx, SetCSRFToken(w, r, h.secret), false)
	sseClients := 0
	if h.hub != nil {
		sseClients = h.hub.Count()
	}
	stats := h.loadAdminStats(ctx)
	showGettingStarted := stats.UserCount <= 1 && stats.ClientCount == 0 && stats.SaveCount == 0
	h.render(w, "admin_overview.html", adminOverviewData{
		PageData:              h.adminPageData(w, r, userID, username, "overview", "admin_overview"),
		Stats:                 stats,
		StatsSnapshots:        statsSnapshots,
		SSEClients:            sseClients,
		AllowRegister:         h.allowRegister,
		ShowGettingStarted:    showGettingStarted,
		MaxStorageBytes:       h.maxStorageBytes,
		ReadOnly:              h.readOnly,
		RecentJobs:            recentJobs,
		JobRunning:            jobsData.JobRunning,
		JobProgressPages:      jobsData.JobProgressPages,
		JobProgressTotal:      jobsData.JobProgressTotal,
		JobGamesSkipped:       jobsData.JobGamesSkipped,
		JobPhase:              jobsData.JobPhase,
		JobAutoCatchUp:        jobsData.JobAutoCatchUp,
		LastSuccessfulSyncAt:  jobsData.LastSuccessfulSyncAt,
		MaxPagesPerRun:        jobsData.MaxPagesPerRun,
		MaxPagesPerRunFromEnv: jobsData.MaxPagesPerRunFromEnv,
		MaxPagesPerRunSource:  jobsData.MaxPagesPerRunSource,
		CapReached:            jobsData.CapReached,
		CapStatusText:         jobsData.CapStatusText,
		ShowPCGWControls:      jobsData.ShowPCGWControls,
		ResumableSyncRun:      jobsData.ResumableSyncRun,
		CatalogStats:          jobsData.CatalogStats,
		JobElapsedSec:         jobsData.JobElapsedSec,
		JobPagesPerSec:        jobsData.JobPagesPerSec,
		JobETAMin:             jobsData.JobETAMin,
		JobETASec:             jobsData.JobETASec,
		JobCatalogScanMode:    jobsData.JobCatalogScanMode,
		JobPhaseLabel:         jobsData.JobPhaseLabel,
		AvgHistPagesPerSec:    jobsData.AvgHistPagesPerSec,
		IdleRunsNeeded:        jobsData.IdleRunsNeeded,
		IdleTotalETASec:       jobsData.IdleTotalETASec,
		IdlePerRunETASec:      jobsData.IdlePerRunETASec,
	})
}

func (h *WebHandler) serveAdminUsers(w http.ResponseWriter, r *http.Request) {
	userID, username, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	users, _ := h.store.ListUserStats(ctx)
	clients, _ := h.store.ListAllClients(ctx)
	h.render(w, "admin_users.html", adminUsersData{
		PageData:       h.adminPageData(w, r, userID, username, "users", "admin_users"),
		CurrentUserID:  userID,
		Users:          users,
		Clients:        clients,
		MaxClientCount: maxClients(users),
	})
}

func parseManifestPagination(r *http.Request) (page, perPage int) {
	perPage = 20
	if n := r.URL.Query().Get("count"); n != "" {
		switch n {
		case "10", "20", "40", "60", "100":
			fmt.Sscanf(n, "%d", &perPage)
		}
	}
	page = 1
	if p := r.URL.Query().Get("page"); p != "" {
		var v int
		if _, err := fmt.Sscanf(p, "%d", &v); err == nil && v >= 1 {
			page = v
		}
	}
	return page, perPage
}

func (h *WebHandler) loadManifestPage(ctx context.Context, r *http.Request) (entries []types.GameSaveLocation, total, page, perPage, totalPages, start, end int) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	page, perPage = parseManifestPagination(r)
	offset := (page - 1) * perPage
	var err error
	entries, total, err = h.store.SearchGameSaveLocations(ctx, query, perPage, offset)
	if err != nil {
		return nil, 0, page, perPage, 1, 0, 0
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
		end = offset + len(entries)
		if end > total {
			end = total
		}
	}
	return entries, total, page, perPage, totalPages, start, end
}

func (h *WebHandler) serveAdminManifest(w http.ResponseWriter, r *http.Request) {
	userID, username, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	entries, total, page, perPage, totalPages, start, end := h.loadManifestPage(r.Context(), r)
	h.render(w, "admin_manifest.html", adminManifestData{
		PageData:           h.adminPageData(w, r, userID, username, "manifest", "admin_manifest"),
		Stats:              h.loadAdminStats(r.Context()),
		Manifest:           entries,
		Query:              r.URL.Query().Get("q"),
		ManifestPage:       page,
		ManifestPerPage:    perPage,
		ManifestTotal:      total,
		ManifestTotalPages: totalPages,
		ManifestStart:      start,
		ManifestEnd:        end,
		ManifestPrevPage:   page - 1,
		ManifestNextPage:   page + 1,
	})
}

func (h *WebHandler) serveAdminManifestPartial(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	entries, total, page, perPage, totalPages, start, end := h.loadManifestPage(r.Context(), r)
	h.renderPartial(w, "partials/admin_manifest_table.html", map[string]interface{}{
		"Manifest": entries, "Query": r.URL.Query().Get("q"),
		"ManifestPage": page, "ManifestPerPage": perPage, "ManifestTotal": total,
		"ManifestTotalPages": totalPages, "ManifestStart": start, "ManifestEnd": end,
		"ManifestPrevPage": page - 1, "ManifestNextPage": page + 1,
	})
}

func (h *WebHandler) serveAdminActivity(w http.ResponseWriter, r *http.Request) {
	userID, username, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	fetches, _ := h.store.ListManifestFetches(ctx, 50)
	auditLog, _ := h.store.ListAuditLog(ctx, 50, "")
	statsSnapshots, _ := h.store.ListStatsSnapshots(ctx, 30)
	recentJobs, _ := h.store.ListJobRuns(ctx, "pcgw_sync", 10)
	jobsData := h.loadJobsViewData(ctx, SetCSRFToken(w, r, h.secret), true)
	h.render(w, "admin_activity.html", adminActivityData{
		PageData:              h.adminPageData(w, r, userID, username, "activity", "admin_activity"),
		Fetches:               fetches,
		AuditLog:              auditLog,
		StatsSnapshots:        statsSnapshots,
		RecentJobs:            recentJobs,
		JobRunning:            jobsData.JobRunning,
		JobProgressPages:      jobsData.JobProgressPages,
		JobProgressTotal:      jobsData.JobProgressTotal,
		JobGamesSkipped:       jobsData.JobGamesSkipped,
		JobPhase:              jobsData.JobPhase,
		JobAutoCatchUp:        jobsData.JobAutoCatchUp,
		LastSuccessfulSyncAt:  jobsData.LastSuccessfulSyncAt,
		CatalogStats:          jobsData.CatalogStats,
		LatestSyncRun:         jobsData.LatestSyncRun,
		MaxPagesPerRun:        jobsData.MaxPagesPerRun,
		MaxPagesPerRunFromEnv: jobsData.MaxPagesPerRunFromEnv,
		MaxPagesPerRunSource:  jobsData.MaxPagesPerRunSource,
		CapReached:            jobsData.CapReached,
		CapStatusText:         jobsData.CapStatusText,
		ShowPCGWControls:      jobsData.ShowPCGWControls,
		ResumableSyncRun:      jobsData.ResumableSyncRun,
		JobElapsedSec:         jobsData.JobElapsedSec,
		JobPagesPerSec:        jobsData.JobPagesPerSec,
		JobETAMin:             jobsData.JobETAMin,
		JobETASec:             jobsData.JobETASec,
		JobCatalogScanMode:    jobsData.JobCatalogScanMode,
		JobPhaseLabel:         jobsData.JobPhaseLabel,
		AvgHistPagesPerSec:    jobsData.AvgHistPagesPerSec,
		IdleRunsNeeded:        jobsData.IdleRunsNeeded,
		IdleTotalETASec:       jobsData.IdleTotalETASec,
		IdlePerRunETASec:      jobsData.IdlePerRunETASec,
	})
}

func (h *WebHandler) serveAdminJobsPartial(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	showPCGWControls := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("context")), "activity")
	data := h.loadJobsViewData(r.Context(), SetCSRFToken(w, r, h.secret), showPCGWControls)
	h.renderPartial(w, "partials/admin_jobs.html", map[string]interface{}{
		"RecentJobs":            data.RecentJobs,
		"CatalogStats":          data.CatalogStats,
		"LatestSyncRun":         data.LatestSyncRun,
		"JobRunning":            data.JobRunning,
		"JobProgressPages":      data.JobProgressPages,
		"JobProgressTotal":      data.JobProgressTotal,
		"JobGamesSkipped":       data.JobGamesSkipped,
		"JobPhase":              data.JobPhase,
		"JobAutoCatchUp":        data.JobAutoCatchUp,
		"LastSuccessfulSyncAt":  data.LastSuccessfulSyncAt,
		"MaxPagesPerRun":        data.MaxPagesPerRun,
		"MaxPagesPerRunFromEnv": data.MaxPagesPerRunFromEnv,
		"MaxPagesPerRunSource":  data.MaxPagesPerRunSource,
		"CapReached":            data.CapReached,
		"CapStatusText":         data.CapStatusText,
		"ShowPCGWControls":      data.ShowPCGWControls,
		"CSRFToken":             data.CSRFToken,
		"ResumableSyncRun":      data.ResumableSyncRun,
		"JobElapsedSec":         data.JobElapsedSec,
		"JobPagesPerSec":        data.JobPagesPerSec,
		"JobETAMin":             data.JobETAMin,
		"JobETASec":             data.JobETASec,
		"JobCatalogScanMode":    data.JobCatalogScanMode,
		"JobPhaseLabel":         data.JobPhaseLabel,
		"AvgHistPagesPerSec":    data.AvgHistPagesPerSec,
	})
}

func (h *WebHandler) handleRevokeClient(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	userID, username, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	clientID := r.FormValue("client_id")
	if clientID == "" {
		Redirect(w, r, "/admin/users?error=missing_client")
		return
	}
	if err := h.store.RegenerateClientToken(r.Context(), clientID); err != nil {
		logx.Logger().Error().Str("client_id", clientID).Err(err).Msg("webui admin revoke failed")
		Redirect(w, r, "/admin/users?error=revoke_failed")
		return
	}
	h.appendAuditBroadcast(r.Context(), userID, username, "revoke_client", clientID, "")
	logx.Logger().Info().Str("client_id", clientID).Str("username", username).Msg("webui admin revoke ok")
	Redirect(w, r, "/admin/users?revoked=1")
}

func (h *WebHandler) handlePushManifest(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	userID, username, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	if h.apiHandler != nil {
		h.apiHandler.InvalidateManifestCache()
	}
	sent := 0
	if h.hub != nil {
		sent = h.hub.Count()
		h.hub.Broadcast(sse.Event{Type: "manifest-updated", Data: "{}"})
		logx.Logger().Info().Int("clients", sent).Msg("webui admin push-manifest broadcast")
	}
	h.appendAuditBroadcast(r.Context(), userID, username, "push_manifest", "", fmt.Sprintf("sent=%d", sent))
	Redirect(w, r, fmt.Sprintf("/admin/manifest?pushed=1&sent=%d", sent))
}

func (h *WebHandler) handleRunJob(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	userID, username, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	redirect := "/admin/activity?job_started=1"
	if h.jobRunner != nil {
		var started bool
		var err error
		if r.FormValue("full") == "1" {
			started, err = h.jobRunner.RunPCGWSyncFull(context.Background())
		} else {
			started, err = h.jobRunner.RunPCGWSync(context.Background())
		}
		if err != nil {
			if errors.Is(err, job.ErrJobAlreadyRunning) {
				Redirect(w, r, "/admin/activity?error=job_already_running")
				return
			}
			Redirect(w, r, "/admin/activity?error=job_start_failed")
			return
		}
		if !started {
			Redirect(w, r, "/admin/activity?error=job_already_running")
			return
		}
	}
	action := "run_job"
	if r.FormValue("full") == "1" {
		action = "pcgw_sync_full"
	}
	h.appendAuditBroadcast(r.Context(), userID, username, action, "pcgw_sync", "")
	logx.Logger().Info().Str("username", username).Str("action", action).Msg("webui admin run-job pcgw_sync triggered")
	Redirect(w, r, redirect)
}

func (h *WebHandler) handleCancelPCGWJob(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	userID, username, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	if h.jobRunner != nil {
		h.jobRunner.CancelPCGWSync(r.Context())
	}
	h.appendAuditBroadcast(r.Context(), userID, username, "cancel_job", "pcgw_sync", "")
	Redirect(w, r, "/admin/activity?job_canceled=1")
}

func (h *WebHandler) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	userID, username, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	newUsername := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	confirm := r.FormValue("confirm_password")
	if newUsername == "" || password == "" {
		Redirect(w, r, "/admin/users?error=missing_credentials")
		return
	}
	if password != confirm {
		Redirect(w, r, "/admin/users?error=password_mismatch")
		return
	}
	if _, err := h.auth.RegisterUser(r.Context(), newUsername, password); err != nil {
		if strings.Contains(err.Error(), "exists") || strings.Contains(err.Error(), "duplicate") {
			Redirect(w, r, "/admin/users?error=username_taken")
			return
		}
		Redirect(w, r, "/admin/users?error=create_user_failed")
		return
	}
	h.appendAuditBroadcast(r.Context(), userID, username, "create_user", newUsername, "")
	Redirect(w, r, "/admin/users?user_created=1")
}

func (h *WebHandler) handleDisableUser(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	userID, username, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	targetID := strings.TrimSpace(r.FormValue("user_id"))
	if targetID == "" {
		Redirect(w, r, "/admin/users?error=missing_user_id")
		return
	}
	if targetID == userID {
		Redirect(w, r, "/admin/users?error=cannot_disable_self")
		return
	}
	if err := h.store.DisableUser(r.Context(), targetID); err != nil {
		Redirect(w, r, "/admin/users?error=disable_failed")
		return
	}
	h.appendAuditBroadcast(r.Context(), userID, username, "disable_user", targetID, "")
	Redirect(w, r, "/admin/users?user_disabled=1")
}

func (h *WebHandler) handleEnableUser(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	userID, username, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	targetID := strings.TrimSpace(r.FormValue("user_id"))
	if targetID == "" {
		Redirect(w, r, "/admin/users?error=missing_user_id")
		return
	}
	if err := h.store.EnableUser(r.Context(), targetID); err != nil {
		Redirect(w, r, "/admin/users?error=enable_failed")
		return
	}
	h.appendAuditBroadcast(r.Context(), userID, username, "enable_user", targetID, "")
	Redirect(w, r, "/admin/users?user_enabled=1")
}

func (h *WebHandler) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	userID, username, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	targetID := strings.TrimSpace(r.FormValue("user_id"))
	if targetID == "" {
		Redirect(w, r, "/admin/users?error=missing_user_id")
		return
	}
	if targetID == userID {
		Redirect(w, r, "/admin/users?error=cannot_delete_self")
		return
	}
	if err := h.store.DeleteUser(r.Context(), targetID); err != nil {
		Redirect(w, r, "/admin/users?error=delete_user_failed")
		return
	}
	h.appendAuditBroadcast(r.Context(), userID, username, "delete_user", targetID, "")
	Redirect(w, r, "/admin/users?user_deleted=1")
}

func (h *WebHandler) handleSetUserQuota(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	userID, username, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	targetID := strings.TrimSpace(r.FormValue("user_id"))
	quotaStr := strings.TrimSpace(r.FormValue("quota_bytes"))
	if targetID == "" {
		Redirect(w, r, "/admin/users?error=missing_user_id")
		return
	}
	var quotaBytes int64
	if quotaStr != "" && quotaStr != "0" {
		if _, err := fmt.Sscanf(quotaStr, "%d", &quotaBytes); err != nil || quotaBytes < 0 {
			Redirect(w, r, "/admin/users?error=invalid_quota")
			return
		}
	}
	if err := h.store.SetUserQuota(r.Context(), targetID, quotaBytes); err != nil {
		Redirect(w, r, "/admin/users?error=quota_failed")
		return
	}
	h.appendAuditBroadcast(r.Context(), userID, username, "set_quota", targetID, fmt.Sprintf("quota=%d", quotaBytes))
	Redirect(w, r, "/admin/users?quota_set=1")
}

func (h *WebHandler) serveManifestCSV(w http.ResponseWriter, r *http.Request) {
	userID, _ := h.getSessionUser(r)
	if userID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	username, _ := h.store.UsernameByID(r.Context(), userID)
	if !h.isAdminUser(r.Context(), userID, username) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	entries, err := h.store.ListGameSaveLocations(r.Context())
	if err != nil {
		http.Error(w, "Failed to load manifest", http.StatusInternalServerError)
		return
	}
	platform := strings.TrimSpace(r.URL.Query().Get("platform"))
	source := strings.TrimSpace(r.URL.Query().Get("source"))
	if platform != "" || source != "" {
		var filtered []types.GameSaveLocation
		for _, e := range entries {
			if platform != "" && e.Platform != platform {
				continue
			}
			if source != "" && e.Source != source {
				continue
			}
			filtered = append(filtered, e)
		}
		entries = filtered
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=\"manifest.csv\"")
	wr := csv.NewWriter(w)
	_ = wr.Write([]string{"game_id", "game_title", "platform", "path_template", "is_config", "source", "updated_at"})
	for _, e := range entries {
		configVal := "false"
		if e.IsConfig {
			configVal = "true"
		}
		_ = wr.Write([]string{e.GameID, e.GameTitle, e.Platform, e.PathTemplate, configVal, e.Source, e.UpdatedAt})
	}
	wr.Flush()
}
