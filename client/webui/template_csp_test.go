package clientwebui_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The loopback WebUI ships a strict Content-Security-Policy with no
// 'unsafe-inline' (client/setup_server.go). These checks keep the templates
// compatible: no inline <script> blocks, no on*= handlers, no style=""
// attributes — the same guarantees server/webui/template_csp_test.go enforces.
var (
	inlineScriptRe = regexp.MustCompile(`(?i)<script(\s[^>]*)?>`)
	scriptSrcRe    = regexp.MustCompile(`(?i)src\s*=`)
	onHandlerRe    = regexp.MustCompile(`(?i)\son[a-z]+\s*=`)
	styleAttrRe    = regexp.MustCompile(`(?i)\sstyle\s*=`)
)

func templateFiles(t *testing.T) []string {
	t.Helper()
	var files []string
	for _, glob := range []string{"templates/*.html", "templates/partials/*.html"} {
		matches, err := filepath.Glob(glob)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, matches...)
	}
	if len(files) == 0 {
		t.Fatal("no template files found")
	}
	return files
}

func TestTemplatesAreCSPClean(t *testing.T) {
	for _, file := range templateFiles(t) {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		content := string(data)

		for _, m := range inlineScriptRe.FindAllString(content, -1) {
			if !scriptSrcRe.MatchString(m) {
				t.Errorf("%s: inline <script> without src= (%q) — move code to static/app.js", file, m)
			}
		}
		for _, line := range strings.Split(content, "\n") {
			if onHandlerRe.MatchString(line) {
				t.Errorf("%s: inline event handler: %q — use data-* wiring in app.js", file, strings.TrimSpace(line))
			}
			if styleAttrRe.MatchString(line) {
				t.Errorf("%s: inline style attribute: %q — use a class in input.css", file, strings.TrimSpace(line))
			}
		}
	}
}
