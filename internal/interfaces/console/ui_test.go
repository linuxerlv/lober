package console

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestFrontendSPAAndAPIBoundary(t *testing.T) {
	var files fs.FS = fstest.MapFS{"index.html": {Data: []byte("<main>console</main>")}, "assets/app.js": {Data: []byte("window.ready=true")}}
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "api-404", 404) })
	h := frontendHandler(files, api)
	for _, tc := range []struct {
		path   string
		status int
		body   string
	}{{"/tasks/new", 200, "console"}, {"/assets/app.js", 200, "window.ready"}, {"/assets/missing.js", 404, ""}, {"/console/v1/missing", 404, "api-404"}, {"/v1/boot", 404, "api-404"}, {"/openapi.json", 404, "api-404"}} {
		t.Run(tc.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
			if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.body) {
				t.Fatalf("%s: %d %s", tc.path, w.Code, w.Body.String())
			}
		})
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/tasks/new", nil))
	if w.Code != 404 {
		t.Fatal("write swallowed by SPA")
	}
}
