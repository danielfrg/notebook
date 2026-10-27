package mcp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// allowing is a handler that answers 204 to what protect lets through.
func allowing(hosts ...string) http.Handler {
	return protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }), hosts)
}

func TestHTTPPolicy(t *testing.T) {
	h := allowing("example.net")
	for _, tc := range []struct {
		host, origin string
		status       int
	}{
		// Localhost is always allowed, with or without a port, and a configured name too.
		{"localhost:8080", "", 204},
		{"127.0.0.1:8080", "", 204},
		{"[::1]:8080", "", 204},
		{"example.net", "", 204},
		{"Example.NET:8080", "", 204},
		{"evil.net", "", 403},
		// An Origin, if there is one, has to be this host.
		{"example.net", "https://example.net", 204},
		{"example.net", "https://evil.net", 403},
		{"example.net", "null", 403},
		{"example.net", "https://example.net/", 403},
		{"example.net", "https://user@example.net", 403},
	} {
		r := httptest.NewRequest("POST", "http://"+tc.host+"/mcp", strings.NewReader("{}"))
		r.Host = tc.host
		if tc.origin != "" {
			r.Header.Set("Origin", tc.origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Errorf("%+v got %d", tc, w.Code)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%+v: Cache-Control = %q, want no-store", tc, w.Header().Get("Cache-Control"))
		}
	}
}

func TestPolicyMethodsAndBodyLimit(t *testing.T) {
	h := transport(sdk.NewServer(&sdk.Implementation{Name: "test", Version: "1"}, nil), nil)
	// Only POST gets through protect, which does not leave it to what is behind it.
	for name, handler := range map[string]http.Handler{"protect": allowing(), "transport": h} {
		for _, method := range []string{"GET", "HEAD", "DELETE", "PUT", "OPTIONS"} {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest(method, "http://localhost/mcp", nil))
			if w.Code != 405 || w.Header().Get("Allow") != "POST" {
				t.Errorf("%s: method %s: %d, Allow %q; want 405 and POST", name, method, w.Code, w.Header().Get("Allow"))
			}
		}
	}

	post := func(size int) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://localhost/mcp", strings.NewReader(strings.Repeat(" ", size)))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/event-stream")
		r.ContentLength = -1 // as when chunked: the limit is on what is read
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := post(1<<20 + 1); w.Code != 413 {
		t.Errorf("a request of 1 MiB and a byte got %d: %s, want 413", w.Code, w.Body.String())
	}
	// Exactly 1 MiB is within the limit (it is not a request, only blanks).
	if w := post(1 << 20); w.Code == 413 {
		t.Errorf("a request of exactly 1 MiB got 413, want it read")
	}
}

func TestMalformedAuthoritiesAndOrigins(t *testing.T) {
	h := allowing("example.net")
	for _, tc := range []struct{ host, origin string }{
		{"example.net:", ""},
		{"example.net:99999", ""},
		{"example.net.evil", ""},
		{"user@example.net", ""},
		{"example.net", "https://example.net?"},
		{"example.net", "https://example.net#"},
		{"example.net:8080", "https://example.net"},
		{"example.net", ""}, // an Origin header that is there and empty
	} {
		r := httptest.NewRequest("POST", "http://localhost/mcp", strings.NewReader("{}"))
		r.Host = tc.host
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Errorf("accepted %+v: %d", tc, w.Code)
		}
	}

	r := httptest.NewRequest("POST", "http://localhost/mcp", nil)
	r.Header.Add("Origin", "http://localhost")
	r.Header.Add("Origin", "http://evil.net")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Errorf("several Origin headers got %d, want 403", w.Code)
	}
}

// The endpoint keeps no session and answers in JSON: a client that asks for the
// tools of a server gets them in one plain request and response.
func TestTransportIsStatelessJSON(t *testing.T) {
	h := transport(sdk.NewServer(&sdk.Implementation{Name: "test", Version: "1"}, nil), nil)
	r := httptest.NewRequest("POST", "http://localhost/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("tools/list got %d %q: %s, want 200 and application/json", w.Code, w.Header().Get("Content-Type"), w.Body.String())
	}
	if id := w.Header().Get("Mcp-Session-Id"); id != "" {
		t.Errorf("Mcp-Session-Id = %q, want no session", id)
	}
	if !strings.Contains(w.Body.String(), `"tools"`) {
		t.Errorf("body = %s, want the list of tools", w.Body.String())
	}
}
