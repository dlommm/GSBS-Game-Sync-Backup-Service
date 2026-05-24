package webui

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed templates/*.html templates/**/*.html
var templatesFS embed.FS

//go:embed static/*.css static/*.js static/ext/*.js static/*.png
var staticFS embed.FS

// StaticFiles returns an http.FileSystem for serving embedded static assets (CSS, etc.).
func StaticFiles() http.FileSystem {
	sub, _ := fs.Sub(staticFS, "static")
	return http.FS(sub)
}
