package webui

import (
	"bytes"
	"fmt"
	"html/template"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gsbs/gsbs/pkg/i18n"
	"github.com/gsbs/gsbs/pkg/logview"
	"github.com/gsbs/gsbs/pkg/types"
	"github.com/gsbs/gsbs/server/logx"
	"github.com/gsbs/gsbs/server/store"
)

// PageData is embedded in page-specific view models for layout rendering.
type PageData struct {
	PageName  string // template prefix for layout blocks, e.g. "dashboard"
	Username  string
	IsAdmin   bool
	Locale    string // negotiated UI locale ("" renders English); used by {{t .Locale ...}} and <html lang>
	CSRFToken string
	NavActive string // dashboard, settings, admin, admin-users, admin-manifest, admin-activity
	BodyClass string
	Error     string
	Success   string
	Restored  bool   // dashboard save restore flash
	Deleted   bool   // dashboard save delete flash
	AdminNav  string // non-empty enables admin sidebar: overview, users, manifest, activity
}

type dashboardData struct {
	PageData
	Stats      dashboardStats
	Clients    []store.ClientInfo
	Saves      []store.SaveSummary
	QuotaBytes int64
}

type dashboardStats struct {
	ClientCount int
	SaveCount   int
	GameCount   int
	TotalBytes  int64 // current saves only (what the stat card shows)
	UsageBytes  int64 // saves + retained version history (what quotas enforce against)
	StoreError  bool  // true when any store call failed; template renders an error notice
}

type saveVersionsData struct {
	PageData
	GameID         string
	PathKey        string
	GameTitle      string
	Versions       []store.SaveVersionInfo
	CurrentVersion int
}

type settingsData struct {
	PageData
	Sessions          []store.SessionRow
	CurrentSessionID  string
	TOTPEnabled       bool
	RecoveryCount     int
	EncryptionEnabled bool
	Notify            store.UserNotifySettings
	Locale            string
	Locales           []string
}

// gameCard is one tile/row on the My Games page.
type gameCard struct {
	GameID     string
	Title      string
	FileCount  int
	TotalBytes int64
	LastSynced string
	Status     string // healthy, stale, unknown
}

// dashboardGamesData drives both the My Games page and its HTMX cards partial.
type dashboardGamesData struct {
	PageData
	Games       []gameCard
	TotalGames  int
	TotalFiles  int
	TotalBytes  int64
	MaxFiles    int    // largest FileCount among games, for the progress ring scale
	Query       string // active search
	Status      string // active status filter: all, healthy, stale
	Sort        string // recent, name, size, files
	View        string // grid, list
	ReadOnly    bool
	ImportedMsg string // "Imported N save(s)" feedback after archive import
}

// gameDetailData drives the individual game detail page.
type gameDetailData struct {
	PageData
	GameID           string
	Title            string
	FileCount        int
	TotalBytes       int64
	LastSynced       string
	Status           string
	Encrypted        bool   // any of the game's saves are E2E-encrypted
	EncryptionLabel  string // "Encrypted" / "Standard"
	CategoryCount    int
	Categories       []saveCategory // reused from saves_group.go
	LargestFile      saveFileRow
	HasLargestChange bool
	LargestChange    store.SaveChangeRow
	ReadOnly         bool
}

type adminStats struct {
	UserCount     int
	ClientCount   int
	SaveCount     int
	ManifestCount int
	TotalBytes    int64
}

type adminOverviewData struct {
	PageData
	Stats                 adminStats
	StatsSnapshots        []store.StatsSnapshotRow
	SSEClients            int
	AllowRegister         bool
	ShowGettingStarted    bool
	MaxStorageBytes       int64
	ReadOnly              bool
	RecentJobs            []store.JobRun
	JobRunning            bool
	JobProgressPages      int
	JobProgressTotal      int
	JobGamesSkipped       int
	JobPhase              string
	JobAutoCatchUp        bool
	LastSuccessfulSyncAt  string
	CatalogStats          types.PCGWCatalogStats
	MaxPagesPerRun        int
	MaxPagesPerRunFromEnv bool
	MaxPagesPerRunSource  string
	CapReached            bool
	CapStatusText         string
	ShowPCGWControls      bool
	ResumableSyncRun      *types.PCGWSyncRun
	JobElapsedSec         int
	JobPagesPerSec        float64
	JobETAMin             int
	JobETASec             int
	JobCatalogScanMode    string
	JobPhaseLabel         string
	AvgHistPagesPerSec    float64
	IdleRunsNeeded        int
	IdleTotalETASec       int
	IdlePerRunETASec      int
	// First-run onboarding: prompt the admin to choose a save-location source
	// (prebuilt bundle vs live PCGW API) when none has been explicitly chosen.
	ShowSourcePrompt bool
	Version          string
	// Blob-integrity verification (weekly job + manual trigger).
	IntegrityFindings []store.IntegrityFinding
	IntegrityCount    int
	IntegrityRunning  bool
	IntegrityLastRun  *store.JobRun
}

