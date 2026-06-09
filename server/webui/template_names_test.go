package webui

import (
	"bytes"
	"io/fs"
	"testing"
	"time"

	"github.com/gsbs/gsbs/pkg/logview"
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
	"admin_logs.html",
	"admin_pcgw.html",
	"admin_pcgw_detail.html",
	"admin_settings.html",
	"admin_analytics.html",
	"partials/dashboard_stats.html",
	"partials/dashboard_clients.html",
	"partials/dashboard_saves.html",
	"partials/dashboard_activity.html",
	"partials/admin_manifest_table.html",
	"partials/admin_jobs.html",
	"partials/admin_logs_table.html",
	"partials/admin_pcgw_table.html",
	"partials/admin_pcgw_job_status.html",
	"partials/admin_analytics_pcgw_table.html",
	"partials/loading_skeleton.html",
}

// Partials referenced via {{template}} inside other templates (not called directly by handlers).
var nestedTemplateRefs = []string{
	"partials/alerts.html",
	"partials/topbar.html",
	"partials/admin_shell.html",
	"partials/job_status_badge.html",
	"partials/timeline_item.html",
	"partials/loading_skeleton.html",
}

// Page-specific layout blocks referenced via {{template (printf "%s_*" .PageName) .}}.
var pageBlockTemplates = []string{
	"dashboard_title", "dashboard_content", "dashboard_scripts",
	"settings_title", "settings_content",
	"enable_2fa_title", "enable_2fa_content",
	"save_versions_title", "save_versions_content",
	"admin_overview_title", "admin_overview_content", "admin_overview_scripts",
	"admin_users_title", "admin_users_content", "admin_users_scripts",
	"admin_settings_title", "admin_settings_content", "admin_settings_scripts",
	"admin_analytics_title", "admin_analytics_content", "admin_analytics_scripts",
	"admin_manifest_title", "admin_manifest_content", "admin_manifest_scripts",
	"admin_activity_title", "admin_activity_content", "admin_activity_scripts",
	"admin_logs_title", "admin_logs_content", "admin_logs_scripts",
	"admin_pcgw_title", "admin_pcgw_content", "admin_pcgw_scripts",
	"admin_pcgw_detail_title", "admin_pcgw_detail_content", "admin_pcgw_detail_scripts",
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

func TestAdminAnalyticsTabsExecute(t *testing.T) {
	tmpl := parseTemplates()
	now := time.Now().UTC().Format(time.RFC3339)
	base := adminAnalyticsData{
		PageData: PageData{
			PageName: "admin_analytics", Username: "admin", IsAdmin: true, CSRFToken: "csrf-test", AdminNav: "analytics",
		},
		TotalStorage: 2048, ManifestGames: 10, SaveGames: 2, PCGWCoveragePct: 20,
		TotalUsers: 3, TotalClients: 2, TotalSaves: 8, SyncVolume7d: 4,
		TopUsers: []store.UserStatRow{
			{Username: "alice", SaveCount: 5, StorageBytes: 1024, QuotaBytes: 0},
		},
		TopSaveGames: []store.SaveGameStatRow{
			{GameID: "730", GameTitle: "Counter-Strike 2", SaveCount: 3, StorageBytes: 512},
		},
		PCGWStats:             PCGWStatsView{TotalGames: 10, OK: 8, Partial: 1, Failed: 1, Pending: 0, ManifestVersion: 1},
		ManifestMeta:          &types.PCGWManifestMeta{ManifestVersion: 1, ManifestETag: "e"},
		ManifestSaveLocations: 5,
		ParseFailureCount:     1,
		ParseFailures: []store.PCGWParseFailureRow{
			{PCGWParseFailure: types.PCGWParseFailure{PageID: 1, Section: "save", ErrorMessage: "parse err", CreatedAt: now}, GameTitle: "Test"},
		},
		Games: []types.PCGWGame{
			{PageID: 1, Title: "Test Game", ParseStatus: "ok", UpdatedAt: now},
		},
		PCGWSyncRuns: []types.PCGWSyncRun{
			{ID: "r1", Mode: "incremental", Status: "success", StartedAt: now, FinishedAt: now},
		},
		SyncRunsTotal: 1, SyncRunsSuccess: 1, SyncRunsSuccessPct: 100,
	}
	for _, tab := range []string{"overview", "pcgw", "sync"} {
		data := base
		data.Tab = tab
		var buf bytes.Buffer
		if err := tmpl.ExecuteTemplate(&buf, "admin_analytics.html", data); err != nil {
			t.Fatalf("tab %q: %v", tab, err)
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
		{"admin_logs.html", templateTestData("admin_logs.html")},
		{"admin_pcgw.html", templateTestData("admin_pcgw.html")},
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
			RecentJobs: []store.JobRun{
				{JobName: "pcgw_sync", StartedAt: now, FinishedAt: now, Status: "success", EntriesCount: 50},
			},
			JobRunning:           false,
			JobProgressPages:     0,
			JobProgressTotal:     0,
			JobGamesSkipped:      0,
			LastSuccessfulSyncAt: now,
			CapStatusText:        "Phase 2 parse/store cap: 5000 pages per run (default). Phase 1 catalog scan always fetches all IDs.",
		}
	case "admin_users.html":
		return adminUsersData{
			PageData: PageData{
				PageName: "admin_users", Username: "admin", IsAdmin: true, CSRFToken: "csrf-test", AdminNav: "users",
			},
			CurrentUserID:  "user-admin",
			MaxClientCount: 3,
			Users: []store.UserStatRow{
				{ID: "user-1", Username: "alice", ClientCount: 2, SaveCount: 5, StorageBytes: 1024, QuotaBytes: 10737418240},
				{ID: "user-2", Username: "bob", ClientCount: 1, SaveCount: 2, StorageBytes: 512, Disabled: true, QuotaBytes: 5368709120},
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
			Stats: adminStats{ManifestCount: 1},
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
			JobRunning:           false,
			JobProgressPages:     0,
			JobProgressTotal:     0,
			JobGamesSkipped:      0,
			LastSuccessfulSyncAt: now,
			MaxPagesPerRun:       500,
			MaxPagesPerRunSource: "GSBS_PCGW_MAX_PAGES_PER_RUN",
			CapStatusText:        "Phase 2 parse/store cap: 500 pages per run (GSBS_PCGW_MAX_PAGES_PER_RUN). Phase 1 catalog scan always fetches all IDs.",
			ShowPCGWControls:     true,
			ResumableSyncRun:     &types.PCGWSyncRun{ID: "r1", Mode: "incremental", Status: "interrupted", StartedAt: now, CheckpointPhase: "ingest", CheckpointQueueCursor: 42},
			JobElapsedSec:        120,
			JobPagesPerSec:       2.5,
			JobETAMin:            15,
			JobPhaseLabel:        "Phase 2: Parsing game data",
			AvgHistPagesPerSec:   3.0,
		}
	case "admin_logs.html":
		return adminLogsData{
			PageData: PageData{
				PageName: "admin_logs", Username: "admin", IsAdmin: true, CSRFToken: "csrf-test", AdminNav: "logs",
			},
			Entries: []logview.Entry{
				{
					Timestamp: now, Level: "info", Event: "http.request", Component: "http",
					Summary:   "GET /api/manifest → 200 in 12ms from 10.0.0.5",
					Message:   "GET /api/manifest → 200 in 12ms from 10.0.0.5",
					Context:   "component=http request_id=abc123 user_id=user-1",
					Method:    "GET", Path: "/api/manifest", Status: "200", Duration: "12ms",
					RequestID: "abc123", IP: "10.0.0.5", UserID: "user-1",
					Raw:       `{"level":"info","event":"http.request","component":"http","message":"GET /api/manifest 200","method":"GET","path":"/api/manifest","status":200,"duration":12,"ip":"10.0.0.5","request_id":"abc123","user_id":"user-1"}`,
				},
			},
			LogSourcePath:    "/tmp/server.log",
			LogSourceInfo:    "Using GSBS_SERVICE_LOG_PATH.",
			LogSourcePresent: true,
			Query:            logview.Query{Level: "all", Limit: 200, RefreshSeconds: 5, Component: "all", HideHTTPNoise: true},
		}
	case "admin_pcgw.html":
		return adminPCGWData{
			PageData: PageData{
				PageName: "admin_pcgw", Username: "admin", IsAdmin: true, CSRFToken: "csrf-test", AdminNav: "pcgw",
			},
			Stats: PCGWStatsView{TotalGames: 10, OK: 8, Partial: 1, Failed: 1, ManifestVersion: 2},
			Games: []types.PCGWGame{
				{PageID: 123, Title: "Test Game", ParseStatus: "ok", UpdatedAt: now, SteamAppIDs: []string{"730"}, PlatformsPresent: []string{"windows"}},
			},
			Page: 1, PerPage: 20, Total: 1, TotalPages: 1, Start: 1, End: 1, PrevPage: 0, NextPage: 2,
			JobRunning: true, JobProgressPages: 42, JobProgressTotal: 100, JobGamesSkipped: 1,
			CapStatusText: "Phase 2 parse/store cap: 5000 pages per run (default). Phase 1 catalog scan always fetches all IDs.",
		}
	case "admin_pcgw_detail.html":
		return adminPCGWDetailData{
			PageData: PageData{
				PageName: "admin_pcgw_detail", Username: "admin", IsAdmin: true, CSRFToken: "csrf-test", AdminNav: "pcgw",
			},
			Game:           &types.PCGWGame{PageID: 123, Title: "Test Game", ParseStatus: "ok"},
			GameData:       []types.PCGWGameData{{PlatformKey: "windows", PlatformRawLabel: "Windows"}},
			ExportJSONPath: "/admin/pcgw/export/123.json",
		}
	case "admin_settings.html":
		return adminSettingsData{
			PageData: PageData{
				PageName: "admin_settings", Username: "admin", IsAdmin: true, CSRFToken: "csrf-test", AdminNav: "settings",
			},
			PCGWCron:              store.DefaultPCGWCron,
			PCGWCronSource:        "default",
			PCGWTitleExcludesJSON: "[]",
			PCGWPathExcludesJSON:  store.DefaultPCGWPathExcludesJSON,
		}
	case "admin_analytics.html":
		return adminAnalyticsData{
			PageData: PageData{
				PageName: "admin_analytics", Username: "admin", IsAdmin: true, CSRFToken: "csrf-test", AdminNav: "analytics",
			},
			Tab:              "overview",
			TotalStorage:     2048,
			TotalUsers:       2,
			TotalClients:     3,
			TotalSaves:       20,
			ActiveClients24h: 2,
			SyncVolume7d:     15,
			ManifestGames:    100,
			SaveGames:        12,
			PCGWCoveragePct:  12,
			TopUsers: []store.UserStatRow{
				{Username: "admin", SaveCount: 10, StorageBytes: 1800, QuotaBytes: 0},
			},
			TopSaveGames: []store.SaveGameStatRow{
				{GameID: "570", GameTitle: "Dota 2", SaveCount: 4, StorageBytes: 900},
			},
			StatsSnapshots: []store.StatsSnapshotRow{
				{At: now, UserCount: 1, ClientCount: 1, SaveCount: 5, StorageBytes: 1024},
			},
			PCGWStats: PCGWStatsView{
				TotalGames: 500, OK: 400, Partial: 50, Failed: 30, Pending: 20,
				LastSyncAt: now, AvgParseMs: 120, DBWikitextBytes: 1 << 20, ManifestVersion: 3,
			},
			ManifestMeta: &types.PCGWManifestMeta{
				ManifestVersion: 3, ManifestETag: "sha256:abc",
				LastIncrementalAt: now, LastFullSyncAt: now, DBWikitextBytes: 1 << 20,
			},
			ManifestSaveLocations: 1200,
			ParseFailureCount:     7,
			ParseFailures: []store.PCGWParseFailureRow{
				{PCGWParseFailure: types.PCGWParseFailure{PageID: 99, Section: "save", ErrorMessage: "bad wikitext", CreatedAt: now}, GameTitle: "Broken Game"},
			},
			Games: []types.PCGWGame{
				{PageID: 99, Title: "Broken Game", ParseStatus: "failed", UpdatedAt: now},
			},
			PCGWSyncRuns: []types.PCGWSyncRun{
				{
					ID: "run-1", Mode: "incremental", Status: "success",
					StartedAt: now, FinishedAt: now,
					GamesTotal: 100, GamesOK: 95, GamesPartial: 3, GamesFailed: 2, GamesSkipped: 0,
					AvgParseMs: 85,
				},
				{
					ID: "run-2", Mode: "full", Status: "failed",
					StartedAt: now, FinishedAt: now,
					GamesTotal: 50, GamesFailed: 5, ErrorMessage: "rate limited",
				},
			},
			SyncRunsTotal: 2, SyncRunsSuccess: 1, SyncRunsFailed: 1, SyncRunsSuccessPct: 50,
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
			"JobRunning":         false,
			"JobProgressPages":   0,
			"JobProgressTotal":   0,
			"JobGamesSkipped":    0,
			"JobElapsedSec":      0,
			"JobETAMin":          -1,
			"JobPhaseLabel":      "",
			"ResumableSyncRun":   (*types.PCGWSyncRun)(nil),
			"CapStatusText":      "Phase 2 parse/store cap: 5000 pages per run (default). Phase 1 catalog scan always fetches all IDs.",
			"CSRFToken":          "csrf-test",
			"ShowPCGWControls":   false,
			"LastSuccessfulSyncAt": now,
		}
	case "partials/admin_logs_table.html":
		return map[string]interface{}{
			"Entries": []logview.Entry{
				{
					Timestamp: now, Level: "info", Event: "http.request",
					Summary:   "GET /api/manifest → 200 in 12ms from 10.0.0.5",
					Message:   "GET /api/manifest → 200 in 12ms from 10.0.0.5",
					Context:   "request_id=abc123 user_id=user-1",
					Method:    "GET", Path: "/api/manifest", Status: "200", Duration: "12ms",
					RequestID: "abc123", IP: "10.0.0.5", UserID: "user-1",
					Raw:       `{"level":"info","event":"http.request","message":"GET /api/manifest 200","method":"GET","path":"/api/manifest","status":200,"duration":12,"ip":"10.0.0.5","request_id":"abc123","user_id":"user-1"}`,
				},
			},
			"LogSourcePresent": true,
		}
	case "partials/admin_pcgw_table.html":
		return map[string]interface{}{
			"Games": []types.PCGWGame{
				{PageID: 123, Title: "Test Game", ParseStatus: "ok", UpdatedAt: now},
			},
			"Total": 1, "Page": 1, "TotalPages": 1, "Start": 1, "End": 1,
		}
	case "partials/admin_analytics_pcgw_table.html":
		return map[string]interface{}{
			"Games": []types.PCGWGame{
				{PageID: 456, Title: "Analytics Game", ParseStatus: "partial", UpdatedAt: now},
			},
			"Total": 1, "Page": 1, "TotalPages": 1, "Start": 1, "End": 1,
		}
	case "partials/admin_pcgw_job_status.html":
		return map[string]interface{}{
			"JobRunning":       true,
			"JobProgressPages": 42,
			"JobProgressTotal": 100,
			"JobGamesSkipped":  3,
			"JobPhase":         "ingest",
			"CSRFToken":        "csrf-test",
			"JobElapsedSec":    90,
			"JobPagesPerSec":   1.5,
			"JobETAMin":        5,
			"JobPhaseLabel":    "Phase 2: Parsing game data",
			"AvgHistPagesPerSec": 2.0,
		}
	case "partials/loading_skeleton.html":
		return map[string]interface{}{}
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
