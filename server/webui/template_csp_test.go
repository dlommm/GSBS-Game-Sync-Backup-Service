package webui

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

// The CSP is 'self' only (no 'unsafe-inline') for both scripts and styles.
// These checks fail the build if any template reintroduces an inline <script>,
// an on*= event handler, or a style="" attribute — the three things that would
// force 'unsafe-inline' back on. New behavior goes in static/app.js or
// static/admin.js with data-* wiring; dynamic widths use data-width-pct.
var (
	reInlineScript = regexp.MustCompile(`(?i)<script(?:\s[^>]*)?>`) // any <script> without a src ends up here after the src filter
	reOnHandler    = regexp.MustCompile(`(?i)\son[a-z]+\s*=`)
	reStyleAttr    = regexp.MustCompile(`(?i)\sstyle\s*=`)
)

func TestTemplatesHaveNoInlineScriptsOrStyles(t *testing.T) {
	err := fs.WalkDir(templatesFS, "templates", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".html") {
			return err
		}
		data, rerr := fs.ReadFile(templatesFS, path)
		if rerr != nil {
			t.Fatalf("read %s: %v", path, rerr)
		}
		content := string(data)

		// Inline <script> blocks: allowed only as <script src="...">.
		for _, m := range reInlineScript.FindAllString(content, -1) {
			if !strings.Contains(strings.ToLower(m), "src=") {
				t.Errorf("%s: inline <script> found (%q) — move JS to static/app.js or admin.js", path, m)
			}
		}
		// on*= event handlers.
		for _, m := range reOnHandler.FindAllString(content, -1) {
			t.Errorf("%s: inline event handler found (%q) — use data-* attributes + delegated listeners", path, strings.TrimSpace(m))
		}
		// style="" attributes.
		if loc := reStyleAttr.FindString(content); loc != "" {
			t.Errorf("%s: style=\"\" attribute found — use a utility class or data-width-pct", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