type adminUsersData struct {
	PageData
	CurrentUserID  string
	Users          []store.UserStatRow
	Clients        []store.ClientInfoWithUser
	MaxClientCount int
}

type adminManifestData struct {
	PageData
	Stats              adminStats
	Manifest           []types.GameSaveLocation
	Query              string
	ManifestPage       int
	ManifestPerPage    int
	ManifestTotal      int
	ManifestTotalPages int
	ManifestStart      int
	ManifestEnd        int
	ManifestPrevPage   int
	ManifestNextPage   int
}

type adminActivityData struct {
	PageData
	Fetches               []store.ManifestFetchRow
	AuditLog              []store.AuditRow
	StatsSnapshots        []store.StatsSnapshotRow
	RecentJobs            []store.JobRun
	JobRunning            bool
	JobProgressPages      int
	JobProgressTotal      int
	JobGamesSkipped       int
	JobPhase              string
	JobAutoCatchUp        bool
	LastSuccessfulSyncAt  string
	CatalogStats          types.PCGWCatalogStats
	LatestSyncRun         *types.PCGWSyncRun
	MaxPagesPerRun        int
	MaxPagesPerRunFromEnv bool
	MaxPagesPerRunSource  string
	CapReached            bool
	CapStatusText         string
	ShowPCGWControls      bool
	ResumableSyncRun      *types.PCGWSyncRun
	JobElapsedSec         int
	JobPagesPerSec        float64
	JobETAMin             int
	JobETASec             int
	JobCatalogScanMode    string
	JobPhaseLabel         string
	AvgHistPagesPerSec    float64
	IdleRunsNeeded        int
	IdleTotalETASec       int
	IdlePerRunETASec      int
	BundleSyncSource      string
	BundleLastFetched     string
	BundleLastExported    string
	BundleLastETag        string
	BundleLastError       string
	BundleJobRunning      bool
}

type adminLogsData struct {
	PageData
	Entries          []logview.Entry
	LogSourcePath    string
	LogSourceInfo    string
	LogSourcePresent bool
	Query            logview.Query
}

func newTemplateFuncs(t *template.Template) template.FuncMap {
	return template.FuncMap{
		"formatTime":      formatTime,
		"formatBytes":     formatBytes,
		"truncate":        truncate,
		"formatDuration":  formatDuration,
		"urlquery":        url.QueryEscape,
		"t":               i18n.T,
		"auditLabel":      auditLabel,
		"chartLineSVG":    chartLineSVG,
		"percent":         percent,
		"percentInt":      percentInt,
		"minInt":          minInt,
		"quotaBarClass":   quotaBarClass,
		"clientBarWidth":  clientBarWidth,
		"renderPageBlock": renderPageBlock(t),
		"add":             func(a, b int) int { return a + b },
		"sub":             func(a, b int) int { return a - b },
		"div": func(a, b int) int {
			if b == 0 {
				return 0
			}
			return a / b
		},
		"mod": func(a, b int) int {
			if b == 0 {
				return 0
			}
			return a % b
		},
		"join":            strings.Join,
		"formatETASec":    formatETASec,
		"gameIconSVG":     gameIconSVG,
		"progressRingSVG": progressRingSVG,
		"gaugeSVG":        gaugeSVG,
		"gameSyncStatus":  gameSyncStatus,
		"auditTone":       auditTone,
		"auditCategory":   auditCategory,
		"barsSVG":         barsSVG,
		"bytesBarsSVG":    bytesBarsSVG,
		"monthsToDays":    monthsToDays,
		"signedBytes":     signedBytes,
		"dict":            dict,
		"iconSVG":         iconSVG,
		"timeAgo":         timeAgo,
	}
}

