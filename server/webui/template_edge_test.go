package webui

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/gsbs/gsbs/server/store"
)

func TestAdminJobsPartialAfterSync(t *testing.T) {
	tmpl := parseTemplates()
	now := time.Now().UTC().Format(time.RFC3339)
	cases := []map[string]interface{}{
		{
			"RecentJobs": []store.JobRun{{
				JobName: "pcgw_sync", StartedAt: now, FinishedAt: now,
				Status: "partial", EntriesCount: 5000,
				ErrorMessage: "some games failed: {{invalid template-ish}}",
			}},
			"JobRunning": false, "JobProgressPages": 0, "JobProgressTotal": 10000,
			"JobGamesSkipped": 42, "LastSuccessfulSyncAt": now, "CSRFToken": "csrf",
		},
		{
			"RecentJobs": []store.JobRun{{Status: "success", StartedAt: now, FinishedAt: now}},
			"JobRunning": true, "JobProgressPages": 9000, "JobProgressTotal": 10000,
			"JobGamesSkipped": 3, "CSRFToken": "csrf",
		},
	}
	for i, data := range cases {
		var buf bytes.Buffer
		if err := tmpl.ExecuteTemplate(&buf, "partials/admin_jobs.html", data); err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
	}
}

func TestAdminPCGWJobStatusEmbedded(t *testing.T) {
	tmpl := parseTemplates()
	data := adminPCGWData{
		PageData:         PageData{CSRFToken: "csrf", PageName: "admin_pcgw"},
		JobRunning:       true,
		JobProgress:      100,
		JobProgressTotal: 500,
		JobGamesSkipped:  2,
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "partials/admin_pcgw_job_status.html", data); err != nil {
		t.Fatalf("embedded pcgw job status: %v", err)
	}
	if err := tmpl.ExecuteTemplate(&buf, "admin_pcgw.html", data); err != nil {
		t.Fatalf("admin_pcgw page during sync: %v", err)
	}
}

func TestAdminPCGWActionLabelsClarifyPhases(t *testing.T) {
	tmpl := parseTemplates()
	data := map[string]interface{}{
		"JobRunning":     false,
		"CSRFToken":      "csrf",
		"CapStatusText":  "Phase 2 parse/store cap: 5000 pages per run (default). Phase 1 catalog scan always fetches all IDs.",
		"LatestSyncRun":  nil,
		"CatalogStats":   map[string]int{"RemoteTotal": 0, "LocalTotal": 0, "MissingLocal": 0, "DeadLetter": 0},
		"MaxPagesPerRun": 5000,
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "partials/admin_pcgw_actions.html", data); err != nil {
		t.Fatalf("render pcgw actions: %v", err)
	}
	html := buf.String()
	want := []string{
		"Phase 1+2: Refresh IDs and Parse/Store Backlog",
		"Phase 1 Only: Refresh Catalog IDs",
		"Phase 2: Parse/Store Missing Pages Only",
		"Phase 2: Retry Failed/Partial Pages",
		"Fetches the remote ID catalog only; does not parse/store page detail.",
	}
	for _, needle := range want {
		if !strings.Contains(html, needle) {
			t.Fatalf("expected rendered actions to include %q", needle)
		}
	}
}
