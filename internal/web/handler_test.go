package web

import (
	"mime"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

const immutable = "public, max-age=31536000, immutable"

func TestIndexServesHTML(t *testing.T) {
	const page = "<!doctype html><title>Notebook</title>"
	assets := fstest.MapFS{"index.html": {Data: []byte(page)}}

	w := httptest.NewRecorder()
	Index(w, httptest.NewRequest(http.MethodGet, "/", nil), assets, http.StatusOK)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if media, _, err := mime.ParseMediaType(w.Header().Get("Content-Type")); err != nil || media != "text/html" {
		t.Errorf("content type = %q, %v; want text/html", w.Header().Get("Content-Type"), err)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", got)
	}
	if w.Body.String() != page {
		t.Errorf("body = %q, want %q", w.Body.String(), page)
	}

	// HEAD answers with the same headers and no body.
	w = httptest.NewRecorder()
	Index(w, httptest.NewRequest(http.MethodHead, "/", nil), assets, http.StatusOK)
	if w.Code != http.StatusOK || w.Body.Len() != 0 || w.Header().Get("Cache-Control") != "no-cache" {
		t.Errorf("HEAD = %d, %q body, Cache-Control %q", w.Code, w.Body.String(), w.Header().Get("Cache-Control"))
	}

	// Callers choose the status the page is served with.
	w = httptest.NewRecorder()
	Index(w, httptest.NewRequest(http.MethodGet, "/", nil), assets, http.StatusNotFound)
	if w.Code != http.StatusNotFound || w.Body.String() != page {
		t.Errorf("status = %d, body = %q; want 404 with the page", w.Code, w.Body.String())
	}
}

func TestIndexWithoutFrontend(t *testing.T) {
	// An empty filesystem and the .keep placeholder of an unbuilt checkout.
	for _, assets := range []fstest.MapFS{{}, {".keep": {}}} {
		w := httptest.NewRecorder()
		Index(w, httptest.NewRequest(http.MethodGet, "/", nil), assets, http.StatusOK)
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("status = %d, want 503", w.Code)
		}
		if !strings.Contains(w.Body.String(), "just build") {
			t.Errorf("body = %q, want it to mention just build", w.Body.String())
		}
	}
}

func TestAssetHandler(t *testing.T) {
	assets := fstest.MapFS{
		"index.html":          {Data: []byte("index")},
		"favicon.svg":         {Data: []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)},
		"other":               {Data: []byte("other")},
		"assets/app.js":       {Data: []byte("console.log('app')")},
		"assets/sub/chunk.js": {Data: []byte("console.log('chunk')")},
		"assets/.hidden":      {Data: []byte("secret")},
		"assets/.git/config":  {Data: []byte("secret")},
	}
	h := Handler(assets)
	for _, tc := range []struct {
		path   string
		status int
		cache  string // expected Cache-Control on a 200
		body   string // expected body on a 200
	}{
		{"/assets/app.js", 200, immutable, "console.log('app')"},
		{"/assets/sub/chunk.js", 200, immutable, "console.log('chunk')"},
		{"/favicon.svg", 200, "no-cache", `<svg xmlns="http://www.w3.org/2000/svg"/>`},
		{"/assets/.hidden", 404, "", ""},
		{"/assets/.git/config", 404, "", ""},
		{"/assets/../index.html", 404, "", ""},
		{"/assets/%2e%2e/index.html", 404, "", ""},
		{"/assets/sub", 404, "", ""}, // a directory is never listed
		{"/assets/", 404, "", ""},
		{"/assets/missing.js", 404, "", ""},
		{"/other", 404, "", ""},
		{"/index.html", 404, "", ""},
	} {
		t.Run(tc.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if w.Code != tc.status {
				t.Fatalf("status = %d, want %d", w.Code, tc.status)
			}
			if strings.Contains(w.Body.String(), "secret") {
				t.Fatalf("body leaks a hidden file: %q", w.Body.String())
			}
			if tc.status != http.StatusOK {
				// A missing file must never be cached as immutable.
				if strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
					t.Errorf("Cache-Control = %q on a %d", w.Header().Get("Cache-Control"), w.Code)
				}
				return
			}
			if got := w.Header().Get("Cache-Control"); got != tc.cache {
				t.Errorf("Cache-Control = %q, want %q", got, tc.cache)
			}
			if w.Body.String() != tc.body {
				t.Errorf("body = %q, want %q", w.Body.String(), tc.body)
			}
		})
	}
}

func TestAssetContentTypesAndHead(t *testing.T) {
	assets := fstest.MapFS{
		"favicon.svg":    {Data: []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)},
		"assets/app.js":  {Data: []byte("console.log('app')")},
		"assets/app.css": {Data: []byte("body{}")},
	}
	h := Handler(assets)
	for path, want := range map[string]string{
		"/assets/app.js":  "text/javascript",
		"/assets/app.css": "text/css",
		"/favicon.svg":    "image/svg+xml",
	} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(method, path, nil))
			media, _, err := mime.ParseMediaType(w.Header().Get("Content-Type"))
			if w.Code != http.StatusOK || err != nil || media != want {
				t.Errorf("%s %s = %d, %q; want 200, %q", method, path, w.Code, w.Header().Get("Content-Type"), want)
			}
			if method == http.MethodHead && w.Body.Len() != 0 {
				t.Errorf("HEAD %s returned a body", path)
			}
		}
	}
}

func TestAssetsEmbedPlaceholder(t *testing.T) {
	assets, err := Assets()
	if err != nil {
		t.Fatal(err)
	}
	// dist always embeds, so the app starts (and answers 503) before the first
	// frontend build.
	if _, err := assets.Open(".keep"); err != nil {
		t.Errorf("embedded dist has no .keep placeholder: %v", err)
	}
}
