package httpapi

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"log"
	"net/http"

	"github.com/danielfrg/notebook/internal/vault"
	"github.com/danielfrg/notebook/internal/web"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

// Options carries the runtime settings the HTTP and MCP handlers share.
type Options struct {
	ReadOnly bool
	// MCPWrite is the raw value of NOTEBOOK_MCP_WRITE. It is effective only when
	// ReadOnly is false: the MCP write tools exist when both say so, and the code
	// that mounts MCP combines them.
	MCPWrite        bool
	MCPAllowedHosts []string
	MCPExclude      map[string][]string // vault name -> glob patterns
}

// NewHandler serves the vaults of reg over the HTTP API, the probes and the
// embedded frontend. The MCP endpoint is mounted here as it is built.
func NewHandler(assets fs.FS, reg *vault.Registry, options Options) http.Handler {
	mux := http.NewServeMux()
	config := huma.DefaultConfig("Notebook API", "0.1.0")
	config.Info.Description = "A request that changes something needs the header X-Notebook: 1, and a JSON body if it has one."
	config.OpenAPIPath = "/api/openapi"
	config.DocsPath = "/api/docs"
	config.SchemasPath = ""
	config.CreateHooks = nil
	api := humago.New(mux, config)
	registerVaults(api, reg, options)
	registerNotes(api, reg)
	registerSearch(api, reg)
	mux.HandleFunc("GET /api/vaults/{vault}/raw", raw(reg))

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok\n")
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		for _, v := range reg.All() {
			if err := v.Ping(); err != nil {
				// The error says where the vault is on disk: that is for the log.
				log.Printf("readyz: %v", err)
				http.Error(w, "Vault unavailable", http.StatusServiceUnavailable)
				return
			}
		}
		_, _ = io.WriteString(w, "ok\n")
	})
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		web.Index(w, r, assets, http.StatusOK)
	})
	static := web.Handler(assets)
	mux.Handle("GET /assets/", static)
	mux.Handle("GET /favicon.svg", static)
	return guard(options, mux)
}

// internalFailure is what a client is told when a request fails for a reason of
// the server's: what failed is for the log.
const internalFailure = "Could not access the vault. Try again."

// problem turns the error of a request into the answer for the client. Only what
// is known to be safe is relayed: the errors of a vault that say what is wrong
// with the request, with their own text and not that of what wraps them, and the
// errors that already are an answer. Any other error can hold the absolute path
// of the vault, as the errors of the file system do, so it is logged and the
// client is told nothing of its cause.
func problem(err error) huma.StatusError {
	var answer huma.StatusError
	if errors.As(err, &answer) {
		return answer
	}
	// The one that tells what to do about it: the revision that is on disk now.
	var conflict *vault.ConflictError
	if errors.As(err, &conflict) {
		return huma.NewError(http.StatusConflict, vault.ErrConflict.Error(), &huma.ErrorDetail{
			Message:  "revision of the note on disk",
			Location: "body.revision",
			Value:    conflict.Current,
		})
	}
	for _, known := range []struct {
		err    error
		status int
	}{
		{vault.ErrNotFound, http.StatusNotFound},
		{vault.ErrInvalidPath, http.StatusBadRequest},
		{vault.ErrEmptyQuery, http.StatusBadRequest},
		{vault.ErrExists, http.StatusConflict},
		{vault.ErrConflict, http.StatusConflict},
		{vault.ErrTooLarge, http.StatusRequestEntityTooLarge},
		{vault.ErrNotNote, http.StatusUnsupportedMediaType},
		{vault.ErrReadOnly, http.StatusMethodNotAllowed},
	} {
		if errors.Is(err, known.err) {
			return huma.NewError(known.status, known.err.Error())
		}
	}
	// A client that left cancels what it asked for, and that is no failure.
	if !errors.Is(err, context.Canceled) {
		log.Printf("request failed: %v", err)
	}
	return huma.Error500InternalServerError(internalFailure)
}
