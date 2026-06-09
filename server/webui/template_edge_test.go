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
			"IdleRunsNeeded": 11, "IdleTotalETASec": 3600, "IdlePerRunETASec": 330,
			"CatalogStats": map[string]int{"RemoteTotal": 55000, "LocalTotal": 3945, "MissingLocal": 50816, "DeadLetter": 0},
		},
		{
			"RecentJobs": []store.JobRun{{Status: "success", StartedAt: now, FinishedAt: now}},
			"JobRunning": true, "JobProgressPages": 9000, "JobProgressTotal": 10000,
			"JobGamesSkipped": 3, "CSRFToken": "csrf",
			"JobPhaseLabel": "Phase 2: Parsing game data", "JobETAMin": 5,
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
		PageData:          PageData{CSRFToken: "csrf", PageName: "admin_pcgw"},
		JobRunning:        true,
		JobProgressPages:  100,
		JobProgressTotal:  500,
		JobGamesSkipped:   2,
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
		"Incremental Sync",
		"Refresh New Games",
		"Auto Catch-Up",
		"Parse Missing Only",
		"Retry Failed Pages",
		"Refresh Catalog Only",
		"Full Reparse",
	}
	for _, needle := range want {
		if !strings.Contains(html, needle) {
			t.Fatalf("expected rendered actions to include %q", needle)
		}
	}
}
