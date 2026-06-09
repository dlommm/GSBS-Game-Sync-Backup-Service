package clientwebui

import (
	"fmt"
	"html/template"
	"net/http"
)

// PageData holds the data available to all client WebUI templates.
type PageData struct {
	NavActive       string // "setup", "dashboard", "games", "quick-actions", "help", "about"
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

var tmpl *template.Template

func init() {
	var err error
	tmpl, err = ParseTemplates()
	if err != nil {
		panic(fmt.Sprintf("clientwebui: init templates: %v", err))
	}
}

// ParseTemplates parses all embedded templates and returns a template set.
func ParseTemplates() (*template.Template, error) {
	t := template.New("")
	return t.ParseFS(templatesFS, "templates/*.html", "templates/**/*.html")
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
