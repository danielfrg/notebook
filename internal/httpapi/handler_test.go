package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/danielfrg/notebook/internal/vault"
)

// builtFrontend stands in for the Vite build that the binary embeds.
func builtFrontend() fstest.MapFS {
	return fstest.MapFS{
		"index.html":    {Data: []byte("<main>Notebook</main>")},
		"favicon.svg":   {Data: []byte("<svg/>")},
		"assets/app.js": {Data: []byte("console.log('app')")},
	}
}

// app is the handler under test, served over HTTP on two temporary vaults,
// notes and work. It is the only place the tests construct the handler.
type app struct {
	t      *testing.T
	url    string
	client *http.Client
	dirs   map[string]string // vault name -> its directory
}

type appConfig struct {
	assets  fs.FS
	options Options
}

func withAssets(assets fs.FS) func(*appConfig)     { return func(c *appConfig) { c.assets = assets } }
func withOptions(options Options) func(*appConfig) { return func(c *appConfig) { c.options = options } }

func newApp(t *testing.T, tweaks ...func(*appConfig)) *app {
	t.Helper()
	config := appConfig{assets: builtFrontend()}
	for _, tweak := range tweaks {
		tweak(&config)
	}
	a := &app{t: t, dirs: map[string]string{"notes": t.TempDir(), "work": t.TempDir()}}
	var vaults []*vault.Vault
	for _, name := range []string{"notes", "work"} {
		v, err := vault.Open(name, a.dirs[name], vault.Options{})
		if err != nil {
			t.Fatal(err)
		}
		vaults = append(vaults, v)
	}
	reg, err := vault.NewRegistry(vaults...)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewHandler(config.assets, reg, config.options))
	t.Cleanup(func() {
		server.Close()
		reg.Close()
	})
	a.url = server.URL
	a.client = server.Client()
	// Redirects are answers like any other here.
	a.client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return a
}

// mutating are the headers of a well-formed request that changes something.
var mutating = map[string]string{"X-Notebook": "1", "Content-Type": "application/json"}

// with returns headers changed by pairs of key and value; an empty value
// removes the header.
func with(base map[string]string, pairs ...string) map[string]string {
	headers := maps.Clone(base)
	for i := 0; i < len(pairs); i += 2 {
		if pairs[i+1] == "" {
			delete(headers, pairs[i])
		} else {
			headers[pairs[i]] = pairs[i+1]
		}
	}
	return headers
}

// response is an answer, read in full.
type response struct {
	Status int
	Header http.Header
	Body   string
}

// do sends one request as it is described: nothing is added to its headers.
func (a *app) do(method, path, body string, headers map[string]string) response {
	a.t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, a.url+path, reader)
	if err != nil {
		a.t.Fatal(err)
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	res, err := a.client.Do(req)
	if err != nil {
		a.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		a.t.Fatalf("%s %s: %v", method, path, err)
	}
	return response{Status: res.StatusCode, Header: res.Header, Body: string(data)}
}

func (a *app) get(path string) response { return a.do("GET", path, "", nil) }

// send makes a well-formed request that changes something. A string body is
// sent as it is, anything else as JSON, and nil is no body.
func (a *app) send(method, path string, body any) response {
	a.t.Helper()
	var data string
	switch body := body.(type) {
	case nil:
	case string:
		data = body
	default:
		encoded, err := json.Marshal(body)
		if err != nil {
			a.t.Fatal(err)
		}
		data = string(encoded)
	}
	return a.do(method, path, data, mutating)
}

// want fails the test unless the status is the one expected.
func (r response) want(t *testing.T, status int) response {
	t.Helper()
	if r.Status != status {
		t.Fatalf("status = %d, want %d; body: %.300s", r.Status, status, r.Body)
	}
	return r
}

func (r response) decode(t *testing.T, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(r.Body), v); err != nil {
		t.Fatalf("body is not the JSON expected: %v; body: %.300s", err, r.Body)
	}
}

// seed writes a file of a vault, and the folders above it, the way another
// program would.
func (a *app) seed(name, rel, content string) {
	a.t.Helper()
	p := filepath.Join(a.dirs[name], filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		a.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		a.t.Fatal(err)
	}
}

