package console

import (
	"bytes"
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

// all: includes Vite's underscore-prefixed runtime chunks.
//
//go:embed all:ui/dist
var frontendFiles embed.FS

// WithFrontend serves the built console with SPA history fallback. Operational
// API paths remain under the API handler, including unknown API routes.
func WithFrontend(api http.Handler) http.Handler {
	files, err := fs.Sub(frontendFiles, "ui/dist")
	if err != nil {
		panic(err)
	}
	return frontendHandler(files, api)
}

func frontendHandler(files fs.FS, api http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if (r.Method != http.MethodGet && r.Method != http.MethodHead) || strings.HasPrefix(r.URL.Path, "/console/") || strings.HasPrefix(r.URL.Path, "/v1/") || r.URL.Path == "/openapi.json" {
			api.ServeHTTP(w, r)
			return
		}
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" || name == "." {
			name = "index.html"
		}
		data, err := fs.ReadFile(files, name)
		if err != nil {
			if path.Ext(name) != "" || strings.HasPrefix(name, "assets/") || strings.HasPrefix(name, "_app/") {
				http.NotFound(w, r)
				return
			}
			name = "index.html"
			data, err = fs.ReadFile(files, name)
			if err != nil {
				http.NotFound(w, r)
				return
			}
		}
		if name == "index.html" || strings.HasPrefix(name, "_app/") {
			w.Header().Set("Cache-Control", "no-cache")
		}
		http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
	})
}
