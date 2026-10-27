package httpapi

import (
	"io"
	"io/fs"
	"net/http"

	"github.com/danielfrg/notebook/internal/web"
)

// Options carries the runtime settings the HTTP and MCP handlers share.
type Options struct {
	ReadOnly        bool
	MCPWrite        bool
	MCPAllowedHosts []string
	MCPExclude      map[string][]string // vault name -> glob patterns
}

// NewHandler serves the probes and the embedded frontend. The vault API and
// the MCP endpoint are mounted here as they are built.
func NewHandler(assets fs.FS, options Options) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		_, _ = io.WriteString(w, "ok\n")
	})
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		web.Index(w, r, assets, http.StatusOK)
	})
	static := web.Handler(assets)
	mux.Handle("GET /assets/", static)
	mux.Handle("GET /favicon.svg", static)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Referrer-Policy", "no-referrer")
		mux.ServeHTTP(w, r)
	})
}
