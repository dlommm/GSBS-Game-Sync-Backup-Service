package clientwebui

import (
	"bytes"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gsbs/gsbs/pkg/logview"
)

// PageData holds the data available to all client WebUI templates.
type PageData struct {
	PageName        string // template prefix for layout blocks; set by the Render helpers
	NavActive       string // "setup", "dashboard", "games", "quick-actions", "help", "about", "logs", "insights"
	Title           string
	ServerURL       string
	Version         string
	BuildDate       string
	Commit          string
	GOOS            string
	GOARCH          string
	Error           string
	Success         string
	SetupServerURL  string
	SetupClientName string
	SetupDone       bool
	LogPath         string
}

// PolicyOverride is one per-game conflict-policy override row (v5.2, FIX-4).
type PolicyOverride struct {
	GameID string
	Title  string // display title when known; the UI falls back to GameID
	Policy string
}

// SettingsPageData holds the editable client settings shown on the Settings page.
type SettingsPageData struct {
	PageData
	SyncInterval        string
	ConflictPolicy      string
	ManifestInclude     string
	MaxSyncKbps         int
	BackupOnPull        bool
	UseCompression      bool
	SkipSyncWhenMetered bool
	// MeteredSupported is false on platforms without metered-connection
	// detection (everything except Windows) — the checkbox renders disabled
	// there so the setting doesn't pretend to work.
	MeteredSupported  bool
	NotificationLevel string // all / errors / silent
	NotifyPerUpload   bool
	QuietHoursStart   string // "22:30" or ""
	QuietHoursEnd     string
	PolicyOverrides   []PolicyOverride
	// End-to-end encryption onboarding (v5.4). The passphrase VALUE is never
	// rendered — only whether one is stored.
	EncryptionKnown          bool // false when the server was unreachable
	EncryptionAccountEnabled bool
	PassphraseSet            bool
}

// LogsPageData holds data for the client logs viewer.
type LogsPageData struct {
	PageData
	Entries []logview.Entry
	// Total is how many entries matched the filters within the tail window
	// (drives the Newer/Older pager).
	Total            int
	LogSourcePath    string
	LogSourcePresent bool
	LogSourceInfo    string
	Query            logview.Query
}

var tmpl *template.Template

func init() {
	var err error
	tmpl, err = ParseTemplates()
	if err != nil {
		panic(fmt.Sprintf("clientwebui: init templates: %v", err))
	}
}

// defaultDesign mirrors the server WebUI's GSBS_DESIGN default so a design
// choice can be provisioned for the local UI too (the account's synced
// choice, applied via SetAppearance, wins once fetched).
var defaultDesign = func() string {
	switch d := strings.TrimSpace(os.Getenv("GSBS_DESIGN")); d {
	case "hud", "crt", "hearth", "synth", "slate":
		return d
	}
	return ""
}()

// Appearance state the local pages render with: the account's synced color
// scheme + layout, set by the client after the cache load at startup and
// after every successful /api/account fetch.
var (
	appearanceMu sync.RWMutex
	appDesign    = defaultDesign
	appLayout    = ""
)

func clientValidDesign(d string) bool {
	switch d {
	case "", "hud", "crt", "hearth", "synth", "slate":
		return true
	}
	return false
}

func clientValidLayout(l string) bool {
	switch l {
	// "widgets" is accepted so the synced pref never bounces, but the local
	// pages render it as the default arrangement (different dashboard).
	case "", "topnav", "dense", "library", "widgets":
		return true
	}
	return false
}

// SetAppearance updates the design/layout the local pages render with.
// Invalid or unknown values fall back to the default look.
func SetAppearance(design, layout string) {
	if !clientValidDesign(design) {
		design = ""
	}
	if !clientValidLayout(layout) {
		layout = ""
	}
	appearanceMu.Lock()
	appDesign, appLayout = design, layout
	appearanceMu.Unlock()
}

func currentDesign() string {
	appearanceMu.RLock()
	defer appearanceMu.RUnlock()
	return appDesign
}

func currentLayout() string {
	appearanceMu.RLock()
	defer appearanceMu.RUnlock()
	return appLayout
}

func newTemplateFuncs(t *template.Template) template.FuncMap {
	return template.FuncMap{
		"formatTime":    formatTime,
		"formatBytes":   formatBytes,
		"add":           func(a, b int) int { return a + b },
		"pageBlock":     pageBlock(t),
		"designDefault": currentDesign,
		"layoutDefault": currentLayout,
	}
}

// pageBlock renders the page-specific "<page>_<block>" template if it exists
// (same layout mechanism as the server WebUI).
func pageBlock(t *template.Template) func(pageName, block string, data interface{}) (template.HTML, error) {
	return func(pageName, block string, data interface{}) (template.HTML, error) {
		name := pageName + "_" + block
		if t.Lookup(name) == nil {
			return "", nil
		}
		var buf bytes.Buffer
		if err := t.ExecuteTemplate(&buf, name, data); err != nil {
			return "", err
		}
		return template.HTML(buf.String()), nil //nolint:gosec // output of html/template execution, already escaped
	}
}

