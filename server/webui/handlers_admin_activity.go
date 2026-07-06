package webui

import (
	"context"
	"encoding/csv"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gsbs/gsbs/server/store"
)

// View structs for the paginated Admin → Activity tables. Each drives one
// partial that HTMX swaps independently (filters live outside the swap region
// so search inputs keep focus).

type auditTableView struct {
	Rows    []store.AuditRow
	Actions []string
	Filter  store.AuditLogFilter
	Pager   pagerView
}

type fetchesTableView struct {
	Rows  []store.ManifestFetchRow
	Pager pagerView
}

type snapshotsTableView struct {
	Rows  []store.StatsSnapshotRow
	Pager pagerView
}

// jobsTableView augments partials/admin_jobs.html with paging and filters.
// The zero value (admin overview) renders the legacy fixed 10-row table.
type jobsTableView struct {
	Rows        []store.JobRun
	Pager       pagerView
	Job         string
	Status      string
	JobNames    []string
	ShowFilters bool
}

// jobStatusOptions are the job_runs.status values offered by the jobs filter.
var jobStatusOptions = []string{"running", "success", "failed", "canceled", "interrupted"}

func (h *WebHandler) buildAuditTableView(ctx context.Context, r *http.Request) auditTableView {
	f := store.AuditLogFilter{
		Action: strings.TrimSpace(r.URL.Query().Get("action")),
		Text:   strings.TrimSpace(r.URL.Query().Get("q")),
	}
	page, per := parsePageParams(r, 25)
	total, _ := h.store.CountAuditLog(ctx, f)
	params := url.Values{}
	if f.Action != "" {
		params.Set("action", f.Action)
	}
	if f.Text != "" {
		params.Set("q", f.Text)
	}
	pager := newPager("/admin/partial/audit", params, page, per, total, "#audit-table-region", "entries")
	rows, _ := h.store.ListAuditLogPage(ctx, f, pager.PerPage, pager.Offset())
	actions, _ := h.store.ListAuditActions(ctx)
	return auditTableView{Rows: rows, Actions: actions, Filter: f, Pager: pager}
}

func (h *WebHandler) buildFetchesTableView(ctx context.Context, r *http.Request) fetchesTableView {
	page, per := parsePageParams(r, 25)
	total, _ := h.store.CountManifestFetches(ctx)
	pager := newPager("/admin/partial/fetches", url.Values{}, page, per, total, "#fetches-table-region", "fetches")
	rows, _ := h.store.ListManifestFetchesPage(ctx, pager.PerPage, pager.Offset())
	return fetchesTableView{Rows: rows, Pager: pager}
}

func (h *WebHandler) buildSnapshotsTableView(ctx context.Context, r *http.Request) snapshotsTableView {
	page, per := parsePageParams(r, 25)
	total, _ := h.store.CountStatsSnapshots(ctx)
	pager := newPager("/admin/partial/snapshots", url.Values{}, page, per, total, "#snapshots-table-region", "snapshots")
	rows, _ := h.store.ListStatsSnapshotsPage(ctx, pager.PerPage, pager.Offset())
	return snapshotsTableView{Rows: rows, Pager: pager}
}

func (h *WebHandler) buildJobsTableView(ctx context.Context, r *http.Request) jobsTableView {
	q := r.URL.Query()
	job := strings.TrimSpace(q.Get("job"))
	status := strings.TrimSpace(q.Get("status"))
	if !validJobStatus(status) {
		status = ""
	}
	page, per := parsePageParams(r, 10)
	total, _ := h.store.CountJobRuns(ctx, job, status)
	params := url.Values{}
	params.Set("context", "activity")
	if job != "" {
		params.Set("job", job)
	}
	if status != "" {
		params.Set("status", status)
	}
	pager := newPager("/admin/partial/jobs", params, page, per, total, "#admin-jobs-panel", "runs")
	rows, _ := h.store.ListJobRunsPage(ctx, job, status, pager.PerPage, pager.Offset())
	names, _ := h.store.ListJobNames(ctx)
	return jobsTableView{
		Rows: rows, Pager: pager, Job: job, Status: status,
		JobNames: names, ShowFilters: true,
	}
}

func validJobStatus(s string) bool {
	if s == "" {
		return true
	}
	for _, v := range jobStatusOptions {
		if s == v {
			return true
		}
	}
	return false
}

func (h *WebHandler) serveAdminAuditPartial(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	h.renderPartial(w, "partials/admin_audit_table.html", h.buildAuditTableView(r.Context(), r))
}

func (h *WebHandler) serveAdminFetchesPartial(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	h.renderPartial(w, "partials/admin_fetches_table.html", h.buildFetchesTableView(r.Context(), r))
}

func (h *WebHandler) serveAdminSnapshotsPartial(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	h.renderPartial(w, "partials/admin_snapshots_table.html", h.buildSnapshotsTableView(r.Context(), r))
}

// serveAdminAuditCSV exports the audit log (current filters, no paging) as CSV.
func (h *WebHandler) serveAdminAuditCSV(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	f := store.AuditLogFilter{
		Action: strings.TrimSpace(r.URL.Query().Get("action")),
		Text:   strings.TrimSpace(r.URL.Query().Get("q")),
	}
	const exportCap = 10000
	rows, err := h.store.ListAuditLogPage(r.Context(), f, exportCap, 0)
	if err != nil {
		http.Error(w, "Failed to load audit log", http.StatusInternalServerError)
		return
	}
	filename := "audit-log-" + time.Now().UTC().Format("2006-01-02T150405") + ".csv"
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"time", "actor", "action", "target", "details"})
	for _, a := range rows {
		_ = cw.Write([]string{a.At, a.ActorUsername, a.Action, a.TargetID, a.Details})
	}
	cw.Flush()
}