// onDisk returns the file of a vault as it is on disk; it is "<missing>" if
// there is none.
func (a *app) onDisk(name, rel string) string {
	a.t.Helper()
	data, err := os.ReadFile(filepath.Join(a.dirs[name], filepath.FromSlash(rel)))
	if errors.Is(err, fs.ErrNotExist) {
		return "<missing>"
	}
	if err != nil {
		a.t.Fatal(err)
	}
	return string(data)
}

// The JSON the API is expected to send, as the frontend reads it. These are the
// tests' own types, so the names of the fields are checked and not only the
// types that produce them.
type (
	noteJSON struct {
		Path     string `json:"path"`
		Content  string `json:"content"`
		Revision string `json:"revision"`
		Size     int64  `json:"size"`
		Modified string `json:"modified"`
	}
	entryJSON struct {
		Path     string `json:"path"`
		Size     int64  `json:"size"`
		Modified string `json:"modified"`
	}
	listingJSON struct {
		Notes       []entryJSON `json:"notes"`
		Assets      []entryJSON `json:"assets"`
		Folders     []string    `json:"folders"`
		Fingerprint string      `json:"fingerprint"`
	}
)

// paths returns the paths of entries.
func paths(entries []entryJSON) []string {
	out := []string{}
	for _, e := range entries {
		out = append(out, e.Path)
	}
	return out
}

// js returns v as JSON, for the body of a request.
func js(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(data)
}

// enc escapes a path for a query the way encodeURIComponent does.
func enc(s string) string { return strings.ReplaceAll(url.QueryEscape(s), "+", "%20") }

// api is the URL of an endpoint of a vault that takes a path.
func api(vaultName, endpoint, path string) string {
	return "/api/vaults/" + vaultName + "/" + endpoint + "?path=" + enc(path)
}

// request is a request to be sent with the headers of a well-formed one.
type request struct{ method, path, body string }

// mutations is one request to each endpoint that changes a vault. The one to
// the note passes rev as the revision it saves over.
func mutations(vaultName, rev string) []request {
	return []request{
		{"PUT", api(vaultName, "note", "a.md"), `{"content":"# B\n","revision":"` + rev + `"}`},
		{"POST", "/api/vaults/" + vaultName + "/notes", `{"path":"new.md","content":"x"}`},
		{"POST", "/api/vaults/" + vaultName + "/folders", `{"path":"folder"}`},
		{"POST", "/api/vaults/" + vaultName + "/move", `{"from":"a.md","to":"b.md"}`},
		{"DELETE", api(vaultName, "entry", "a.md"), ""},
	}
}

// syncBuffer collects what the handler logs while the test reads it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// captureLog collects the log of the test until it ends.
func captureLog(t *testing.T) *syncBuffer {
	t.Helper()
	logs := &syncBuffer{}
	log.SetOutput(logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return logs
}

func TestHealthz(t *testing.T) {
	// The probe does not depend on the frontend build.
	for _, assets := range []fstest.MapFS{builtFrontend(), {}} {
		res := newApp(t, withAssets(assets)).get("/healthz")
		if res.Status != http.StatusOK || res.Body != "ok\n" {
			t.Fatalf("GET /healthz = %d %q, want 200 \"ok\\n\"", res.Status, res.Body)
		}
		if got := res.Header.Get("Cache-Control"); got != "no-store" {
			t.Errorf("Cache-Control = %q, want no-store", got)
		}
	}
}

func TestRoutes(t *testing.T) {
	a := newApp(t)
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
		{"GET", "/readyz", 200, "ok\n"},
		{"GET", "/index.html", 404, ""},
		{"GET", "/assets/missing.js", 404, ""},
		{"GET", "/anything/else", 404, ""},
		{"POST", "/", 405, ""},
		{"POST", "/healthz", 405, ""},
		{"POST", "/readyz", 405, ""},
		{"POST", "/assets/app.js", 405, ""},
		// The API is under /api, and only what is registered there answers.
		{"GET", "/api/vaults", 200, ""},
		{"GET", "/api", 404, ""},
		{"GET", "/api/", 404, ""},
		{"GET", "/api/nope", 404, ""},
		{"GET", "/api/vaults/notes", 404, ""},
		{"GET", "/apiary", 404, ""},
		{"GET", "/vaults", 404, ""},
		{"OPTIONS", "/api/vaults", 405, ""},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			res := a.do(tc.method, tc.path, "", nil)
			if res.Status != tc.status {
				t.Fatalf("status = %d, want %d", res.Status, tc.status)
			}
			if tc.method == http.MethodGet && tc.status == http.StatusOK && tc.body != "" && res.Body != tc.body {
				t.Errorf("body = %q, want %q", res.Body, tc.body)
			}
			if got := res.Header.Get("Referrer-Policy"); got != "no-referrer" {
				t.Errorf("Referrer-Policy = %q, want no-referrer", got)
			}
		})
	}
}

