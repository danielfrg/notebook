package web

import (
	"io/fs"
	"net/http"
	"strings"
)

// Index serves the single page app shell. Without a frontend build it answers
// 503 with the command that creates one.
func Index(w http.ResponseWriter, r *http.Request, assets fs.FS, status int) {
	data, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		http.Error(w, "Frontend is not built. Run just frontend for development or just build for the embedded app.", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
}

// Handler serves the content-hashed files under /assets/ with an immutable
// cache policy and the favicon. It never lists directories, never serves a
// path with a dot-prefixed segment, and answers 404 for everything else.
func Handler(assets fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cache := "public, max-age=31536000, immutable"
		switch {
		case r.URL.Path == "/favicon.svg":
			// The favicon is not content-hashed, so it must revalidate.
			cache = "no-cache"
		case !strings.HasPrefix(r.URL.Path, "/assets/"):
			http.NotFound(w, r)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/")
		for _, part := range strings.Split(name, "/") {
			if strings.HasPrefix(part, ".") {
				http.NotFound(w, r)
				return
			}
		}
		info, err := fs.Stat(assets, name)
		if err != nil || !info.Mode().IsRegular() {
			http.NotFound(w, r)
			return
		}
		// Set only once the file exists: a 404 must never be cached.
		w.Header().Set("Cache-Control", cache)
		http.ServeFileFS(w, r, assets, name)
	})
}
