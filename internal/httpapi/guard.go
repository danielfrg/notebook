package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/danielgtaylor/huma/v2"
)

// maxRequestBytes is the most that a request may send.
const maxRequestBytes = 4 << 20

// guard is the outermost handler. It sets the headers that responses carry, and
// holds back the requests that would change something unless they are the kind
// that the app sends (see admit).
func guard(options Options, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := w.Header()
		header.Set("Referrer-Policy", "no-referrer")
		switch {
		case r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/"):
			header.Set("Cache-Control", "no-store")
			if changes(r.Method) && !admit(w, r, options.ReadOnly) {
				return
			}
		case r.URL.Path == "/healthz" || r.URL.Path == "/readyz":
			header.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

// changes reports whether a request of this method may change something.
func changes(method string) bool {
	return method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions
}

// sendsBody reports whether a request carries a body, and so needs a JSON content
// type. POST, PUT and PATCH do even when the body is empty. Any other method
// does only if it announces one (a length that is not 0, or chunks), so a DELETE
// without a body needs no content type.
func sendsBody(r *http.Request) bool {
	switch r.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		return true
	}
	return r.ContentLength != 0
}

// admit checks a request that would change something and reports whether it may
// go on; if not, it has already answered. A read-only instance refuses all of
// them first. The others must be requests that a page of another site cannot
// make: they carry the header X-Notebook, which a form cannot send, they come
// from this host if they say where from, and a body is JSON, which a form cannot
// send either.
//
// The body is read here so that every operation gets the same limits: at most
// 4 MiB, and valid UTF-8. The JSON decoder would otherwise turn the bytes that
// are not into U+FFFD, and a note, or the name of one, would be saved changed.
func admit(w http.ResponseWriter, r *http.Request, readOnly bool) bool {
	refuse := func(err huma.StatusError) bool {
		writeProblem(w, err)
		return false
	}
	if readOnly {
		w.Header().Set("Allow", "GET, HEAD")
		return refuse(huma.Error405MethodNotAllowed("Notebook is read-only"))
	}
	if r.Header.Get("X-Notebook") != "1" {
		return refuse(huma.Error403Forbidden("X-Notebook header is required"))
	}
	if origin := r.Header.Get("Origin"); origin != "" && !sameHost(origin, r.Host) {
		return refuse(huma.Error403Forbidden("Origin does not match the request host"))
	}
	if !sendsBody(r) {
		return true
	}
	if media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || media != "application/json" {
		return refuse(huma.NewError(http.StatusUnsupportedMediaType, "Use application/json"))
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBytes))
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		return refuse(huma.NewError(http.StatusRequestEntityTooLarge, "Request exceeds 4 MiB"))
	case err != nil:
		return refuse(huma.Error400BadRequest("Could not read the request body"))
	case !utf8.Valid(body):
		return refuse(huma.Error400BadRequest("Request body must be UTF-8"))
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	return true
}

// sameHost reports whether an Origin header names the host that was asked.
func sameHost(origin, host string) bool {
	u, err := url.Parse(origin)
	return err == nil &&
		u.Host != "" && strings.EqualFold(u.Host, host) &&
		(u.Scheme == "http" || u.Scheme == "https") &&
		u.User == nil && u.RawQuery == "" && u.Fragment == "" &&
		(u.Path == "" || u.Path == "/")
}

// writeProblem answers with err as the problem document (RFC 9457) that the API
// gives for all its errors, the ones of Huma included.
func writeProblem(w http.ResponseWriter, err huma.StatusError) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(err.GetStatus())
	_ = json.NewEncoder(w).Encode(err)
}
