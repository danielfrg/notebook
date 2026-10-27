package httpapi

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/danielfrg/notebook/internal/vault"
)

// Text that must come back from the API exactly as it went in, written as bytes
// so that no tool can turn it into something else.
const (
	bom  = "\xef\xbb\xbf"     // U+FEFF at the start of a note
	lsep = "\xe2\x80\xa8"     // U+2028, which JSON writers escape
	grin = "\xf0\x9f\x98\x80" // U+1F600, a surrogate pair when it is escaped in JSON
)

func TestGetAndPutNote(t *testing.T) {
	a := newApp(t)
	// Nothing in this text may change on the way in or out: a BOM, CRLF, a bare
	// CR, trailing spaces, no final newline, and characters that JSON escapes.
	original := bom + "---\r\ntitle: Título\r\n---\r\n# Hola  \r\n<b>&amp;</b> " + lsep + grin + "\rbare\tCR \"q\" \\ end"
	a.seed("notes", "docs/a.md", original)
	target := api("notes", "note", "docs/a.md")

	var got noteJSON
	a.get(target).want(t, 200).decode(t, &got)
	if got.Path != "docs/a.md" || got.Content != original || got.Size != int64(len(original)) ||
		got.Revision != vault.Revision([]byte(original)) || got.Modified == "" {
		t.Fatalf("GET note = %+v, want the note exactly as it is on disk", got)
	}

	// A save writes the text exactly as it was sent.
	edited := strings.Replace(original, "# Hola", "# Hola mundo", 1)
	var saved noteJSON
	a.send("PUT", target, map[string]string{"content": edited, "revision": got.Revision}).want(t, 200).decode(t, &saved)
	if saved.Path != "docs/a.md" || saved.Content != edited || saved.Size != int64(len(edited)) || saved.Revision != vault.Revision([]byte(edited)) {
		t.Errorf("PUT note = %+v, want the note as saved", saved)
	}
	if disk := a.onDisk("notes", "docs/a.md"); disk != edited {
		t.Errorf("on disk = %q, want exactly the text that was sent, %q", disk, edited)
	}

	var current struct {
		Revision string `json:"revision"`
		Modified string `json:"modified"`
	}
	a.get(api("notes", "revision", "docs/a.md")).want(t, 200).decode(t, &current)
	if current.Revision != saved.Revision || current.Modified == "" {
		t.Errorf("GET revision = %+v, want revision %s and a modification time", current, saved.Revision)
	}

	// A body from another JSON writer, which escapes every one of those
	// characters: the escape is made here from its parts, so that no tool turns it
	// into the character itself.
	u := `\` + `u`
	escaped := `{"content":"` + u + `feff# T\r\n` + u + `2028` + u + `d83d` + u + `de00 ` + u + `003cb` + u + `003e\r\n","revision":"` + saved.Revision + `"}`
	a.send("PUT", target, escaped).want(t, 200)
	want := bom + "# T\r\n" + lsep + grin + " <b>\r\n"
	if disk := a.onDisk("notes", "docs/a.md"); disk != want {
		t.Errorf("on disk = %q, want %q", disk, want)
	}

	// Saving what is there already is not a conflict and changes nothing.
	var again noteJSON
	a.send("PUT", target, map[string]string{"content": want, "revision": vault.Revision([]byte(want))}).want(t, 200).decode(t, &again)
	if again.Content != want || again.Revision != vault.Revision([]byte(want)) {
		t.Errorf("PUT of the same text = %+v, want the note as it is", again)
	}
}

func TestPutNoteConflict(t *testing.T) {
	a := newApp(t)
	a.seed("notes", "a.md", "v1\n")
	target := api("notes", "note", "a.md")
	var loaded noteJSON
	a.get(target).want(t, 200).decode(t, &loaded)

	// Another program changes the note after it was loaded: the save loses, and
	// the answer carries the revision that is on disk now.
	a.seed("notes", "a.md", "theirs\n")
	theirs := vault.Revision([]byte("theirs\n"))
	res := a.send("PUT", target, map[string]string{"content": "mine\n", "revision": loaded.Revision}).want(t, 409)
	var problem struct {
		Detail string `json:"detail"`
		Errors []struct {
			Message  string `json:"message"`
			Location string `json:"location"`
			Value    string `json:"value"`
		} `json:"errors"`
	}
	res.decode(t, &problem)
	if len(problem.Errors) != 1 || problem.Errors[0].Value != theirs || problem.Errors[0].Location != "body.revision" || problem.Detail != "note changed on disk" {
		t.Fatalf("conflict = %s, want errors[0].value %s", res.Body, theirs)
	}
	if got := res.Header.Get("Content-Type"); got != "application/problem+json" {
		t.Errorf("Content-Type = %q, want application/problem+json", got)
	}
	if disk := a.onDisk("notes", "a.md"); disk != "theirs\n" {
		t.Errorf("on disk = %q, want the other program's text untouched", disk)
	}

	// A revision that is empty matches nothing, and names the current one.
	res = a.send("PUT", target, map[string]string{"content": "mine\n", "revision": ""}).want(t, 409)
	res.decode(t, &problem)
	if len(problem.Errors) != 1 || problem.Errors[0].Value != theirs {
		t.Errorf("conflict for an empty revision = %s, want errors[0].value %s", res.Body, theirs)
	}

	// Saving again with the revision the conflict named wins.
	a.send("PUT", target, map[string]string{"content": "mine\n", "revision": theirs}).want(t, 200)
	if disk := a.onDisk("notes", "a.md"); disk != "mine\n" {
		t.Errorf("on disk = %q, want mine", disk)
	}

	// A note deleted since it was loaded is gone, not in conflict, and a save does
	// not bring it back.
	if err := os.Remove(filepath.Join(a.dirs["notes"], "a.md")); err != nil {
		t.Fatal(err)
	}
	for _, revision := range []string{loaded.Revision, ""} {
		a.send("PUT", target, map[string]string{"content": "again\n", "revision": revision}).want(t, 404)
	}
	if disk := a.onDisk("notes", "a.md"); disk != "<missing>" {
		t.Errorf("on disk = %q, want the note not to come back", disk)
	}
}

func TestInvalidPathsRejected(t *testing.T) {
	a := newApp(t)
	dir := a.dirs["notes"]
	a.seed("notes", "a.md", "# A\n")
	a.seed("notes", "pic.png", "png")
	a.seed("notes", "sub/b.md", "# B\n")
	a.seed("notes", "sub/pic.png", "png")
	// What ../ would reach.
	outside := filepath.Join(filepath.Dir(dir), "x.md")
	if err := os.WriteFile(outside, []byte("outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for link, target := range map[string]string{"link.md": "a.md", "link.png": "pic.png", "linkdir": "sub"} {
		if err := os.Symlink(target, filepath.Join(dir, link)); err != nil {
			t.Fatal(err)
		}
	}

	// Paths as they come in a query, already escaped.
	for _, query := range []string{
		"../x.md",
		"%2e%2e/x.md",
		"%2E%2E%2Fx.md",
		".git/config",
		".obsidian/x.md",
		"sub/.hidden.md",
		"a%5Cb.md",
		"/etc/passwd",
		"%2Fetc%2Fpasswd",
		"a//b.md",
		"sub/",
		"./a.md",
		"a%00b.md",
		"%FF%FE.md", // not UTF-8
	} {
		for _, e := range []struct{ method, endpoint string }{{"GET", "note"}, {"GET", "revision"}, {"DELETE", "entry"}, {"GET", "raw"}} {
			path := "/api/vaults/notes/" + e.endpoint + "?path=" + query
			if res := a.do(e.method, path, "", mutating); res.Status != 400 {
				t.Errorf("%s %s = %d, want 400", e.method, path, res.Status)
			}
		}
	}

	// A symlink is never followed, and a folder that is one is not entered.
	for _, tc := range []struct{ method, endpoint, path string }{
		{"GET", "note", "link.md"},
		{"GET", "revision", "link.md"},
		{"DELETE", "entry", "link.md"},
		{"GET", "note", "linkdir/b.md"},
		{"DELETE", "entry", "linkdir"},
		{"DELETE", "entry", "linkdir/b.md"},
		{"GET", "raw", "link.png"},
		{"GET", "raw", "linkdir/pic.png"},
	} {
		if res := a.do(tc.method, api("notes", tc.endpoint, tc.path), "", mutating); res.Status != 400 {
			t.Errorf("%s %s of %s = %d, want 400", tc.method, tc.endpoint, tc.path, res.Status)
		}
	}

	// The same paths in what is written.
	for _, bad := range []string{"../x.md", ".git/config", `a\b.md`, "/etc/passwd", "a//b.md", "link.md", "linkdir/new.md"} {
		for _, r := range []request{
			{"PUT", api("notes", "note", bad), `{"content":"x","revision":"r"}`},
			{"POST", "/api/vaults/notes/notes", js(map[string]string{"path": bad, "content": "x"})},
			{"POST", "/api/vaults/notes/folders", js(map[string]string{"path": bad})},
			{"POST", "/api/vaults/notes/move", js(map[string]string{"from": "a.md", "to": bad})},
			{"POST", "/api/vaults/notes/move", js(map[string]string{"from": bad, "to": "c.md"})},
		} {
			if res := a.send(r.method, r.path, r.body); res.Status != 400 {
				t.Errorf("%s %s %s = %d, want 400", r.method, r.path, r.body, res.Status)
			}
		}
	}

	// Nothing was made, changed or removed, in the vault or outside it.
	var names []string
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if want := []string{"a.md", "link.md", "link.png", "linkdir", "pic.png", "sub"}; !slices.Equal(names, want) {
		t.Errorf("vault directory = %v, want %v", names, want)
	}
	if disk := a.onDisk("notes", "a.md"); disk != "# A\n" {
		t.Errorf("a.md = %q, want it untouched", disk)
	}
	if data, err := os.ReadFile(outside); err != nil || string(data) != "outside\n" {
		t.Errorf("file outside the vault = %q, %v; want it untouched", data, err)
	}
}

func TestUnicodePathQuery(t *testing.T) {
	encoders := map[string]func(string) string{"encodeURIComponent": enc, "QueryEscape": url.QueryEscape}
	for _, name := range []string{"¿Qué es el Estoicismo?.md", "100% #1.md", "a,b [c].md", "a+b.md", "日本語 ノート.md"} {
		for encoderName, encode := range encoders {
			t.Run(name+" "+encoderName, func(t *testing.T) {
				a := newApp(t)
				at := func(endpoint, path string) string { return "/api/vaults/notes/" + endpoint + "?path=" + encode(path) }
				moved := "sub/" + name

				var created noteJSON
				a.send("POST", "/api/vaults/notes/notes", map[string]string{"path": name, "content": "one\n"}).want(t, 201).decode(t, &created)
				if created.Path != name {
					t.Errorf("created path = %q, want %q", created.Path, name)
				}
				if disk := a.onDisk("notes", name); disk != "one\n" {
					t.Fatalf("on disk = %q, want the note under its exact name", disk)
				}

				var got noteJSON
				a.get(at("note", name)).want(t, 200).decode(t, &got)
				if got.Path != name || got.Content != "one\n" {
					t.Errorf("GET note = %+v, want the note %q", got, name)
				}
				a.send("PUT", at("note", name), map[string]string{"content": "two\n", "revision": got.Revision}).want(t, 200)
				if disk := a.onDisk("notes", name); disk != "two\n" {
					t.Errorf("on disk after the save = %q, want two", disk)
				}
				a.get(at("revision", name)).want(t, 200)

				var move struct {
					Path string `json:"path"`
				}
				a.send("POST", "/api/vaults/notes/move", map[string]string{"from": name, "to": moved}).want(t, 200).decode(t, &move)
				if move.Path != moved {
					t.Errorf("move path = %q, want %q", move.Path, moved)
				}
				a.get(at("note", name)).want(t, 404)
				a.get(at("note", moved)).want(t, 200)
				var listing listingJSON
				a.get("/api/vaults/notes/files").want(t, 200).decode(t, &listing)
				if !slices.Equal(paths(listing.Notes), []string{moved}) {
					t.Errorf("notes = %v, want [%s]", paths(listing.Notes), moved)
				}

				a.send("DELETE", at("entry", moved), nil).want(t, 204)
				a.get(at("note", moved)).want(t, 404)
				trash, err := os.ReadDir(filepath.Join(a.dirs["notes"], ".trash"))
				if err != nil || len(trash) != 1 || !strings.HasSuffix(trash[0].Name(), "-"+name) {
					t.Errorf(".trash = %v, %v; want the note, named after %q", trash, err, name)
				}
			})
		}
	}
}

func TestEntryOperations(t *testing.T) {
	a := newApp(t)
	notes, folders, move := "/api/vaults/notes/notes", "/api/vaults/notes/folders", "/api/vaults/notes/move"

	// Create: the note and the folders above it.
	var created noteJSON
	a.send("POST", notes, map[string]string{"path": "ideas/a.md", "content": "# A\n"}).want(t, 201).decode(t, &created)
	if created.Path != "ideas/a.md" || created.Content != "# A\n" || created.Size != 4 || created.Revision != vault.Revision([]byte("# A\n")) || created.Modified == "" {
		t.Errorf("created = %+v, want the new note", created)
	}
	if disk := a.onDisk("notes", "ideas/a.md"); disk != "# A\n" {
		t.Errorf("on disk = %q, want # A", disk)
	}
	// It never replaces anything.
	res := a.send("POST", notes, map[string]string{"path": "ideas/a.md", "content": "other"}).want(t, 409)
	if !strings.Contains(res.Body, "already exists") {
		t.Errorf("answer = %s, want it to say the note exists", res.Body)
	}
	if disk := a.onDisk("notes", "ideas/a.md"); disk != "# A\n" {
		t.Errorf("on disk = %q, want # A untouched", disk)
	}
	a.send("POST", notes, map[string]string{"path": "empty.md"}).want(t, 201) // no content is an empty note
	if disk := a.onDisk("notes", "empty.md"); disk != "" {
		t.Errorf("empty.md = %q, want an empty note", disk)
	}
	a.send("POST", notes, map[string]string{"path": "ideas/a.txt", "content": "x"}).want(t, 415)

	// Folders.
	var folder struct {
		Path string `json:"path"`
	}
	a.send("POST", folders, map[string]string{"path": "archive/2026"}).want(t, 201).decode(t, &folder)
	if info, err := os.Stat(filepath.Join(a.dirs["notes"], "archive", "2026")); err != nil || !info.IsDir() || folder.Path != "archive/2026" {
		t.Errorf("folder = %q, %v, %v; want archive/2026 made", folder.Path, info, err)
	}
	a.send("POST", folders, map[string]string{"path": "archive/2026"}).want(t, 409)
	a.send("POST", folders, map[string]string{"path": "ideas/a.md"}).want(t, 409)

	// Move: never over something.
	var moved struct {
		Path string `json:"path"`
	}
	a.send("POST", move, map[string]string{"from": "ideas/a.md", "to": "archive/2026/a.md"}).want(t, 200).decode(t, &moved)
	if moved.Path != "archive/2026/a.md" || a.onDisk("notes", "ideas/a.md") != "<missing>" || a.onDisk("notes", "archive/2026/a.md") != "# A\n" {
		t.Errorf("after the move: path %q, old %q, new %q", moved.Path, a.onDisk("notes", "ideas/a.md"), a.onDisk("notes", "archive/2026/a.md"))
	}
	a.send("POST", move, map[string]string{"from": "archive/2026/a.md", "to": "empty.md"}).want(t, 409)
	a.send("POST", move, map[string]string{"from": "nothing.md", "to": "x.md"}).want(t, 404)
	a.send("POST", move, map[string]string{"from": "empty.md", "to": "empty.txt"}).want(t, 400)
	if disk := a.onDisk("notes", "archive/2026/a.md"); disk != "# A\n" {
		t.Errorf("the note = %q, want it where it was moved to, untouched", disk)
	}

	// Delete moves the entry to the trash of the vault, which is hidden.
	a.send("DELETE", api("notes", "entry", "archive/2026/a.md"), nil).want(t, 204)
	a.get(api("notes", "note", "archive/2026/a.md")).want(t, 404)
	a.send("DELETE", api("notes", "entry", "archive/2026/a.md"), nil).want(t, 404)
	trash, err := os.ReadDir(filepath.Join(a.dirs["notes"], ".trash"))
	if err != nil || len(trash) != 1 || !strings.HasSuffix(trash[0].Name(), "-a.md") {
		t.Fatalf(".trash = %v, %v; want the deleted note", trash, err)
	}
	if data, err := os.ReadFile(filepath.Join(a.dirs["notes"], ".trash", trash[0].Name())); err != nil || string(data) != "# A\n" {
		t.Errorf("trashed note = %q, %v; want its text kept", data, err)
	}
	var listing listingJSON
	a.get("/api/vaults/notes/files").want(t, 200).decode(t, &listing)
	for _, p := range append(paths(listing.Notes), listing.Folders...) {
		if strings.Contains(p, ".trash") {
			t.Errorf("listing has %q, want the trash hidden", p)
		}
	}
	a.get(api("notes", "note", ".trash/"+trash[0].Name())).want(t, 400)
	// A folder goes with what is in it.
	a.send("DELETE", api("notes", "entry", "archive"), nil).want(t, 204)
	if _, err := os.Stat(filepath.Join(a.dirs["notes"], "archive")); !os.IsNotExist(err) {
		t.Errorf("archive: %v, want it moved to the trash", err)
	}
}

func TestSearch(t *testing.T) {
	a := newApp(t)
	a.seed("notes", "stoicism.md", "# Estoicismo\nUna reflexión sobre la virtud\n")
	a.seed("notes", "Reflexión.md", "nothing about it\n")
	a.seed("notes", "other.md", "unrelated\n")
	search := func(q string) response { return a.get("/api/vaults/notes/search?q=" + enc(q)) }

	// Case and accents do not matter, and a name ranks above a text.
	res := search("REFLEXION").want(t, 200)
	var got struct {
		Hits []struct {
			Path     string `json:"path"`
			Title    string `json:"title"`
			Score    int    `json:"score"`
			Modified string `json:"modified"`
			Snippets []struct {
				Line int    `json:"line"`
				Text string `json:"text"`
			} `json:"snippets"`
		} `json:"hits"`
	}
	res.decode(t, &got)
	if len(got.Hits) != 2 || got.Hits[0].Path != "Reflexión.md" || got.Hits[1].Path != "stoicism.md" {
		t.Fatalf("hits = %+v, want Reflexión.md and then stoicism.md", got.Hits)
	}
	byText := got.Hits[1]
	if byText.Title != "stoicism" || byText.Score <= 0 || byText.Modified == "" || len(byText.Snippets) != 1 ||
		byText.Snippets[0].Line != 2 || byText.Snippets[0].Text != "Una reflexión sobre la virtud" {
		t.Errorf("hit by text = %+v, want its title, score, time and the line", byText)
	}
	// A note found only by its name has no snippets, and the field is an array.
	if !strings.Contains(res.Body, `"snippets":[]`) {
		t.Errorf("body = %s, want an empty snippets array for the hit by name", res.Body)
	}
	if body := strings.TrimSpace(search("zzzzzz").want(t, 200).Body); body != `{"hits":[]}` {
		t.Errorf("search for nothing found = %s, want {\"hits\":[]}", body)
	}

	// A query without a term is a mistake of the caller.
	for _, q := range []string{"", "   ", `""`} {
		search(q).want(t, 400)
	}
	a.get("/api/vaults/notes/search").want(t, 400)
}

// The spec says the endpoint returns at most 50 hits. The vault itself does not
// cap them, because MCP pages through more.
func TestSearchLimit(t *testing.T) {
	a := newApp(t)
	for i := range 60 {
		a.seed("notes", fmt.Sprintf("n%02d.md", i), "needle\n")
	}
	for _, tc := range []struct {
		query        string
		status, hits int
	}{
		{"", 200, 50},
		{"&limit=3", 200, 3},
		{"&limit=50", 200, 50},
		{"&limit=51", 200, 50},
		{"&limit=500", 200, 50},
		{"&limit=0", 200, 50},
		{"&limit=-5", 200, 50},
		{"&limit=abc", 422, 0},
	} {
		res := a.get("/api/vaults/notes/search?q=needle"+tc.query).want(t, tc.status)
		if tc.status != 200 {
			continue
		}
		var got struct {
			Hits []struct{} `json:"hits"`
		}
		res.decode(t, &got)
		if len(got.Hits) != tc.hits {
			t.Errorf("search%s found %d hits, want %d", tc.query, len(got.Hits), tc.hits)
		}
	}
}

func TestNoteLimits(t *testing.T) {
	a := newApp(t)
	a.seed("notes", "big.md", strings.Repeat("a", vault.MaxNoteBytes+1))
	a.seed("notes", "binary.md", "caf\xe9 \xff\xfe")

	// A note that cannot be opened says why.
	for _, endpoint := range []string{"note", "revision"} {
		if res := a.get(api("notes", endpoint, "big.md")).want(t, 413); !strings.Contains(res.Body, "note exceeds 2 MiB") {
			t.Errorf("%s of big.md = %s, want it to say why", endpoint, res.Body)
		}
		if res := a.get(api("notes", endpoint, "binary.md")).want(t, 415); !strings.Contains(res.Body, "not a UTF-8 markdown note") {
			t.Errorf("%s of binary.md = %s, want it to say why", endpoint, res.Body)
		}
	}
	// It is listed all the same, and cannot be written over either.
	var listing listingJSON
	a.get("/api/vaults/notes/files").want(t, 200).decode(t, &listing)
	if !slices.Equal(paths(listing.Notes), []string{"big.md", "binary.md"}) {
		t.Errorf("notes = %v, want both listed", paths(listing.Notes))
	}
	a.send("PUT", api("notes", "note", "big.md"), map[string]string{"content": "x", "revision": "r"}).want(t, 413)
	a.send("PUT", api("notes", "note", "binary.md"), map[string]string{"content": "x", "revision": "r"}).want(t, 415)

	// Content over 2 MiB is refused though the request is under the 4 MiB cap, and
	// 2 MiB itself is a note.
	notes := "/api/vaults/notes/notes"
	a.send("POST", notes, map[string]string{"path": "over.md", "content": strings.Repeat("a", vault.MaxNoteBytes+1)}).want(t, 413)
	if disk := a.onDisk("notes", "over.md"); disk != "<missing>" {
		t.Errorf("over.md = %d bytes, want nothing made", len(disk))
	}
	atLimit := strings.Repeat("a", vault.MaxNoteBytes)
	a.send("POST", notes, map[string]string{"path": "limit.md", "content": atLimit}).want(t, 201)
	var got noteJSON
	a.get(api("notes", "note", "limit.md")).want(t, 200).decode(t, &got)
	if got.Content != atLimit {
		t.Errorf("note of exactly 2 MiB came back with %d bytes", len(got.Content))
	}
}

func TestRaw(t *testing.T) {
	a := newApp(t)
	png := "\x89PNG\r\n\x1a\n" + strings.Repeat("\x00data", 100)
	a.seed("notes", "img/pic.png", png)

	res := a.get(api("notes", "raw", "img/pic.png")).want(t, 200)
	if res.Body != png {
		t.Errorf("body is %d bytes, want the %d bytes of the image", len(res.Body), len(png))
	}
	for header, want := range map[string]string{
		"Content-Type":            "image/png",
		"X-Content-Type-Options":  "nosniff",
		"Content-Security-Policy": "sandbox",
		"Cache-Control":           "no-cache",
	} {
		if got := res.Header.Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
	if head := a.do("HEAD", api("notes", "raw", "img/pic.png"), "", nil).want(t, 200); head.Body != "" || head.Header.Get("Content-Type") != "image/png" {
		t.Errorf("HEAD = %q with %q, want only the headers", head.Body, head.Header.Get("Content-Type"))
	}

	// The type is fixed by the extension, whatever is in the file: an image that
	// is something else is never taken for a page.
	for name, contentType := range map[string]string{
		"a.png": "image/png", "A.PNG": "image/png", "a.jpg": "image/jpeg", "a.jpeg": "image/jpeg",
		"a.gif": "image/gif", "a.webp": "image/webp", "a.avif": "image/avif",
	} {
		a.seed("notes", name, `<script>alert(1)</script>`)
		res := a.get(api("notes", "raw", name)).want(t, 200)
		if res.Header.Get("Content-Type") != contentType || res.Header.Get("X-Content-Type-Options") != "nosniff" || res.Header.Get("Content-Security-Policy") != "sandbox" {
			t.Errorf("%s: %q, nosniff %q, CSP %q; want %s, nosniff and sandbox", name, res.Header.Get("Content-Type"), res.Header.Get("X-Content-Type-Options"), res.Header.Get("Content-Security-Policy"), contentType)
		}
	}

	// Only images: nothing else is served, not even the notes.
	a.seed("notes", "x.svg", `<svg onload="alert(1)"/>`)
	a.seed("notes", "n.md", "# N\n")
	if err := os.Mkdir(filepath.Join(a.dirs["notes"], "dir.png"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"x.svg", "n.md", "dir.png"} {
		a.get(api("notes", "raw", name)).want(t, 400)
	}
	// An error is not an image, and is not cached.
	res = a.get(api("notes", "raw", "missing.png")).want(t, 404)
	if res.Header.Get("Content-Type") != "application/problem+json" || res.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("error answer: %q, Cache-Control %q; want a problem document that is not stored", res.Header.Get("Content-Type"), res.Header.Get("Cache-Control"))
	}
}