// iconPaths holds the inner markup of each UI icon (16×16 viewBox, stroked
// with currentColor at 1.4 — the same visual language as the admin sidebar).
var iconPaths = map[string]string{
	"game":     `<rect x="1.5" y="5" width="13" height="7" rx="3"/><path d="M5 7.5v2M4 8.5h2"/><circle cx="10.5" cy="7.8" r="0.8" fill="currentColor" stroke="none"/><circle cx="12.2" cy="9.4" r="0.8" fill="currentColor" stroke="none"/>`,
	"device":   `<rect x="1.5" y="2.5" width="13" height="8.5" rx="1"/><path d="M5.5 14h5M8 11v3"/>`,
	"settings": `<circle cx="8" cy="8" r="2.2"/><path d="M8 1.8v2M8 12.2v2M1.8 8h2M12.2 8h2M3.6 3.6l1.4 1.4M11 11l1.4 1.4M12.4 3.6 11 5M5 11l-1.4 1.4"/>`,
	"search":   `<circle cx="7" cy="7" r="4.5"/><path d="m10.5 10.5 3.5 3.5"/>`,
	"sun":      `<circle cx="8" cy="8" r="3"/><path d="M8 1.5V3M8 13v1.5M1.5 8H3M13 8h1.5M3.4 3.4l1 1M11.6 11.6l1 1M12.6 3.4l-1 1M4.4 11.6l-1 1"/>`,
	"moon":     `<path d="M13.5 9.5A6 6 0 0 1 6.5 2.5a6 6 0 1 0 7 7Z"/>`,
	"grid":     `<rect x="1.5" y="1.5" width="5" height="5" rx="1"/><rect x="9.5" y="1.5" width="5" height="5" rx="1"/><rect x="1.5" y="9.5" width="5" height="5" rx="1"/><rect x="9.5" y="9.5" width="5" height="5" rx="1"/>`,
	"list":     `<path d="M5 4h9M5 8h9M5 12h9"/><circle cx="2" cy="4" r="0.9" fill="currentColor" stroke="none"/><circle cx="2" cy="8" r="0.9" fill="currentColor" stroke="none"/><circle cx="2" cy="12" r="0.9" fill="currentColor" stroke="none"/>`,
	"chart":    `<path d="M2 14V2M2 14h12"/><rect x="4" y="8" width="2.2" height="4" fill="currentColor" stroke="none"/><rect x="7.5" y="5" width="2.2" height="7" fill="currentColor" stroke="none"/><rect x="11" y="9" width="2.2" height="3" fill="currentColor" stroke="none"/>`,
	"clock":    `<circle cx="8" cy="8" r="6"/><path d="M8 4.5V8l2.5 1.5"/>`,
	"shield":   `<path d="M8 1.5 13.5 3.5v4.2c0 3.2-2.3 5.6-5.5 6.8-3.2-1.2-5.5-3.6-5.5-6.8V3.5Z"/><path d="m5.5 7.8 1.8 1.8 3.2-3.4"/>`,
	"save":     `<path d="M2.5 2.5h8.5l2.5 2.5v8.5h-11Z"/><path d="M5 2.5V6h5.5V2.5M5 13.5V9.5h6v4"/>`,
	"folder":   `<path d="M1.5 4a1 1 0 0 1 1-1h3.6l1.5 1.8h6a1 1 0 0 1 1 1V12a1 1 0 0 1-1 1H2.5a1 1 0 0 1-1-1Z"/>`,
	"copy":     `<rect x="5.5" y="5.5" width="8" height="8" rx="1"/><path d="M10.5 3.5v-1h-8v8h1"/>`,
	"check":    `<path d="m3 8.5 3.2 3.2L13 4.5"/>`,
	"x":        `<path d="m4 4 8 8M12 4l-8 8"/>`,
	"alert":    `<path d="M8 2 14.5 13.5H1.5Z"/><path d="M8 6.5v3.2"/><circle cx="8" cy="11.6" r="0.7" fill="currentColor" stroke="none"/>`,
	"info":     `<circle cx="8" cy="8" r="6"/><path d="M8 7.2V11"/><circle cx="8" cy="5" r="0.7" fill="currentColor" stroke="none"/>`,
	"download": `<path d="M8 2v7.5M4.8 6.8 8 10l3.2-3.2M2.5 12.5v1h11v-1"/>`,
	"refresh":  `<path d="M13 8a5 5 0 1 1-1.5-3.5M13 2.5V5h-2.5"/>`,
	"key":      `<circle cx="5" cy="8" r="3"/><path d="M8 8h6M11.5 8v2.5M13.5 8v1.8"/>`,
	"eye":      `<path d="M1.5 8s2.4-4.2 6.5-4.2S14.5 8 14.5 8s-2.4 4.2-6.5 4.2S1.5 8 1.5 8Z"/><circle cx="8" cy="8" r="1.9"/>`,
	"eye-off":  `<path d="M3 3l10 10M6.3 6.4a1.9 1.9 0 0 0 2.6 2.7M5 4.6C3 5.7 1.5 8 1.5 8s2.4 4.2 6.5 4.2c1.1 0 2.1-.3 3-.7M9.9 4A6.7 6.7 0 0 0 8 3.8C3.9 3.8 1.5 8 1.5 8"/>`,
	"user":     `<circle cx="8" cy="5" r="2.6"/><path d="M2.5 14c0-3 2.5-4.5 5.5-4.5s5.5 1.5 5.5 4.5"/>`,
	"logout":   `<path d="M6 2.5H3a1 1 0 0 0-1 1v9a1 1 0 0 0 1 1h3M10.5 11 13.5 8l-3-3M6.5 8h7"/>`,
}

