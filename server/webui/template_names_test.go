package webui

import (
	"bytes"
	"io/fs"
	"testing"
	"time"

	"github.com/gsbs/gsbs/pkg/types"
	"github.com/gsbs/gsbs/server/store"
)

// All template names passed to render() / renderPartial() in handlers.
var handlerTemplates = []string{
	"login.html",
	"login_totp.html",
	"register.html",
	"dashboard.html",
	"settings.html",
	"enable_2fa.html",
	"save_versions.html",
	"admin_overview.html",
	"admin_users.html",
	"admin_manifest.html",
	"admin_activity.html",
	"partials/dashboard_stats.html",
	"partials/dashboard_clients.html",
	"partials/dashboard_saves.html",
	"partials/dashboard_activity.html",
	"partials/admin_manifest_table.html",
	"partials/admin_jobs.html",
}

// Partials referenced via {{template}} inside other templates (not called directly by handlers).
var nestedTemplateRefs = []string{
	"partials/alerts.html",
	"partials/topbar.html",
	"partials/admin_shell.html",
	"partials/timeline_item.html",
}

// Page-specific layout blocks referenced via {{template (printf "%s_*" .PageName) .}}.
var pageBlockTemplates = []string{
	"dashboard_title", "dashboard_content", "dashboard_scripts",
	"settings_title", "settings_content",
	"enable_2fa_title", "enable_2fa_content",
	"save_versions_title", "save_versions_content",
	"admin_overview_title", "admin_overview_content", "admin_overview_scripts",
	"admin_users_title", "admin_users_content",
	"admin_manifest_title", "admin_manifest_content", "admin_manifest_scripts",
	"admin_activity_title", "admin_activity_content", "admin_activity_scripts",
}

func TestAllHandlerTemplatesExist(t *testing.T) {
	tmpl := parseTemplates()
	names := map[string]bool{}
	for _, tt := range tmpl.Templates() {
		names[tt.Name()] = true
	}
	for _, name := range append(append(handlerTemplates, nestedTemplateRefs...), pageBlockTemplates...) {
		if !names[name] {
			t.Errorf("missing template %q; have %v", name, sortedKeys(names))
		}
	}
}

func TestAllHandlerTemplatesExecute(t *testing.T) {
	tmpl := parseTemplates()
	for _, name := range handlerTemplates {
		data := templateTestData(name)
		var buf bytes.Buffer
		if err := tmpl.ExecuteTemplate(&buf, name, data); err != nil {
			t.Errorf("ExecuteTemplate(%q): %v", name, err)
			continue
		}
		if buf.Len() == 0 {
			t.Errorf("ExecuteTemplate(%q): empty output", name)
		}
	}
}

func TestLayoutReferencesResolve(t *testing.T) {
	tmpl := parseTemplates()
	cases := []struct {
		name string
		data interface{}
	}{
		{"dashboard.html", templateTestData("dashboard.html")},
		{"settings.html", templateTestData("settings.html")},
		{"admin_overview.html", templateTestData("admin_overview.html")},
		{"admin_manifest.html", templateTestData("admin_manifest.html")},
		{"admin_activity.html", templateTestData("admin_activity.html")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := tmpl.ExecuteTemplate(&buf, tc.name, tc.data); err != nil {
				t.Fatalf("ExecuteTemplate(%q): %v", tc.name, err)
			}
			if buf.Len() == 0 {
				t.Fatalf("ExecuteTemplate(%q): empty output", tc.name)
			}
		})
	}
}

func TestNestedPartialsExecute(t *testing.T) {
	tmpl := parseTemplates()
	now := time.Now().UTC().Format(time.RFC3339)

	cases := []struct {
		name string
		data interface{}
	}{
		{"partials/alerts.html", dashboardData{
			PageData: PageData{PageName: "dashboard", Error: "test error", Success: "ok", Restored: true, Deleted: true},
		}},
		{"partials/topbar.html", PageData{
			PageName: "dashboard", Username: "testuser", IsAdmin: true, CSRFToken: "csrf", NavActive: "dashboard",
		}},
		{"partials/admin_shell.html", adminOverviewData{
			PageData: PageData{PageName: "admin_overview", Username: "admin", CSRFToken: "csrf", AdminNav: "overview"},
		}},
		{"partials/timeline_item.html", store.AuditRow{
			At: now, Action: "revoke_client", Details: "client-1",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := tmpl.ExecuteTemplate(&buf, tc.name, tc.data); err != nil {
				t.Fatalf("ExecuteTemplate(%q): %v", tc.name, err)
			}
		})
	}
}

