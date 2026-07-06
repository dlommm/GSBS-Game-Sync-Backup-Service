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
	"setup.html",
	"error.html",
	"dashboard.html",
	"settings.html",
	"enable_2fa.html",
	"recovery_codes.html",
	"save_versions.html",
	"dashboard_games.html",
	"game_detail.html",
	"partials/game_cards.html",
	"dashboard_clients.html",
	"partials/clients_list.html",
	"partials/cmdk_results.html",
	"dashboard_analytics.html",
	"admin_user_detail.html",
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
	"partials/admin_audit_table.html",
	"partials/admin_fetches_table.html",
	"partials/admin_snapshots_table.html",
	"partials/admin_pcgw_table.html",
	"partials/admin_pcgw_job_status.html",
	"partials/admin_analytics_pcgw_table.html",
	"partials/loading_skeleton.html",
}

// Partials referenced via {{template}} inside other templates (not called directly by handlers).
var nestedTemplateRefs = []string{
	"partials/alerts.html",
	"partials/topbar.html",
	"partials/sidebar.html",
	"partials/admin_shell.html",
	"partials/job_status_badge.html",
	"partials/timeline_item.html",
	"partials/loading_skeleton.html",
	"partials/game-icon.html",
	"partials/metric-card.html",
	"partials/empty-state.html",
	"partials/insights_body.html",
	"partials/table_pager.html",
}

