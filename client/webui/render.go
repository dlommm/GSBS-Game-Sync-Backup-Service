package clientwebui

import (
	"fmt"
	"html/template"
	"net/http"
	"time"

	"github.com/gsbs/gsbs/pkg/logview"
)

// PageData holds the data available to all client WebUI templates.
type PageData struct {
	NavActive       string // "setup", "dashboard", "games", "quick-actions", "help", "about", "logs"
	Title           string
	ServerURL       string
	Version         string
	GOOS            string
	GOARCH          string
	Error           string
	Success         string
	SetupServerURL  string
	SetupClientName string
	SetupDone       bool
	LogPath         string
}

// LogsPageData holds data for the client logs viewer.
type LogsPageData struct {
	PageData
	Entries          []logview.Entry
	LogSourcePath    string
	LogSourcePresent bool
	LogSourceInfo    string
	Level            string
	Query            string
	Limit            int
	AutoRefresh      bool
	RefreshSeconds   int
}

var tmpl *template.Template

func init() {
	var err error
	tmpl, err = ParseTemplates()
	if err != nil {
		panic(fmt.Sprintf("clientwebui: init templates: %v", err))
	}
}

func newTemplateFuncs() template.FuncMap {
	return template.FuncMap{
		"formatTime": formatTime,
	}
}

// ParseTemplates parses all embedded templates and returns a template set.
func ParseTemplates() (*template.Template, error) {
	t := template.New("")
	t = t.Funcs(newTemplateFuncs())
	return t.ParseFS(templatesFS, "templates/*.html", "templates/**/*.html")
}

// formatTime formats an RFC3339 timestamp for display.
func formatTime(s string) string {
	if s == "" {
		return "\u2014"
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

// Render renders the named template from the given template set to w.
func Render(w http.ResponseWriter, t *template.Template, name string, data PageData) {
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
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "logs", data); err != nil {
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