func TestIndexAndAssetHeaders(t *testing.T) {
	a := newApp(t)
	if got := a.get("/").Header.Get("Cache-Control"); got != "no-cache" {
		t.Errorf("index Cache-Control = %q, want no-cache", got)
	}
	if got := a.get("/assets/app.js").Header.Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("asset Cache-Control = %q, want immutable", got)
	}
}

func TestIndexWithoutFrontend(t *testing.T) {
	res := newApp(t, withAssets(fstest.MapFS{})).get("/")
	if res.Status != http.StatusServiceUnavailable || !strings.Contains(res.Body, "just build") {
		t.Errorf("GET / = %d %q, want 503 mentioning just build", res.Status, res.Body)
	}
}

func TestStaticAndDocs(t *testing.T) {
	a := newApp(t)
	if res := a.get("/").want(t, 200); res.Body != "<main>Notebook</main>" {
		t.Errorf("GET / = %q, want the index of the frontend", res.Body)
	}
	if got := a.get("/assets/app.js").want(t, 200).Header.Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Errorf("asset Cache-Control = %q, want immutable", got)
	}

	var spec struct {
		Paths map[string]json.RawMessage `json:"paths"`
	}
	a.get("/api/openapi.json").want(t, 200).decode(t, &spec)
	for _, path := range []string{
		"/api/vaults",
		"/api/vaults/{vault}/files",
		"/api/vaults/{vault}/note",
		"/api/vaults/{vault}/revision",
		"/api/vaults/{vault}/notes",
		"/api/vaults/{vault}/folders",
		"/api/vaults/{vault}/move",
		"/api/vaults/{vault}/entry",
		"/api/vaults/{vault}/search",
	} {
		if _, ok := spec.Paths[path]; !ok {
			t.Errorf("/api/openapi.json does not list %s", path)
		}
	}
	if docs := a.get("/api/docs").want(t, 200); !strings.Contains(docs.Body, "/api/openapi") {
		t.Errorf("/api/docs does not point at /api/openapi: %.200s", docs.Body)
	}
	// Responses carry only their own data, with no $schema links.
	if body := a.get("/api/vaults").want(t, 200).Body; strings.Contains(body, "$schema") {
		t.Errorf("response has a $schema field: %s", body)
	}
}

func TestProbes(t *testing.T) {
	a := newApp(t)
	for _, path := range []string{"/healthz", "/readyz"} {
		res := a.get(path).want(t, 200)
		if res.Body != "ok\n" || res.Header.Get("Cache-Control") != "no-store" {
			t.Errorf("GET %s = %q with Cache-Control %q, want \"ok\\n\" and no-store", path, res.Body, res.Header.Get("Cache-Control"))
		}
	}
}

func TestReadyzUnavailable(t *testing.T) {
	logs := captureLog(t)
	a := newApp(t)
	a.get("/readyz").want(t, 200)

	if err := os.RemoveAll(a.dirs["work"]); err != nil {
		t.Fatal(err)
	}
	res := a.get("/readyz").want(t, http.StatusServiceUnavailable)
	if res.Body != "Vault unavailable\n" {
		t.Errorf("body = %q, want the fixed \"Vault unavailable\\n\"", res.Body)
	}
	if res.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", res.Header.Get("Cache-Control"))
	}
	// The cause is for the operator: it is in the log, not in the answer.
	if !strings.Contains(logs.String(), "unavailable") {
		t.Errorf("log = %q, want the cause of the failed probe", logs.String())
	}
	// Liveness does not depend on the vaults.
	a.get("/healthz").want(t, 200)
}