// Page-specific layout blocks referenced via {{template (printf "%s_*" .PageName) .}}.
var pageBlockTemplates = []string{
	"dashboard_title", "dashboard_content", "dashboard_scripts",
	"settings_title", "settings_content",
	"enable_2fa_title", "enable_2fa_content",
	"recovery_codes_title", "recovery_codes_content", "recovery_codes_scripts",
	"save_versions_title", "save_versions_content",
	"dashboard_games_title", "dashboard_games_content", "dashboard_games_scripts",
	"game_detail_title", "game_detail_content", "game_detail_scripts",
	"dashboard_clients_title", "dashboard_clients_content", "dashboard_clients_scripts",
	"dashboard_analytics_title", "dashboard_analytics_content", "dashboard_analytics_scripts",
	"admin_user_detail_title", "admin_user_detail_content",
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
		{"partials/sidebar.html", PageData{
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

func samplePager(path, target, label string) pagerView {
	return newPager(path, nil, 1, 25, 42, target, label)
}

func sampleInsights(now string) userInsights {
	return userInsights{
		GameCount: 2, SaveCount: 4, TotalBytes: 2048, DeviceCount: 2, OnlineCount: 1,
		SyncByDay: []store.DayCount{
			{Day: "2026-06-23", Count: 1}, {Day: "2026-06-24", Count: 0}, {Day: "2026-06-25", Count: 3},
		},
		SyncTotal: 4,
		TopGames: []topGame{
			{GameID: "730", Title: "Counter-Strike 2", TotalBytes: 1536, FileCount: 3, Pct: 100},
			{GameID: "570", Title: "Dota 2", TotalBytes: 512, FileCount: 1, Pct: 33},
		},
		Devices: []clientRow{
			{ClientInfo: store.ClientInfo{ID: "c1", Name: "Gaming PC", OS: "windows", LastSeen: now}, Online: true},
		},
		Alerts: []healthAlert{
			{Tone: "ok", Text: "All devices have synced recently."},
			{Tone: "danger", Text: "Steam Deck hasn't synced in 9 days."},
		},
		LinkGames: true,
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
	case "setup.html":
		return map[string]interface{}{
			"CSRFToken": "csrf-test",
			"Error":     "",
			"Locked":    false,
		}
	case "recovery_codes.html":
		return map[string]interface{}{
			"PageName": "recovery_codes", "Username": "testuser", "IsAdmin": false,
			"CSRFToken": "csrf-test", "NavActive": "settings",
			"Codes":        []string{"AAAAA-BBBBB", "CCCCC-DDDDD"},
			"CodesJoined":  "AAAAA-BBBBB\nCCCCC-DDDDD",
			"DownloadData": "data:text/plain;base64,dGVzdA==",
		}
	case "error.html":
		return map[string]interface{}{
			"StatusCode": 404,
			"Title":      "Page not found",
			"Message":    "The page you're looking for doesn't exist.",
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
				{Version: 1, UpdatedAt: now, SizeBytes: 512, ChangeBytes: 512, ClientName: "Gaming PC"},
				{Version: 2, UpdatedAt: now, SizeBytes: 768, ChangeBytes: 256, ClientID: "c1", ClientName: "Steam Deck"},
			},
			CurrentVersion: 2,
		}
	case "dashboard_games.html", "partials/game_cards.html":
		return dashboardGamesData{
			PageData: PageData{
				PageName: "dashboard_games", Username: pd.Username, IsAdmin: pd.IsAdmin,
				CSRFToken: pd.CSRFToken, NavActive: "games",
			},
			Games: []gameCard{
				{GameID: "730", Title: "Counter-Strike 2", FileCount: 3, TotalBytes: 1536, LastSynced: now, Status: "healthy"},
				{GameID: "570", Title: "Dota 2", FileCount: 1, TotalBytes: 512, LastSynced: now, Status: "stale"},
			},
			TotalGames: 2, TotalFiles: 4, TotalBytes: 2048, MaxFiles: 3,
			Query: "", Status: "all", Sort: "recent", View: "grid",
		}
	case "game_detail.html":
		return gameDetailData{
			PageData: PageData{
				PageName: "game_detail", Username: pd.Username, IsAdmin: pd.IsAdmin,
				CSRFToken: pd.CSRFToken, NavActive: "games",
			},
			GameID: "730", Title: "Counter-Strike 2",
			FileCount: 2, TotalBytes: 1536, LastSynced: now, Status: "healthy",
			Encrypted: false, EncryptionLabel: "Standard", CategoryCount: 1,
			Categories: []saveCategory{{
				Name: "Saves", TotalBytes: 1536,
				Files: []saveFileRow{
					{SaveSummary: store.SaveSummary{GameID: "730", PathKey: "save/main", SizeBytes: 1024, UpdatedAt: now}, Name: "main.dat"},
					{SaveSummary: store.SaveSummary{GameID: "730", PathKey: "save/alt", SizeBytes: 512, UpdatedAt: now}, Name: "alt.dat"},
				},
			}},
			LargestFile:      saveFileRow{SaveSummary: store.SaveSummary{GameID: "730", PathKey: "save/main", SizeBytes: 1024}, Name: "main.dat"},
			HasLargestChange: true,
			LargestChange:    store.SaveChangeRow{ChangeBytes: 256, ClientName: "Gaming PC", PathKey: "save/main", UpdatedAt: now},
		}
	case "dashboard_clients.html", "partials/clients_list.html":
		return dashboardClientsData{
			PageData: PageData{
				PageName: "dashboard_clients", Username: pd.Username, IsAdmin: pd.IsAdmin,
				CSRFToken: pd.CSRFToken, NavActive: "clients",
			},
			Clients: []clientRow{
				{ClientInfo: store.ClientInfo{ID: "c1", Name: "Gaming PC", OS: "windows", LastSeen: now}, Online: true},
				{ClientInfo: store.ClientInfo{ID: "c2", Name: "Steam Deck", OS: "linux", LastSeen: now}, Online: false},
			},
			Online: 1, Total: 2,
		}
	case "partials/cmdk_results.html":
		return cmdkResults{
			Query: "cs",
			Commands: []cmdkCommand{
				{Label: "My Games", Sub: "Browse synced saves", Href: "/dashboard/games", Icon: "🎮"},
			},
			Games: []cmdkGameResult{
				{GameID: "730", Title: "Counter-Strike 2", Meta: "3 files · 1.5 KB"},
			},
		}
	case "dashboard_analytics.html", "partials/insights_body.html":
		return analyticsData{
			PageData: PageData{
				PageName: "dashboard_analytics", Username: pd.Username, IsAdmin: pd.IsAdmin,
				CSRFToken: pd.CSRFToken, NavActive: "analytics",
			},
			userInsights: sampleInsights(now),
		}
	case "admin_user_detail.html":
		return adminUserDetailData{
			PageData: PageData{
				PageName: "admin_user_detail", Username: "admin", IsAdmin: true,
				CSRFToken: "csrf-test", AdminNav: "users",
			},
			userInsights:   sampleInsights(now),
			TargetUsername: "alice",
			TargetUserID:   "user-1",
			QuotaBytes:     10737418240,
			TargetRole:     "user",
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
			FetchesTable: fetchesTableView{
				Rows: []store.ManifestFetchRow{
					{ClientName: "PC", Username: "alice", EntriesCount: 100, FetchedAt: now},
				},
				Pager: samplePager("/admin/partial/fetches", "#fetches-table-region", "fetches"),
			},
			AuditTable: auditTableView{
				Rows: []store.AuditRow{
					{At: now, ActorUsername: "admin", Action: "run_job", TargetID: "pcgw_sync"},
				},
				Actions: []string{"run_job", "revoke_client"},
				Filter:  store.AuditLogFilter{},
				Pager:   samplePager("/admin/partial/audit", "#audit-table-region", "entries"),
			},
			SnapshotsTable: snapshotsTableView{
				Rows: []store.StatsSnapshotRow{
					{At: now, UserCount: 1, ClientCount: 1, SaveCount: 5, StorageBytes: 1024},
				},
				Pager: samplePager("/admin/partial/snapshots", "#snapshots-table-region", "snapshots"),
			},
			JobsTable: jobsTableView{
				Rows: []store.JobRun{
					{JobName: "pcgw_sync", StartedAt: now, FinishedAt: now, Status: "success", EntriesCount: 50},
				},
				Pager:       samplePager("/admin/partial/jobs", "#admin-jobs-panel", "runs"),
				JobNames:    []string{"pcgw_sync", "backup"},
				ShowFilters: true,
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
			JobETASec:            900,
			JobCatalogScanMode:   "fast_probe",
			JobPhaseLabel:        "Phase 2: Parsing game data",
			AvgHistPagesPerSec:   3.0,
			BundleSyncSource:     store.PCGWSyncSourceS3,
			BundleLastFetched:    now,
			BundleLastExported:   now,
			BundleLastETag:       `"abc123"`,
		}
	case "admin_logs.html":
		return adminLogsData{
			PageData: PageData{
				PageName: "admin_logs", Username: "admin", IsAdmin: true, CSRFToken: "csrf-test", AdminNav: "logs",
			},
			Entries: []logview.Entry{
				{
					Timestamp: now, Level: "info", Event: "http.request", Component: "http",
					Summary: "GET /api/manifest → 200 in 12ms from 10.0.0.5",
					Message: "GET /api/manifest → 200 in 12ms from 10.0.0.5",
					Context: "component=http request_id=abc123 user_id=user-1",
					Method:  "GET", Path: "/api/manifest", Status: "200", Duration: "12ms",
					RequestID: "abc123", IP: "10.0.0.5", UserID: "user-1",
					Raw: `{"level":"info","event":"http.request","component":"http","message":"GET /api/manifest 200","method":"GET","path":"/api/manifest","status":200,"duration":12,"ip":"10.0.0.5","request_id":"abc123","user_id":"user-1"}`,
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
			CapStatusText:     "Phase 2 parse/store cap: 5000 pages per run (default). Phase 1 catalog scan always fetches all IDs.",
			BundleSyncSource:  store.PCGWSyncSourceS3,
			BundleLastFetched: now,
			BundleLastETag:    `"etag-test"`,
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
			PCGWSyncSource:        store.PCGWSyncSourceS3,
			PCGWBundleCron:        store.DefaultPCGWBundleCron,
			PCGWBundleURL:         store.DefaultPCGWBundleURL,
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
			"Append":     false,
			"HasMore":    true,
			"NextOffset": 20,
		}
	case "partials/admin_audit_table.html":
		return auditTableView{
			Rows: []store.AuditRow{
				{At: now, ActorUsername: "admin", Action: "run_job", TargetID: "pcgw_sync"},
			},
			Actions: []string{"run_job"},
			Pager:   samplePager("/admin/partial/audit", "#audit-table-region", "entries"),
		}
	case "partials/admin_fetches_table.html":
		return fetchesTableView{
			Rows: []store.ManifestFetchRow{
				{ClientName: "PC", Username: "alice", EntriesCount: 100, FetchedAt: now},
			},
			Pager: samplePager("/admin/partial/fetches", "#fetches-table-region", "fetches"),
		}
	case "partials/admin_snapshots_table.html":
		return snapshotsTableView{
			Rows: []store.StatsSnapshotRow{
				{At: now, UserCount: 1, ClientCount: 1, SaveCount: 5, StorageBytes: 1024},
			},
			Pager: samplePager("/admin/partial/snapshots", "#snapshots-table-region", "snapshots"),
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
			"JobRunning":           false,
			"JobProgressPages":     0,
			"JobProgressTotal":     0,
			"JobGamesSkipped":      0,
			"JobElapsedSec":        0,
			"JobETAMin":            -1,
			"JobPhaseLabel":        "",
			"ResumableSyncRun":     (*types.PCGWSyncRun)(nil),
			"CapStatusText":        "Phase 2 parse/store cap: 5000 pages per run (default). Phase 1 catalog scan always fetches all IDs.",
			"CSRFToken":            "csrf-test",
			"ShowPCGWControls":     false,
			"LastSuccessfulSyncAt": now,
			"JobsTable": jobsTableView{
				Pager:       samplePager("/admin/partial/jobs", "#admin-jobs-panel", "runs"),
				JobNames:    []string{"pcgw_sync"},
				ShowFilters: true,
			},
		}
	case "partials/admin_logs_table.html":
		return map[string]interface{}{
			"Entries": []logview.Entry{
				{
					Timestamp: now, Level: "info", Event: "http.request",
					Summary: "GET /api/manifest → 200 in 12ms from 10.0.0.5",
					Message: "GET /api/manifest → 200 in 12ms from 10.0.0.5",
					Context: "request_id=abc123 user_id=user-1",
					Method:  "GET", Path: "/api/manifest", Status: "200", Duration: "12ms",
					RequestID: "abc123", IP: "10.0.0.5", UserID: "user-1",
					Raw: `{"level":"info","event":"http.request","message":"GET /api/manifest 200","method":"GET","path":"/api/manifest","status":200,"duration":12,"ip":"10.0.0.5","request_id":"abc123","user_id":"user-1"}`,
				},
			},
			"Total":            1,
			"Query":            logview.Query{Level: "all", Limit: 200, RefreshSeconds: 5, Component: "all", HideHTTPNoise: true},
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
			"JobRunning":         true,
			"JobProgressPages":   42,
			"JobProgressTotal":   100,
			"JobGamesSkipped":    3,
			"JobPhase":           "ingest",
			"CSRFToken":          "csrf-test",
			"JobElapsedSec":      90,
			"JobPagesPerSec":     1.5,
			"JobETAMin":          5,
			"JobETASec":          300,
			"JobPhaseLabel":      "Phase 2: Parsing game data",
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