// iconSVG renders a named inline UI icon at the given pixel size. Unknown
// names render the "info" icon so a typo is visible rather than invisible.
func iconSVG(name string, size int) template.HTML {
	inner, ok := iconPaths[name]
	if !ok {
		inner = iconPaths["info"]
	}
	if size <= 0 {
		size = 16
	}
	return template.HTML(fmt.Sprintf(
		`<svg viewBox="0 0 16 16" width="%d" height="%d" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">%s</svg>`,
		size, size, inner))
}

// timeAgo renders a compact relative time ("2h ago", "3d ago"). Zero times
// render as "never". Templates should pair it with an absolute title attr.
func timeAgo(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := time.Since(t)
	switch {
	case d < 0:
		return "just now"
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo ago", int(d.Hours()/(24*30)))
	default:
		return fmt.Sprintf("%dy ago", int(d.Hours()/(24*365)))
	}
}

// signedBytes formats a byte delta with an explicit sign: "+1.2 KB", "−500 B",
// or "—" for no change.
func signedBytes(n int64) string {
	if n == 0 {
		return "—"
	}
	if n > 0 {
		return "+" + formatBytes(n)
	}
	return "−" + formatBytes(-n)
}

// barsSVG renders a vertical bar chart from a per-day count series (oldest
// first). Each bar carries a <title> for hover tooltips. The viewBox is
// stretched to fill its container (preserveAspectRatio=none).
func barsSVG(counts []store.DayCount, width, height int) template.HTML {
	if len(counts) == 0 {
		return template.HTML(`<p class="cell-muted">No activity yet.</p>`)
	}
	maxV := 1
	for _, c := range counts {
		if c.Count > maxV {
			maxV = c.Count
		}
	}
	n := len(counts)
	gap := 1.0
	bw := (float64(width) - gap*float64(n-1)) / float64(n)
	var b bytes.Buffer
	for i, c := range counts {
		bh := float64(height) * float64(c.Count) / float64(maxV)
		if c.Count > 0 && bh < 2 {
			bh = 2
		}
		x := float64(i) * (bw + gap)
		y := float64(height) - bh
		fill := "var(--accent)"
		if c.Count == 0 {
			fill = "var(--border)"
			bh = 2
			y = float64(height) - bh
		}
		fmt.Fprintf(&b, `<rect x="%.2f" y="%.2f" width="%.2f" height="%.2f" fill="%s"><title>%s: %d</title></rect>`,
			x, y, bw, bh, fill, c.Day, c.Count)
	}
	return template.HTML(fmt.Sprintf( //nolint:gosec // G203: SVG markup built entirely from server-computed numbers, no user input
		`<svg class="bars-svg" viewBox="0 0 %d %d" preserveAspectRatio="none" role="img" aria-label="Sync volume by day">%s</svg>`,
		width, height, b.String()))
}