func TestAPIResponsesAreNotCached(t *testing.T) {
	a := newApp(t)
	a.seed("notes", "a.md", "# A\n")
	for _, tc := range []struct {
		method, path string
		headers      map[string]string
		status       int
	}{
		{"GET", "/api/vaults", nil, 200},
		{"GET", "/api/vaults/notes/files", nil, 200},
		{"GET", api("notes", "note", "a.md"), nil, 200},
		{"GET", api("notes", "note", "missing.md"), nil, 404},
		{"GET", "/api/vaults/nope/files", nil, 404},
		{"GET", "/api/nope", nil, 404},
		{"GET", "/api/docs", nil, 200},
		{"GET", "/api/openapi.json", nil, 200},
		{"POST", "/api/vaults/notes/folders", nil, 403}, // held back by the guard
		{"DELETE", api("notes", "entry", "a.md"), mutating, 204},
	} {
		res := a.do(tc.method, tc.path, "", tc.headers)
		if res.Status != tc.status {
			t.Errorf("%s %s = %d, want %d", tc.method, tc.path, res.Status, tc.status)
		}
		if got := res.Header.Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s %s: Cache-Control = %q, want no-store", tc.method, tc.path, got)
		}
	}
}

func TestVaultsAndFiles(t *testing.T) {
	a := newApp(t)
	a.seed("notes", "a.md", "# A\n")
	a.seed("notes", "sub/b.md", "# B\n")
	a.seed("notes", "pic.png", "png")
	a.seed("notes", ".obsidian/app.json", "{}")

	res := a.get("/api/vaults").want(t, 200)
	if want := `{"readOnly":false,"vaults":[{"name":"notes"},{"name":"work"}]}`; strings.TrimSpace(res.Body) != want {
		t.Errorf("GET /api/vaults = %s, want %s", res.Body, want)
	}

	files := "/api/vaults/notes/files"
	res = a.get(files).want(t, 200)
	var got listingJSON
	res.decode(t, &got)
	if fmt.Sprint(paths(got.Notes)) != "[a.md sub/b.md]" || fmt.Sprint(paths(got.Assets)) != "[pic.png]" || fmt.Sprint(got.Folders) != "[sub]" {
		t.Errorf("listing = %+v, want notes [a.md sub/b.md], assets [pic.png], folders [sub]", got)
	}
	if got.Notes[0].Size != 4 || got.Notes[0].Modified == "" {
		t.Errorf("entry = %+v, want its size and modification time", got.Notes[0])
	}
	etag := res.Header.Get("ETag")
	if got.Fingerprint == "" || etag != `"`+got.Fingerprint+`"` {
		t.Fatalf("ETag = %q, want the fingerprint %q in quotes", etag, got.Fingerprint)
	}
	// An empty vault lists empty arrays, not null.
	if body := a.get("/api/vaults/work/files").want(t, 200).Body; !strings.Contains(body, `"notes":[],"assets":[],"folders":[]`) {
		t.Errorf("listing of an empty vault = %s, want empty arrays", body)
	}

	// The same listing is not sent again to a client that has it, however it
	// names the tag.
	for _, ifNoneMatch := range []string{etag, "W/" + etag, `"other", ` + etag, "*"} {
		res := a.do("GET", files, "", map[string]string{"If-None-Match": ifNoneMatch}).want(t, http.StatusNotModified)
		if res.Body != "" || res.Header.Get("ETag") != etag || res.Header.Get("Cache-Control") != "no-store" {
			t.Errorf("If-None-Match %s: body %q, ETag %q, Cache-Control %q; want no body, the ETag and no-store", ifNoneMatch, res.Body, res.Header.Get("ETag"), res.Header.Get("Cache-Control"))
		}
	}
	for _, ifNoneMatch := range []string{`"other"`, got.Fingerprint, `""`} {
		a.do("GET", files, "", map[string]string{"If-None-Match": ifNoneMatch}).want(t, 200)
	}

	// A change shows as a new tag, and the old one now gets the new listing.
	a.seed("notes", "c.md", "# C\n")
	res = a.do("GET", files, "", map[string]string{"If-None-Match": etag}).want(t, 200)
	if res.Header.Get("ETag") == etag || res.Header.Get("ETag") == "" {
		t.Errorf("ETag after a new file = %q, want one other than %q", res.Header.Get("ETag"), etag)
	}
	var changed listingJSON
	res.decode(t, &changed)
	if fmt.Sprint(paths(changed.Notes)) != "[a.md c.md sub/b.md]" {
		t.Errorf("notes after a new file = %v, want a.md c.md sub/b.md", paths(changed.Notes))
	}
}

