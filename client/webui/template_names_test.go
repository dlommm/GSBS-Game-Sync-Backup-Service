package clientwebui_test

import (
	"net/http/httptest"
	"testing"

	clientwebui "github.com/gsbs/gsbs/client/webui"
)

var pageNames = []string{"setup", "dashboard", "games", "quick_actions", "help", "about", "open_log"}

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
			clientwebui.RenderPage(rec, name, clientwebui.PageData{
				NavActive: name,
				Title:     "Test",
				Version:   "v1.0.0",
				GOOS:      "linux",
				GOARCH:    "amd64",
			})
			if rec.Code >= 500 {
				t.Errorf("render %s: status %d body: %s", name, rec.Code, rec.Body.String())
			}
		})
	}
}