// ParseTemplates parses all embedded templates and returns a template set.
func ParseTemplates() (*template.Template, error) {
	t := template.New("")
	t = t.Funcs(newTemplateFuncs(t))
	return t.ParseFS(templatesFS, "templates/*.html", "templates/**/*.html")
}

// formatTime formats an RFC3339 timestamp for display.
func formatTime(s string) string {
	if s == "" {
		return "—"
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		// Plain log.Printf timestamps (2026/06/09 14:30:00) pass through.
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

// formatBytes renders a human-readable byte size.
func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// Render renders the named template from the given template set to w.
func Render(w http.ResponseWriter, t *template.Template, name string, data PageData) {
	data.PageName = name
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, "Template error: "+err.Error(), http.StatusInternalServerError)
	}
}

// RenderPage renders the named template from the package-level template set.
func RenderPage(w http.ResponseWriter, name string, data PageData) {
	Render(w, tmpl, name, data)
}

// RenderLogsPage renders the full logs viewer page.
func RenderLogsPage(w http.ResponseWriter, data LogsPageData) {
	data.PageName = "logs"
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "logs", data); err != nil {
		http.Error(w, "Template error: "+err.Error(), http.StatusInternalServerError)
	}
}

// RenderSettingsPage renders the full client settings page.
func RenderSettingsPage(w http.ResponseWriter, data SettingsPageData) {
	data.PageName = "settings"
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "settings", data); err != nil {
		http.Error(w, "Template error: "+err.Error(), http.StatusInternalServerError)
	}
}

// RenderPartial renders a partial template (e.g. logs table fragment).
func RenderPartial(w http.ResponseWriter, name string, data interface{}) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, "Template error: "+err.Error(), http.StatusInternalServerError)
	}
}

// ── Insights page (local sync analytics) ────────────────────────────────────

// InsightsBar is one labelled activity bar (Pct of the busiest day).
type InsightsBar struct {
	Label string
	Count int
	Pct   int
}

// InsightsGameRow is one matched game's sync state.
type InsightsGameRow struct {
	GameID       string
	Title        string
	Status       string // ok, conflict, pending, error, syncing
	Direction    string // push / pull
	LastSyncAt   string // RFC3339
	Conflict     bool
	FirstPathKey string // for the local Versions page link
}

// InsightsConflictRow is one recorded save conflict.
type InsightsConflictRow struct {
	GameID          string
	PathKey         string
	FilePath        string
	DetectedAt      string
	Policy          string
	LocalMtime      string
	ServerUpdatedAt string
}

// InsightsOutboxRow is one queued offline upload.
type InsightsOutboxRow struct {
	GameID      string
	PathKey     string
	CreatedAt   string
	Attempts    int
	SizeBytes   int64
	NextRetryAt string
}

// InsightsActivityRow is one line of the recent-activity feed.
type InsightsActivityRow struct {
	At        string // RFC3339
	Title     string
	PathKey   string
	Direction string // push / pull / queued
	OK        bool
	Detail    string
}

// InsightsStorageRow is one game's server-side storage footprint.
type InsightsStorageRow struct {
	Title     string
	SizeBytes int64
	Pct       int // of the largest game (bar width)
}

// InsightsPageData drives the client's local Insights page.
type InsightsPageData struct {
	PageData
	TotalCycles   int
	OKCycles      int
	SuccessPct    int
	SavesSynced7d int
	LastFailure   string
	DayBars       []InsightsBar
	Games         []InsightsGameRow
	Conflicts     []InsightsConflictRow
	Outbox        []InsightsOutboxRow
	Activity      []InsightsActivityRow
	// Resolving is set right after a conflict-resolution POST (it runs in the
	// background and can take a while).
	Resolving bool
	// Storage panel (server-side usage; hidden when unavailable).
	StorageGames []InsightsStorageRow
	UsageBytes   int64
	QuotaBytes   int64 // 0 = unlimited/unknown
	UsagePct     int
	StorageKnown bool
}

// ── Versions page (local save-version history + restore) ────────────────────

// VersionRow is one restorable server-side version of a save slot.
type VersionRow struct {
	Version     int
	UpdatedAt   string
	SizeBytes   int64
	ChangeBytes int64
	ClientName  string
	Current     bool
}

// VersionsPageData drives the local version-history page.
type VersionsPageData struct {
	PageData
	GameID       string
	PathKey      string
	GameTitle    string
	Versions     []VersionRow
	Restored     bool
	RestoreError string
	NotConnected bool
}

// RenderVersionsPage renders the local save-version history page.
func RenderVersionsPage(w http.ResponseWriter, data VersionsPageData) {
	data.PageName = "versions"
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "versions", data); err != nil {
		http.Error(w, "Template error: "+err.Error(), http.StatusInternalServerError)
	}
}

// RenderInsightsPage renders the local sync-insights page.
func RenderInsightsPage(w http.ResponseWriter, data InsightsPageData) {
	data.PageName = "insights"
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "insights", data); err != nil {
		http.Error(w, "Template error: "+err.Error(), http.StatusInternalServerError)
	}
}
