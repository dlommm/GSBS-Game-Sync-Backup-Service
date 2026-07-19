package clientwebui

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSetAppearanceRendersAttrs(t *testing.T) {
	t.Cleanup(func() { SetAppearance("", "") })

	SetAppearance("synth", "dense")
	rec := httptest.NewRecorder()
	RenderPage(rec, "dashboard", PageData{Title: "Status"})
	body := rec.Body.String()
	if !strings.Contains(body, `data-design="synth"`) {
		t.Error("dashboard missing data-design after SetAppearance")
	}
	if !strings.Contains(body, `data-layout="dense"`) {
		t.Error("dashboard missing data-layout after SetAppearance")
	}

	// Unknown values fall back to the default look (no attributes).
	SetAppearance("evil", "wat")
	rec = httptest.NewRecorder()
	RenderPage(rec, "dashboard", PageData{Title: "Status"})
	body = rec.Body.String()
	if strings.Contains(body, "data-design=") || strings.Contains(body, "data-layout=") {
		t.Error("invalid appearance values must render as default (no attrs)")
	}
}