// bytesBarsSVG renders a vertical bar chart from a per-day byte series
// (oldest first), mirroring barsSVG but with human-readable byte tooltips.
func bytesBarsSVG(series []store.DayBytes, width, height int) template.HTML {
	if len(series) == 0 {
		return ""
	}
	var maxV int64 = 1
	for _, d := range series {
		if d.Bytes > maxV {
			maxV = d.Bytes
		}
	}
	n := len(series)
	gap := 1.0
	bw := (float64(width) - gap*float64(n-1)) / float64(n)
	var b bytes.Buffer
	for i, d := range series {
		bh := float64(height) * float64(d.Bytes) / float64(maxV)
		if d.Bytes > 0 && bh < 2 {
			bh = 2
		}
		x := float64(i) * (bw + gap)
		y := float64(height) - bh
		fill := "var(--info)"
		if d.Bytes == 0 {
			fill = "var(--border)"
			bh = 2
			y = float64(height) - bh
		}
		fmt.Fprintf(&b, `<rect x="%.2f" y="%.2f" width="%.2f" height="%.2f" fill="%s"><title>%s: %s</title></rect>`,
			x, y, bw, bh, fill, d.Day, formatBytes(d.Bytes))
	}
	return template.HTML(fmt.Sprintf( //nolint:gosec // G203: SVG markup built entirely from server-computed numbers, no user input
		`<svg class="bars-svg" viewBox="0 0 %d %d" preserveAspectRatio="none" role="img" aria-label="Data synced by day">%s</svg>`,
		width, height, b.String()))
}

// monthsToDays adapts a per-month series to the DayCount shape barsSVG
// renders (the Day field is just the tooltip label).
func monthsToDays(months []store.MonthCount) []store.DayCount {
	out := make([]store.DayCount, 0, len(months))
	for _, m := range months {
		out = append(out, store.DayCount{Day: m.Month, Count: m.Count})
	}
	return out
}

// auditCategory groups an audit action for the dashboard activity tabs.
func auditCategory(action string) string {
	switch action {
	case "restore_version", "delete_save", "delete_game_saves":
		return "saves"
	case "revoke_client", "rename_client":
		return "devices"
	case "enable_2fa", "disable_2fa", "encryption_setting", "revoke_session":
		return "security"
	default:
		return "other"
	}
}

// auditTone maps an audit action to a semantic colour tone for its timeline dot.
func auditTone(action string) string {
	switch action {
	case "delete_save", "delete_game_saves", "revoke_client", "delete_user", "disable_user", "disable_2fa":
		return "danger"
	case "restore_version", "rename_client", "enable_2fa", "enable_user", "set_quota":
		return "success"
	case "run_job", "push_manifest":
		return "info"
	default:
		return ""
	}
}

// dict builds a map from alternating key/value arguments. It lets parameterised
// partials (e.g. metric-card, empty-state) be invoked with named fields, since
// html/template has no map literal syntax.
func dict(values ...interface{}) (map[string]interface{}, error) {
	if len(values)%2 != 0 {
		return nil, fmt.Errorf("dict: expected an even number of arguments, got %d", len(values))
	}
	m := make(map[string]interface{}, len(values)/2)
	for i := 0; i < len(values); i += 2 {
		key, ok := values[i].(string)
		if !ok {
			return nil, fmt.Errorf("dict: key %d is not a string", i)
		}
		m[key] = values[i+1]
	}
	return m, nil
}

func renderPageBlock(t *template.Template) func(pageName, block string, data interface{}) (template.HTML, error) {
	return func(pageName, block string, data interface{}) (template.HTML, error) {
		name := pageName + "_" + block
		if t.Lookup(name) == nil {
			return "", nil
		}
		var buf bytes.Buffer
		if err := t.ExecuteTemplate(&buf, name, data); err != nil {
			return "", err
		}
		return template.HTML(buf.String()), nil //nolint:gosec // G203: SVG markup built entirely from server-computed numbers, no user input
	}
}

func parseTemplates() *template.Template {
	tmpl := template.New("")
	tmpl = tmpl.Funcs(newTemplateFuncs(tmpl))
	return template.Must(tmpl.ParseFS(templatesFS, "templates/*.html", "templates/**/*.html"))
}

func (h *WebHandler) renderPartial(w http.ResponseWriter, name string, data interface{}) {
	h.render(w, name, data)
}

func (h *WebHandler) render(w http.ResponseWriter, name string, data interface{}) {
	if err := h.templates.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
		logx.Logger().Error().Str("template", name).Err(err).Msg("webui template render failed")
	}
}

