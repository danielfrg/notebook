package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// newTestHandler is the only place the tests construct the handler.
func newTestHandler(assets fstest.MapFS) http.Handler {
	return NewHandler(assets, Options{})
}

func builtFrontend() fstest.MapFS {
	return fstest.MapFS{
		"index.html":    {Data: []byte("<main>Notebook</main>")},
		"favicon.svg":   {Data: []byte("<svg/>")},
		"assets/app.js": {Data: []byte("console.log('app')")},
	}
}

func do(h http.Handler, method, target string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(method, target, nil))
	return w
}

func TestHealthz(t *testing.T) {
	// The probe does not depend on the frontend build.
	for _, assets := range []fstest.MapFS{builtFrontend(), {}} {
		w := do(newTestHandler(assets), http.MethodGet, "/healthz")
		if w.Code != http.StatusOK || w.Body.String() != "ok\n" {
			t.Fatalf("GET /healthz = %d %q, want 200 \"ok\\n\"", w.Code, w.Body.String())
		}
		if got := w.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("Cache-Control = %q, want no-store", got)
		}
	}
}

func TestRoutes(t *testing.T) {
	h := newTestHandler(builtFrontend())
	for _, tc := range []struct {
		method, path string
		status       int
		body         string
	}{
		{"GET", "/", 200, "<main>Notebook</main>"},
		{"HEAD", "/", 200, ""},
		{"GET", "/assets/app.js", 200, "console.log('app')"},
		{"GET", "/favicon.svg", 200, "<svg/>"},
		{"GET", "/healthz", 200, "ok\n"},
		{"GET", "/index.html", 404, ""},
		{"GET", "/assets/missing.js", 404, ""},
		{"GET", "/anything/else", 404, ""},
		{"POST", "/", 405, ""},
		{"POST", "/healthz", 405, ""},
		{"POST", "/assets/app.js", 405, ""},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			w := do(h, tc.method, tc.path)
			if w.Code != tc.status {
				t.Fatalf("status = %d, want %d", w.Code, tc.status)
			}
			if tc.status == http.StatusOK && tc.method == http.MethodGet && w.Body.String() != tc.body {
				t.Errorf("body = %q, want %q", w.Body.String(), tc.body)
			}
			if got := w.Header().Get("Referrer-Policy"); got != "no-referrer" {
				t.Errorf("Referrer-Policy = %q, want no-referrer", got)
			}
		})
	}
}

func TestIndexAndAssetHeaders(t *testing.T) {
	h := newTestHandler(builtFrontend())
	if got := do(h, "GET", "/").Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("index Cache-Control = %q, want no-cache", got)
	}
	if got := do(h, "GET", "/assets/app.js").Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("asset Cache-Control = %q, want immutable", got)
	}
}

func TestIndexWithoutFrontend(t *testing.T) {
	w := do(newTestHandler(fstest.MapFS{}), "GET", "/")
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "just build") {
		t.Errorf("GET / = %d %q, want 503 mentioning just build", w.Code, w.Body.String())
	}
}