func TestUnknownVault(t *testing.T) {
	a := newApp(t)
	for _, path := range []string{
		"/api/vaults/nope/files",
		api("nope", "note", "a.md"),
		api("nope", "revision", "a.md"),
		api("nope", "raw", "a.png"),
		"/api/vaults/nope/search?q=a",
		"/api/vaults/Notes/files", // names are exact
	} {
		a.get(path).want(t, 404)
	}
	for _, r := range mutations("nope", "r") {
		a.send(r.method, r.path, r.body).want(t, 404)
	}
}

func TestGuard(t *testing.T) {
	const limit = 4 << 20
	a := newApp(t)
	const original = "# A\n"
	a.seed("notes", "a.md", original)
	// Padding that JSON ignores makes a request of exactly the size wanted.
	padded := func(body string, size int) string { return body + strings.Repeat(" ", size-len(body)) }
	rev := vault.Revision([]byte(original))
	put := `{"content":"# B\n","revision":"` + rev + `"}`
	note := api("notes", "note", "a.md")

	// Everything that is held back changes nothing.
	for _, tc := range []struct {
		name, method, path, body string
		headers                  map[string]string
		status                   int
	}{
		{"no X-Notebook header", "PUT", note, put, with(mutating, "X-Notebook", ""), 403},
		{"X-Notebook is not 1", "PUT", note, put, with(mutating, "X-Notebook", "0"), 403},
		{"no X-Notebook header on a POST", "POST", "/api/vaults/notes/folders", `{"path":"x"}`, with(mutating, "X-Notebook", ""), 403},
		{"no X-Notebook header on a DELETE", "DELETE", api("notes", "entry", "a.md"), "", with(mutating, "X-Notebook", ""), 403},
		{"foreign Origin", "PUT", note, put, with(mutating, "Origin", "http://evil.example"), 403},
		{"Origin null", "PUT", note, put, with(mutating, "Origin", "null"), 403},
		{"Origin of another port", "PUT", note, put, with(mutating, "Origin", "http://127.0.0.1:1"), 403},
		{"Origin that has a path", "PUT", note, put, with(mutating, "Origin", a.url+"/x"), 403},
		{"text/plain", "PUT", note, put, with(mutating, "Content-Type", "text/plain"), 415},
		{"form encoded", "PUT", note, put, with(mutating, "Content-Type", "application/x-www-form-urlencoded"), 415},
		{"no content type", "PUT", note, put, with(mutating, "Content-Type", ""), 415},
		{"POST without a content type", "POST", "/api/vaults/notes/folders", "", with(mutating, "Content-Type", ""), 415},
		{"DELETE with a body that is not JSON", "DELETE", api("notes", "entry", "a.md"), "x", with(mutating, "Content-Type", "text/plain"), 415},
		{"body of 4 MiB and a byte", "PUT", note, padded(put, limit+1), mutating, 413},
		{"body that is not UTF-8", "POST", "/api/vaults/notes/folders", "{\"path\":\"a\xffb\"}", mutating, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := a.do(tc.method, tc.path, tc.body, tc.headers).want(t, tc.status)
			if res.Header.Get("Content-Type") != "application/problem+json" || !strings.Contains(res.Body, `"detail"`) {
				t.Errorf("answer = %s %q, want a problem document with a detail", res.Header.Get("Content-Type"), res.Body)
			}
			if disk := a.onDisk("notes", "a.md"); disk != original {
				t.Errorf("a.md on disk = %q, want it untouched", disk)
			}
			if _, err := os.Stat(filepath.Join(a.dirs["notes"], "x")); err == nil {
				t.Errorf("a folder was made")
			}
		})
	}

	// What is well formed gets through.
	for _, tc := range []struct {
		name, method, path, body string
		headers                  map[string]string
		status                   int
	}{
		{"same Origin", "POST", "/api/vaults/notes/folders", `{"path":"origin"}`, with(mutating, "Origin", a.url), 201},
		{"charset", "POST", "/api/vaults/notes/folders", `{"path":"charset"}`, with(mutating, "Content-Type", "application/json; charset=utf-8"), 201},
		{"body of exactly 4 MiB", "POST", "/api/vaults/notes/folders", padded(`{"path":"big"}`, limit), mutating, 201},
		{"DELETE without a body", "DELETE", api("notes", "entry", "a.md"), "", map[string]string{"X-Notebook": "1"}, 204},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a.do(tc.method, tc.path, tc.body, tc.headers).want(t, tc.status)
		})
	}
}

