package webui

import (
	"bytes"
	"io/fs"
	"testing"
)

func TestLoginTemplateRenders(t *testing.T) {
	tmpl := parseTemplates()
	var buf bytes.Buffer
	err := tmpl.ExecuteTemplate(&buf, "login.html", map[string]interface{}{
		"CSRFToken":     "test",
		"AllowRegister": true,
	})
	if err != nil {
		t.Fatalf("ExecuteTemplate login.html: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("empty output")
	}
}

func TestEmbeddedTemplatePaths(t *testing.T) {
	matches, err := fs.Glob(templatesFS, "templates/*.html")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) == 0 {
		t.Fatal("expected top-level templates/*.html in embed")
	}
	foundLogin := false
	for _, m := range matches {
		if m == "templates/login.html" {
			foundLogin = true
		}
	}
	if !foundLogin {
		t.Fatalf("login.html not embedded; got %v", matches)
	}
}
