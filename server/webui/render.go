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
	TotalBytes  int64
	StoreError  bool // true when any store call failed; template renders an error notice
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
	EncryptionEnabled bool
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
	Games      []gameCard
	TotalGames int
	TotalFiles int
	TotalBytes int64
	MaxFiles   int    // largest FileCount among games, for the progress ring scale
	Query      string // active search
	Status     string // active status filter: all, healthy, stale
	Sort       string // recent, name, size, files
	View       string // grid, list
	ReadOnly   bool
}

// gameDetailData drives the individual game detail page.
type gameDetailData struct {
	PageData
	GameID          string
	Title           string
	FileCount       int
	TotalBytes      int64
	LastSynced      string
	Status          string
	Encrypted       bool   // any of the game's saves are E2E-encrypted
	EncryptionLabel string // "Encrypted" / "Standard"
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
		"div":             func(a, b int) int { if b == 0 { return 0 }; return a / b },
		"mod":             func(a, b int) int { if b == 0 { return 0 }; return a % b },
		"join":            strings.Join,
		"formatETASec":    formatETASec,
		"gameIconSVG":     gameIconSVG,
		"progressRingSVG": progressRingSVG,
		"gaugeSVG":        gaugeSVG,
		"gameSyncStatus":  gameSyncStatus,
		"auditTone":       auditTone,
		"auditCategory":   auditCategory,
		"barsSVG":         barsSVG,
		"signedBytes":     signedBytes,
		"dict":            dict,
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
	return template.HTML(fmt.Sprintf(
		`<svg class="bars-svg" viewBox="0 0 %d %d" preserveAspectRatio="none" role="img" aria-label="Sync volume by day">%s</svg>`,
		width, height, b.String()))
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
		return template.HTML(buf.String()), nil
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

func auditLabel(action string) string {
	labels := map[string]string{
		"restore_version": "Restored save version",
		"delete_save":     "Deleted save",
		"delete_game_saves": "Deleted all saves for a game",
		"revoke_client":   "Revoked client token",
		"rename_client":   "Renamed a device",
		"push_manifest":   "Pushed manifest update",
		"run_job":         "Started PCGW sync job",
		"disable_user":    "Disabled user",
		"enable_user":     "Enabled user",
		"delete_user":     "Deleted user",
		"set_quota":       "Updated storage quota",
		"revoke_session":  "Revoked session",
		"enable_2fa":      "Enabled two-factor auth",
		"disable_2fa":     "Disabled two-factor auth",
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
	return template.HTML(svg)
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