// renderError serves the branded error page (error.html) with the given
// status. Used instead of bare http.Error/http.NotFound on user-facing pages.
func (h *WebHandler) renderError(w http.ResponseWriter, status int, title, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	h.render(w, "error.html", map[string]interface{}{
		"StatusCode": status,
		"Title":      title,
		"Message":    message,
	})
}

// notFoundPage is the branded 404 for unmatched WebUI routes.
func (h *WebHandler) notFoundPage(w http.ResponseWriter) {
	h.renderError(w, http.StatusNotFound, "Page not found",
		"The page you're looking for doesn't exist or may have moved.")
}

func auditLabel(action string) string {
	labels := map[string]string{
		"restore_version":           "Restored save version",
		"delete_save":               "Deleted save",
		"delete_game_saves":         "Deleted all saves for a game",
		"revoke_client":             "Revoked client token",
		"rename_client":             "Renamed a device",
		"clear_cover_cache":         "Cleared the cover-art cache",
		"push_manifest":             "Pushed manifest update",
		"run_job":                   "Started PCGW sync job",
		"disable_user":              "Disabled user",
		"enable_user":               "Enabled user",
		"delete_user":               "Deleted user",
		"set_quota":                 "Updated storage quota",
		"revoke_session":            "Revoked session",
		"enable_2fa":                "Enabled two-factor auth",
		"regenerate_recovery_codes": "Regenerated 2FA recovery codes",
		"disable_2fa":               "Disabled two-factor auth",
	}
	if l, ok := labels[action]; ok {
		return l
	}
	return strings.ReplaceAll(action, "_", " ")
}

