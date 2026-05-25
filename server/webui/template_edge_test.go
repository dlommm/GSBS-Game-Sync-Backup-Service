package webui

import (
	"bytes"
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
