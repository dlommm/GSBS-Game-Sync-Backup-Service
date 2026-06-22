package clientwebui_test

import (
	"net/http/httptest"
	"testing"

	clientwebui "github.com/gsbs/gsbs/client/webui"
	"github.com/gsbs/gsbs/pkg/logview"
)

var pageNames = []string{"setup", "dashboard", "games", "quick_actions", "help", "about", "open_log", "logs", "settings"}

func TestParseTemplates(t *testing.T) {
	_, err := clientwebui.ParseTemplates()
	if err != nil {
		t.Fatalf("ParseTemplates: %v", err)
	}
}

func TestRenderPages(t *testing.T) {
	for _, name := range pageNames {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			if name == "logs" {
				clientwebui.RenderLogsPage(rec, clientwebui.LogsPageData{
					PageData: clientwebui.PageData{
						NavActive: "logs",
						Title:     "Test",
					},
					Query: logview.Query{Level: "all", Limit: 200, Component: "all"},
				})
			} else if name == "settings" {
				clientwebui.RenderSettingsPage(rec, clientwebui.SettingsPageData{
					PageData:        clientwebui.PageData{NavActive: "settings", Title: "Test"},
					SyncInterval:    "5m",
					ConflictPolicy:  "last_write_wins",
					ManifestInclude: "both",
				})
			} else {
				clientwebui.RenderPage(rec, name, clientwebui.PageData{
					NavActive: name,
					Title:     "Test",
					Version:   "v1.0.0",
					GOOS:      "linux",
					GOARCH:    "amd64",
				})
			}
			if rec.Code >= 500 {
				t.Errorf("render %s: status %d body: %s", name, rec.Code, rec.Body.String())
			}
		})
	}
}