func percent(used, total int64) float64 {
	if total <= 0 {
		return 0
	}
	return float64(used) / float64(total) * 100
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// chartLineSVG renders a simple line chart SVG from stats snapshots (oldest first).
func chartLineSVG(snapshots []store.StatsSnapshotRow, field string, width, height int) template.HTML {
	if len(snapshots) < 2 {
		return template.HTML(`<p class="cell-muted">Not enough data for chart.</p>`)
	}
	// snapshots come newest-first; reverse for chronological
	pts := make([]store.StatsSnapshotRow, len(snapshots))
	for i := range snapshots {
		pts[i] = snapshots[len(snapshots)-1-i]
	}
	values := make([]float64, len(pts))
	for i, s := range pts {
		switch field {
		case "users":
			values[i] = float64(s.UserCount)
		case "clients":
			values[i] = float64(s.ClientCount)
		case "saves":
			values[i] = float64(s.SaveCount)
		default:
			values[i] = float64(s.StorageBytes)
		}
	}
	minV, maxV := values[0], values[0]
	for _, v := range values[1:] {
		if v < minV {
			minV = v
		}
		if v > maxV {
			maxV = v
		}
	}
	if maxV == minV {
		maxV = minV + 1
	}
	pad := 4.0
	w := float64(width)
	h := float64(height)
	var path bytes.Buffer
	for i, v := range values {
		x := pad + (w-2*pad)*float64(i)/float64(len(values)-1)
		y := h - pad - (h-2*pad)*(v-minV)/(maxV-minV)
		if i == 0 {
			fmt.Fprintf(&path, "M%.1f,%.1f", x, y)
		} else {
			fmt.Fprintf(&path, " L%.1f,%.1f", x, y)
		}
	}
	svg := fmt.Sprintf(`<svg class="chart-svg" viewBox="0 0 %d %d" preserveAspectRatio="none" aria-hidden="true"><path d="%s" fill="none" stroke="#6366f1" stroke-width="2"/></svg>`,
		width, height, path.String())
	return template.HTML(svg) //nolint:gosec // G203: SVG markup built entirely from server-computed numbers, no user input
}

// gameInitials returns up to two uppercase letters representing a title: the
// first letters of the first two words, or the first two runes of a single word.
func gameInitials(title string) string {
	fields := strings.Fields(title)
	if len(fields) == 0 {
		return "?"
	}
	if len(fields) >= 2 {
		a := []rune(fields[0])
		b := []rune(fields[1])
		return strings.ToUpper(string(a[0]) + string(b[0]))
	}
	r := []rune(fields[0])
	if len(r) == 1 {
		return strings.ToUpper(string(r[0]))
	}
	return strings.ToUpper(string(r[0:2]))
}

// gameIconSVG renders a deterministic cover-art stand-in: a rounded tile whose
// gradient hue is derived from the title plus the game's initials. It is the
// single source of truth for "cover art" until the real cover pipeline lands
// (see backlog). The title is HTML-escaped for both the label and the text.
func gameIconSVG(title string, size int) template.HTML {
	t := strings.TrimSpace(title)
	if t == "" {
		t = "?"
	}
	var hash uint32 = 2166136261
	for _, r := range t {
		hash ^= uint32(r)
		hash *= 16777619
	}
	hue := int(hash % 360)
	id := fmt.Sprintf("gi%d", hash%1000000)
	initials := template.HTMLEscapeString(gameInitials(t))
	label := template.HTMLEscapeString(t)
	fontSize := size * 36 / 100
	radius := size * 20 / 100
	svg := fmt.Sprintf(`<svg class="game-icon" width="%d" height="%d" viewBox="0 0 %d %d" role="img" aria-label="%s" xmlns="http://www.w3.org/2000/svg">`+
		`<defs><linearGradient id="%s" x1="0" y1="0" x2="1" y2="1">`+
		`<stop offset="0" stop-color="hsl(%d,52%%,44%%)"/><stop offset="1" stop-color="hsl(%d,58%%,26%%)"/></linearGradient></defs>`+
		`<rect width="%d" height="%d" rx="%d" fill="url(#%s)"/>`+
		`<text x="50%%" y="50%%" text-anchor="middle" dominant-baseline="central" `+
		`font-family="DM Sans, system-ui, sans-serif" font-weight="700" font-size="%d" fill="rgba(255,255,255,0.94)">%s</text></svg>`,
		size, size, size, size, label,
		id, hue, (hue+22)%360,
		size, size, radius, id,
		fontSize, initials)
	return template.HTML(svg)
}

// progressRingSVG renders a circular progress indicator with the value at its
// centre. max<=0 is treated as 1; the fraction is clamped to [0,1].
func progressRingSVG(value, max, size int) template.HTML {
	if max <= 0 {
		max = 1
	}
	frac := float64(value) / float64(max)
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	sw := float64(size) * 0.1
	r := (float64(size) - sw) / 2
	c := float64(size) / 2
	circ := 2 * math.Pi * r
	off := circ * (1 - frac)
	fontSize := size * 30 / 100
	svg := fmt.Sprintf(`<svg class="progress-ring" width="%d" height="%d" viewBox="0 0 %d %d" role="img" aria-label="%d of %d">`+
		`<circle cx="%.1f" cy="%.1f" r="%.1f" fill="none" stroke="var(--border)" stroke-width="%.1f"/>`+
		`<circle cx="%.1f" cy="%.1f" r="%.1f" fill="none" stroke="var(--accent)" stroke-width="%.1f" stroke-linecap="round" `+
		`stroke-dasharray="%.2f" stroke-dashoffset="%.2f" transform="rotate(-90 %.1f %.1f)"/>`+
		`<text x="50%%" y="50%%" text-anchor="middle" dominant-baseline="central" font-family="var(--font)" font-weight="700" font-size="%d" fill="var(--text)">%d</text></svg>`,
		size, size, size, size, value, max,
		c, c, r, sw,
		c, c, r, sw, circ, off, c, c,
		fontSize, value)
	return template.HTML(svg)
}

// gaugeSVG renders a semicircular storage gauge (a half-donut) with the used
// percentage at its centre. The arc colour shifts to warning/error as usage
// approaches the total. total<=0 renders an empty gauge.
func gaugeSVG(used, total int64, w, h int) template.HTML {
	frac := 0.0
	if total > 0 {
		frac = float64(used) / float64(total)
	}
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	pad := float64(w) * 0.1
	r := float64(w)/2 - pad
	cx := float64(w) / 2
	cy := float64(w) / 2
	sw := r * 0.22
	ang := math.Pi * (1 - frac)
	vEndX := cx + r*math.Cos(ang)
	vEndY := cy - r*math.Sin(ang)
	pct := int(frac*100 + 0.5)
	col := "var(--accent)"
	if frac >= 0.95 {
		col = "var(--error)"
	} else if frac >= 0.8 {
		col = "var(--warning)"
	}
	vh := int(cy + sw)
	fontSize := w * 15 / 100
	svg := fmt.Sprintf(`<svg class="gauge" width="%d" height="%d" viewBox="0 0 %d %d" role="img" aria-label="%d%% of storage used">`+
		`<path d="M%.1f %.1f A%.1f %.1f 0 0 1 %.1f %.1f" fill="none" stroke="var(--border)" stroke-width="%.1f" stroke-linecap="round"/>`+
		`<path d="M%.1f %.1f A%.1f %.1f 0 0 1 %.1f %.1f" fill="none" stroke="%s" stroke-width="%.1f" stroke-linecap="round"/>`+
		`<text x="%.1f" y="%.1f" text-anchor="middle" font-family="var(--font)" font-weight="700" font-size="%d" fill="var(--text)">%d%%</text></svg>`,
		w, vh, w, vh, pct,
		cx-r, cy, r, r, cx+r, cy, sw,
		cx-r, cy, r, r, vEndX, vEndY, col, sw,
		cx, cy-r*0.12, fontSize, pct)
	return template.HTML(svg)
}

// gameSyncStatus derives a coarse health label from a game's most-recent sync
// time: "healthy" when synced within 30 days, "stale" when older, "unknown"
// when no/invalid timestamp.
func gameSyncStatus(lastSynced string) string {
	if lastSynced == "" {
		return "unknown"
	}
	t, err := time.Parse(time.RFC3339, lastSynced)
	if err != nil {
		return "unknown"
	}
	if time.Since(t) > 30*24*time.Hour {
		return "stale"
	}
	return "healthy"
}

// formatTime formats an RFC3339 timestamp for display.
func formatTime(s string) string {
	if s == "" {
		return "\u2014"
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return s
	}
	d := time.Since(t)
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		m := int(d.Minutes())
		if m == 1 {
			return "1 min ago"
		}
		return fmt.Sprintf("%d mins ago", m)
	}
	if d < 24*time.Hour {
		hrs := int(d.Hours())
		if hrs == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hrs)
	}
	if d < 7*24*time.Hour {
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	}
	return t.Format("Jan 2, 2006")
}

