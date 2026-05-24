package webui

import (
	"bytes"
	"fmt"
	"html/template"
	"log"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gsbs/gsbs/pkg/types"
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
	Restored  bool // dashboard save restore flash
	Deleted   bool // dashboard save delete flash
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

type adminStats struct {
	UserCount     int
	ClientCount   int
	SaveCount     int
	ManifestCount int
	TotalBytes    int64
}

type adminOverviewData struct {
	PageData
	Stats           adminStats
	StatsSnapshots  []store.StatsSnapshotRow
	SSEClients      int
	AllowRegister   bool
	MaxStorageBytes int64
	ReadOnly        bool
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
	Fetches          []store.ManifestFetchRow
	AuditLog         []store.AuditRow
	StatsSnapshots   []store.StatsSnapshotRow
	RecentJobs       []store.JobRun
	JobRunning       bool
	JobProgressPages int
}

func newTemplateFuncs(t *template.Template) template.FuncMap {
	return template.FuncMap{
		"formatTime":       formatTime,
		"formatBytes":      formatBytes,
		"truncate":         truncate,
		"formatDuration":   formatDuration,
		"urlquery":         url.QueryEscape,
		"auditLabel":       auditLabel,
		"chartLineSVG":     chartLineSVG,
		"percent":          percent,
		"minInt":           minInt,
		"quotaBarClass":    quotaBarClass,
		"clientBarWidth":   clientBarWidth,
		"renderPageBlock":  renderPageBlock(t),
		"add":              func(a, b int) int { return a + b },
		"sub":              func(a, b int) int { return a - b },
	}
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
		log.Printf("webui template %s: %v", name, err)
	}
}

func auditLabel(action string) string {
	labels := map[string]string{
		"restore_version": "Restored save version",
		"delete_save":     "Deleted save",
		"revoke_client":   "Revoked client token",
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