// A body that ends before the length it announced is not a request, even when
// what came is a whole JSON document: nothing is done with it.
func TestGuardTruncatedBody(t *testing.T) {
	a := newApp(t)
	host := strings.TrimPrefix(a.url, "http://")
	conn, err := net.Dial("tcp", host)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	body := `{"path":"half"}`
	fmt.Fprintf(conn, "POST /api/vaults/notes/folders HTTP/1.1\r\nHost: %s\r\nX-Notebook: 1\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n%s",
		host, len(body)+50, body)
	// Half-close: the server sees the body end early and can still answer.
	if err := conn.(*net.TCPConn).CloseWrite(); err != nil {
		t.Fatal(err)
	}
	res, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", res.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(a.dirs["notes"], "half")); err == nil {
		t.Errorf("a folder was made from a request that did not arrive whole")
	}
}

func TestReadOnly(t *testing.T) {
	a := newApp(t, withOptions(Options{ReadOnly: true}))
	a.seed("notes", "a.md", "# A\n")
	rev := vault.Revision([]byte("# A\n"))
	// The guard answers before it looks at anything else, so a request that is
	// not well formed gets the same answer.
	for _, r := range mutations("notes", rev) {
		for name, headers := range map[string]map[string]string{"well formed": mutating, "bare": nil} {
			t.Run(r.method+" "+r.path+" "+name, func(t *testing.T) {
				res := a.do(r.method, r.path, r.body, headers).want(t, 405)
				if res.Header.Get("Allow") != "GET, HEAD" {
					t.Errorf("Allow = %q, want GET, HEAD", res.Header.Get("Allow"))
				}
			})
		}
	}
	if disk := a.onDisk("notes", "a.md"); disk != "# A\n" {
		t.Errorf("a.md on disk = %q, want it untouched", disk)
	}
	if entries, err := os.ReadDir(a.dirs["notes"]); err != nil || len(entries) != 1 {
		t.Errorf("vault directory = %v, %v; want only a.md", entries, err)
	}

	// Reading goes on, and the app is told.
	if body := a.get("/api/vaults").want(t, 200).Body; !strings.Contains(body, `"readOnly":true`) {
		t.Errorf("GET /api/vaults = %s, want readOnly true", body)
	}
	a.get(api("notes", "note", "a.md")).want(t, 200)
	a.get("/api/vaults/notes/files").want(t, 200)
}