func formatBytes(n int64) string {
	if n < 0 {
		n = 0
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	d := float64(n)
	i := 0
	for d >= 1024 && i < len(units)-1 {
		d /= 1024
		i++
	}
	if d >= 10 || d == float64(int64(d)) {
		return fmt.Sprintf("%d %s", int64(d), units[i])
	}
	return fmt.Sprintf("%.1f %s", d, units[i])
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

func formatDuration(start, end string) string {
	if start == "" || end == "" {
		return "—"
	}
	t1, err1 := time.Parse(time.RFC3339, start)
	t2, err2 := time.Parse(time.RFC3339, end)
	if err1 != nil || err2 != nil {
		return "—"
	}
	d := t2.Sub(t1)
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
}

// formatETASec returns a human-readable estimate of seconds remaining.
func formatETASec(sec int) string {
	if sec <= 0 {
		return "—"
	}
	if sec < 60 {
		return fmt.Sprintf("~%ds", sec)
	}
	if sec < 3600 {
		return fmt.Sprintf("~%dm", int(math.Ceil(float64(sec)/60)))
	}
	h := sec / 3600
	m := (sec % 3600) / 60
	if m == 0 {
		return fmt.Sprintf("~%dh", h)
	}
	return fmt.Sprintf("~%dh %dm", h, m)
}

func clientBarWidth(count, max int) int {
	if max <= 0 {
		return 0
	}
	w := count * 100 / max
	if w > 100 {
		return 100
	}
	return w
}

func percentInt(used, total int) float64 {
	if total <= 0 {
		return 0
	}
	p := float64(used) / float64(total) * 100
	if p > 100 {
		return 100
	}
	return p
}

func quotaBarClass(used, quota int64) string {
	if quota <= 0 {
		return ""
	}
	p := float64(used) / float64(quota) * 100
	if p >= 95 {
		return "danger"
	}
	if p >= 80 {
		return "warning"
	}
	return ""
}

func maxClients(users []store.UserStatRow) int {
	max := 1
	for _, u := range users {
		if u.ClientCount > max {
			max = u.ClientCount
		}
	}
	return max
}

func mathCeilDiv(a, b int) int {
	if b <= 0 {
		return 0
	}
	return int(math.Ceil(float64(a) / float64(b)))
}
