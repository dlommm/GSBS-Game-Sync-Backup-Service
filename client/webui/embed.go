package clientwebui

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed templates/*.html templates/**/*.html
var templatesFS embed.FS

//go:embed static/*.css static/*.js static/*.png static/fonts/*.woff2
var staticFS embed.FS

// StaticFiles returns an http.FileSystem for serving embedded static assets.
func StaticFiles() http.FileSystem {
	sub, _ := fs.Sub(staticFS, "static")
	return http.FS(sub)
}

// StaticFS returns the embedded static tree rooted at static/.
func StaticFS() fs.FS {
	sub, _ := fs.Sub(staticFS, "static")
	return sub
}