func templateTestData(name string) interface{} {
	now := time.Now().UTC().Format(time.RFC3339)
	pd := PageData{
		PageName:  "dashboard",
		Username:  "testuser",
		IsAdmin:   true,
		CSRFToken: "csrf-test",
		NavActive: "dashboard",
	}
	adminPD := PageData{
		PageName:  "admin_overview",
		Username:  "admin",
		IsAdmin:   true,
		CSRFToken: "csrf-test",
		NavActive: "admin",
		AdminNav:  "overview",
	}

	switch name {
	case "login.html", "login_totp.html", "register.html":
		return map[string]interface{}{
			"AllowRegister": true,
			"CSRFToken":     "csrf-test",
		}
	case "enable_2fa.html":
		return map[string]interface{}{
			"PageName":  "enable_2fa",
			"CSRFToken": "csrf-test",
			"QRDataURL": "data:image/png;base64,test",
			"Secret":    "TESTSECRET",
		}
	case "dashboard.html":
		return dashboardData{
			PageData: PageData{
				PageName: "dashboard", Username: pd.Username, IsAdmin: pd.IsAdmin,
				CSRFToken: pd.CSRFToken, NavActive: pd.NavActive,
			},
			Stats:      dashboardStats{ClientCount: 1, SaveCount: 2, GameCount: 1, TotalBytes: 1024},
			QuotaBytes: 4096,
		}
	case "settings.html":
		return settingsData{
			PageData: PageData{
				PageName: "settings", Username: pd.Username, IsAdmin: pd.IsAdmin,
				CSRFToken: pd.CSRFToken, NavActive: "settings",
			},
			Sessions: []store.SessionRow{{
				ID: "sess-1", CreatedAt: now, LastSeen: now, UserAgent: "TestBrowser/1.0",
			}},
			CurrentSessionID:  "sess-1",
			TOTPEnabled:       false,
			EncryptionEnabled: false,
		}
	case "save_versions.html":
		return saveVersionsData{
			PageData: PageData{
				PageName: "save_versions", Username: pd.Username, IsAdmin: pd.IsAdmin,
				CSRFToken: pd.CSRFToken, NavActive: pd.NavActive,
			},
			GameID:    "game-1",
			PathKey:   "save/main",
			GameTitle: "Test Game",
			Versions: []store.SaveVersionInfo{
				{Version: 1, UpdatedAt: now, SizeBytes: 512},
				{Version: 2, UpdatedAt: now, SizeBytes: 768},
			},
			CurrentVersion: 2,
		}
	case "admin_overview.html":
		return adminOverviewData{
			PageData: adminPD,
			Stats: adminStats{
				UserCount: 2, ClientCount: 3, SaveCount: 10,
				ManifestCount: 100, TotalBytes: 2048,
			},
			StatsSnapshots: []store.StatsSnapshotRow{
				{At: now, UserCount: 1, ClientCount: 1, SaveCount: 5, StorageBytes: 1024},
				{At: now, UserCount: 2, ClientCount: 3, SaveCount: 10, StorageBytes: 2048},
			},
			SSEClients:      1,
			AllowRegister:   true,
			MaxStorageBytes: 0,
			ReadOnly:        false,
		}
	case "admin_users.html":
		return adminUsersData{
			PageData: PageData{
				PageName: "admin_users", Username: "admin", IsAdmin: true, CSRFToken: "csrf-test", AdminNav: "users",
			},
			CurrentUserID:  "user-admin",
			MaxClientCount: 3,
			Users: []store.UserStatRow{
				{ID: "user-1", Username: "alice", ClientCount: 2, SaveCount: 5, StorageBytes: 1024},
				{ID: "user-2", Username: "bob", ClientCount: 1, SaveCount: 2, StorageBytes: 512, Disabled: true},
			},
			Clients: []store.ClientInfoWithUser{
				{ID: "c1", UserID: "user-1", Username: "alice", Name: "PC", OS: "windows", LastSeen: now},
			},
		}
	case "admin_manifest.html":
		return adminManifestData{
			PageData: PageData{
				PageName: "admin_manifest", Username: "admin", IsAdmin: true, CSRFToken: "csrf-test", AdminNav: "manifest",
			},
			Stats:    adminStats{ManifestCount: 1},
			Manifest: []types.GameSaveLocation{
				{
					GameID: "730", GameTitle: "Counter-Strike 2", Platform: "windows",
					PathTemplate: "%USERPROFILE%/save", Source: "pcgw", UpdatedAt: now,
				},
			},
			Query:              "",
			ManifestPage:       1,
			ManifestPerPage:    20,
			ManifestTotal:      1,
			ManifestTotalPages: 1,
			ManifestStart:      1,
			ManifestEnd:        1,
			ManifestPrevPage:   0,
			ManifestNextPage:   2,
		}
	case "admin_activity.html":
		return adminActivityData{
			PageData: PageData{
				PageName: "admin_activity", Username: "admin", IsAdmin: true, CSRFToken: "csrf-test", AdminNav: "activity",
			},
			Fetches: []store.ManifestFetchRow{
				{ClientName: "PC", Username: "alice", EntriesCount: 100, FetchedAt: now},
			},
			AuditLog: []store.AuditRow{
				{At: now, ActorUsername: "admin", Action: "run_job", TargetID: "pcgw_sync"},
			},
			StatsSnapshots: []store.StatsSnapshotRow{
				{At: now, UserCount: 1, ClientCount: 1, SaveCount: 5, StorageBytes: 1024},
			},
			RecentJobs: []store.JobRun{
				{JobName: "pcgw_sync", StartedAt: now, FinishedAt: now, Status: "success", EntriesCount: 50},
			},
			JobRunning:       false,
			JobProgressPages: 0,
		}
	case "partials/dashboard_stats.html":
		return map[string]interface{}{
			"Stats":      dashboardStats{ClientCount: 1, SaveCount: 2, GameCount: 1, TotalBytes: 1024},
			"QuotaBytes": int64(4096),
		}
	case "partials/dashboard_clients.html":
		return map[string]interface{}{
			"Clients": []store.ClientInfo{
				{ID: "c1", Name: "PC", OS: "windows", LastSeen: now},
			},
			"CSRFToken": "csrf-test",
			"ReadOnly":  false,
		}
	case "partials/dashboard_saves.html":
		return map[string]interface{}{
			"Saves": []store.SaveSummary{
				{GameID: "730", PathKey: "save/main", GameTitle: "CS2", SizeBytes: 512, UpdatedAt: now},
			},
			"CSRFToken": "csrf-test",
			"Query":     "",
			"ReadOnly":  false,
		}
	case "partials/dashboard_activity.html":
		return map[string]interface{}{
			"Entries": []store.AuditRow{
				{At: now, Action: "restore_version", Details: "v2"},
			},
		}
	case "partials/admin_manifest_table.html":
		return map[string]interface{}{
			"Manifest": []types.GameSaveLocation{
				{
					GameID: "730", GameTitle: "Counter-Strike 2", Platform: "windows",
					PathTemplate: "%USERPROFILE%/save", Source: "pcgw", UpdatedAt: now,
				},
			},
			"Query":              "",
			"ManifestPage":       1,
			"ManifestPerPage":    20,
			"ManifestTotal":      1,
			"ManifestTotalPages": 1,
			"ManifestStart":      1,
			"ManifestEnd":        1,
			"ManifestPrevPage":   0,
			"ManifestNextPage":   2,
		}
	case "partials/admin_jobs.html":
		return map[string]interface{}{
			"RecentJobs": []store.JobRun{
				{JobName: "pcgw_sync", StartedAt: now, FinishedAt: now, Status: "success", EntriesCount: 50},
			},
			"JobRunning":       false,
			"JobProgressPages": 0,
			"CSRFToken":        "csrf-test",
		}
	default:
		return pd
	}
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		if k != "" {
			out = append(out, k)
		}
	}
	return out
}

func TestListAllTemplateNames(t *testing.T) {
	tmpl := parseTemplates()
	for _, tt := range tmpl.Templates() {
		if tt.Name() != "" {
			t.Logf("template: %q", tt.Name())
		}
	}
	matches, _ := fs.Glob(templatesFS, "templates/*.html")
	t.Logf("embedded top-level: %d", len(matches))
}