func TestProblemMapping(t *testing.T) {
	logs := captureLog(t)
	const secret = "/home/daniel/vaults/notes"
	const generic = "Could not access the vault. Try again."
	for _, tc := range []struct {
		err     error
		status  int
		message string
	}{
		{vault.ErrNotFound, 404, "not found"},
		{vault.ErrExists, 409, "already exists"},
		{vault.ErrConflict, 409, "note changed on disk"},
		{&vault.ConflictError{Current: "abc"}, 409, "note changed on disk"},
		{vault.ErrInvalidPath, 400, "invalid path"},
		{vault.ErrEmptyQuery, 400, "empty query"},
		{vault.ErrTooLarge, 413, "note exceeds 2 MiB"},
		{vault.ErrNotNote, 415, "not a UTF-8 markdown note"},
		{vault.ErrReadOnly, 405, "vault is read-only"},
		// A sentinel that is wrapped keeps its status, and only its own text is
		// told: the wrapper may hold anything.
		{fmt.Errorf("vault %q at %s: %w", "notes", secret, vault.ErrNotFound), 404, "not found"},
		// Anything else is the server's failure, and says nothing of its cause.
		{fmt.Errorf("vault %q unavailable: stat %s: no such file or directory", "notes", secret), 500, generic},
		{&fs.PathError{Op: "open", Path: secret + "/a.md", Err: fs.ErrPermission}, 500, generic},
		{context.Canceled, 500, generic},
	} {
		got := problem(tc.err)
		if got.GetStatus() != tc.status || got.Error() != tc.message {
			t.Errorf("problem(%v) = %d %q, want %d %q", tc.err, got.GetStatus(), got.Error(), tc.status, tc.message)
		}
		if body, err := json.Marshal(got); err != nil || strings.Contains(string(body), secret) {
			t.Errorf("problem(%v) is %s, %v; want no path in it", tc.err, body, err)
		}
	}
	// The causes of the failures are logged, except that of a client that left.
	if !strings.Contains(logs.String(), "unavailable") || !strings.Contains(logs.String(), "permission denied") {
		t.Errorf("log = %q, want the causes of the failures", logs.String())
	}
	if strings.Contains(logs.String(), "canceled") {
		t.Errorf("log = %q, want a canceled request left out", logs.String())
	}
}

// A vault that fails must not tell where it is on disk, whatever fails.
func TestFailingVaultNeverLeaksItsPath(t *testing.T) {
	logs := captureLog(t)
	a := newApp(t)
	a.seed("notes", "a.md", "# A\n")
	a.seed("notes", "pic.png", "png")
	dir := a.dirs["notes"]
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(filepath.Dir(dir))
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		method, path, body string
		status             int
	}{
		{"GET", "/api/vaults/notes/files", "", 500},
		{"GET", "/api/vaults/notes/search?q=a", "", 500},
		// What the file system says to a write in a directory that is gone is its
		// own: any error will do, as long as it tells nothing.
		{"POST", "/api/vaults/notes/notes", `{"path":"new.md"}`, 0},
		{"POST", "/api/vaults/notes/folders", `{"path":"folder"}`, 0},
		{"GET", api("notes", "note", "a.md"), "", 404},
		{"GET", api("notes", "revision", "a.md"), "", 404},
		{"GET", api("notes", "raw", "pic.png"), "", 404},
		{"PUT", api("notes", "note", "a.md"), `{"content":"x","revision":"r"}`, 404},
		{"POST", "/api/vaults/notes/move", `{"from":"a.md","to":"b.md"}`, 404},
		{"DELETE", api("notes", "entry", "a.md"), "", 404},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			res := a.do(tc.method, tc.path, tc.body, mutating)
			if tc.status != 0 && res.Status != tc.status || tc.status == 0 && res.Status < 400 {
				t.Fatalf("status = %d, want %d (0 is any error); body: %.300s", res.Status, tc.status, res.Body)
			}
			for _, secret := range []string{dir, filepath.ToSlash(dir), filepath.Join(resolved, filepath.Base(dir)), a.dirs["work"]} {
				if strings.Contains(res.Body, secret) {
					t.Errorf("answer %s tells the vault directory %s", res.Body, secret)
				}
			}
			if res.Status == 500 && !strings.Contains(res.Body, "Could not access the vault. Try again.") {
				t.Errorf("answer = %s, want the generic message", res.Body)
			}
		})
	}
	// The cause of the first failure is in the log, where the operator reads it.
	if !strings.Contains(logs.String(), `vault "notes" unavailable`) {
		t.Errorf("log = %q, want the cause of the failure", logs.String())
	}
}
