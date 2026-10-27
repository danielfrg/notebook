package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/danielfrg/notebook/internal/vault"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// What the tools say when they refuse. They are short, fixed, and the same for
// every path: an agent is told what is wrong with its request and nothing of the
// machine the vaults are on.
const (
	msgNotFound   = "Note not found"
	msgInvalid    = "Invalid path"
	msgNotNote    = "Not a UTF-8 Markdown note"
	msgTooLarge   = "Note exceeds 2 MiB"
	msgExists     = "Note already exists"
	msgConflict   = "Note changed since it was read; read it again and retry"
	msgReadOnly   = "Notes are read-only"
	msgEmptyQuery = "Query is empty"
	msgNoVault    = "Vault not found"
	msgNoText     = "Text is empty"
	msgPaging     = "limit must be 1 to 100 and offset must be nonnegative"
	msgGeneric    = "Notes unavailable; try again"
)

// epoch is where the modification times of the seeded notes start: they are
// hours from it.
var epoch = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func stamp(hours int) string { return epoch.Add(time.Duration(hours) * time.Hour).Format(time.RFC3339) }

// The JSON that the tools are expected to send, as an agent reads it. These are
// the tests' own types, and they are decoded strictly, so the names of the fields
// are checked and not only the types that produce them.
type (
	vaultsOut struct {
		Vaults []struct {
			Name  string `json:"name"`
			Notes int    `json:"notes"`
		} `json:"vaults"`
	}
	summaryOut struct {
		Path     string   `json:"path"`
		Title    string   `json:"title"`
		Modified string   `json:"modified"`
		Size     int      `json:"size"`
		Tags     []string `json:"tags"`
	}
	listOut struct {
		Notes      []summaryOut `json:"notes"`
		Total      int          `json:"total"`
		NextOffset *int         `json:"next_offset"`
	}
	noteOut struct {
		Vault       string         `json:"vault"`
		Path        string         `json:"path"`
		Title       string         `json:"title"`
		Content     string         `json:"content"`
		Revision    string         `json:"revision"`
		Modified    string         `json:"modified"`
		Tags        []string       `json:"tags"`
		Frontmatter map[string]any `json:"frontmatter"`
	}
	hitOut struct {
		Vault    string `json:"vault"`
		Path     string `json:"path"`
		Title    string `json:"title"`
		Snippets []struct {
			Line int    `json:"line"`
			Text string `json:"text"`
		} `json:"snippets"`
	}
	searchOut struct {
		Hits       []hitOut `json:"hits"`
		Total      int      `json:"total"`
		NextOffset *int     `json:"next_offset"`
	}
)

func (l listOut) paths() []string {
	out := []string{}
	for _, n := range l.Notes {
		out = append(out, n.Path)
	}
	return out
}

// ids returns the vault and path of each hit, as vault:path.
func (s searchOut) ids() []string {
	out := []string{}
	for _, h := range s.Hits {
		out = append(out, h.Vault+":"+h.Path)
	}
	return out
}

// agent is an MCP client connected over HTTP to the handler, which serves two
// temporary vaults, notes and work.
type agent struct {
	t       *testing.T
	ctx     context.Context
	dirs    map[string]string // vault name -> its directory
	session *sdk.ClientSession
	said    *strings.Builder // when set, all that the tools say in what they answer
}

// newAgent serves the vaults with options. They are opened as the vault options
// say, if there are any.
func newAgent(t *testing.T, options Options, vaultOptions ...vault.Options) *agent {
	t.Helper()
	a := &agent{t: t, dirs: map[string]string{"notes": t.TempDir(), "work": t.TempDir()}}
	var vaults []*vault.Vault
	for _, name := range []string{"notes", "work"} {
		v, err := vault.Open(name, a.dirs[name], append(vaultOptions, vault.Options{})[0])
		if err != nil {
			t.Fatal(err)
		}
		vaults = append(vaults, v)
	}
	reg, err := vault.NewRegistry(vaults...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reg.Close() })
	server := httptest.NewServer(NewHandler(reg, options, nil))
	t.Cleanup(server.Close)
	var cancel context.CancelFunc
	a.ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	a.session, err = sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil).
		Connect(a.ctx, &sdk.StreamableClientTransport{Endpoint: server.URL, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.session.Close() })
	return a
}

// at returns the agent for a subtest, which reports its failures there.
func (a *agent) at(t *testing.T) *agent {
	b := *a
	b.t = t
	return &b
}

// args makes the arguments of a call from pairs of a name and its value.
func args(pairs ...any) map[string]any {
	m := map[string]any{}
	for i := 0; i < len(pairs); i += 2 {
		m[pairs[i].(string)] = pairs[i+1]
	}
	return m
}

// noteTools are the tools that take the path of a note.
var noteTools = []string{"get_note", "update_note", "append_to_note", "create_note"}

// onNote makes the arguments of a call of one of the noteTools on a note. The
// revision is for update_note, which sends some text instead of the note's.
func onNote(tool, vaultName, path, revision string) map[string]any {
	a := args("vault", vaultName, "path", path)
	switch tool {
	case "create_note":
		a["content"] = "new\n"
	case "update_note":
		a["content"] = "changed\n"
		a["revision"] = revision
	case "append_to_note":
		a["text"] = "more\n"
	}
	return a
}

// try calls a tool. The error that it returns is that of the protocol, which a
// tool that is not there gets; a tool that fails answers with an error result.
func (a *agent) try(tool string, arguments any) (*sdk.CallToolResult, error) {
	return a.session.CallTool(a.ctx, &sdk.CallToolParams{Name: tool, Arguments: arguments})
}

func (a *agent) call(tool string, arguments any) *sdk.CallToolResult {
	a.t.Helper()
	res, err := a.try(tool, arguments)
	if err != nil {
		a.t.Fatalf("%s: %v", tool, err)
	}
	if a.said != nil {
		a.said.WriteString(text(res) + "\n")
	}
	return res
}

// text is what a result says: for a failure the message, otherwise the JSON of
// what the tool returned.
func text(res *sdk.CallToolResult) string {
	if len(res.Content) > 0 {
		if c, ok := res.Content[0].(*sdk.TextContent); ok {
			return c.Text
		}
	}
	return ""
}

// fail calls a tool that has to answer with an error, and returns the message.
func (a *agent) fail(tool string, arguments any) string {
	a.t.Helper()
	res := a.call(tool, arguments)
	if !res.IsError {
		a.t.Fatalf("%s(%v) = %.300s, want an error", tool, arguments, text(res))
	}
	return text(res)
}

// got calls a tool that has to succeed, and returns what it sent as a T.
func got[T any](a *agent, tool string, arguments any) T {
	a.t.Helper()
	res := a.call(tool, arguments)
	if res.IsError {
		a.t.Fatalf("%s(%v) failed: %s", tool, arguments, text(res))
	}
	data, err := json.Marshal(res.StructuredContent)
	if err != nil {
		a.t.Fatal(err)
	}
	var out T
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&out); err != nil {
		a.t.Fatalf("%s: %.300s is not the output that is documented: %v", tool, data, err)
	}
	return out
}

// seed writes a file of a vault the way another program would, modified the
// given number of hours after epoch.
func (a *agent) seed(name, rel, content string, hours int) {
	a.t.Helper()
	p := filepath.Join(a.dirs[name], filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		a.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		a.t.Fatal(err)
	}
	modified := epoch.Add(time.Duration(hours) * time.Hour)
	if err := os.Chtimes(p, modified, modified); err != nil {
		a.t.Fatal(err)
	}
}

// onDisk returns a file of a vault as it is; "<missing>" if there is none.
func (a *agent) onDisk(name, rel string) string {
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

// snapshot returns every file of the vaults by its vault and path, with its text,
// and a symlink as the word link.
func (a *agent) snapshot() map[string]string {
	a.t.Helper()
	files := map[string]string{}
	for name, root := range a.dirs {
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(root, p)
			key, data := name+"/"+filepath.ToSlash(rel), []byte("link")
			if d.Type()&fs.ModeSymlink == 0 {
				data, err = os.ReadFile(p)
			}
			files[key] = string(data)
			return err
		})
		if err != nil {
			a.t.Fatal(err)
		}
	}
	return files
}

// syncBuffer collects what the handler logs while the test reads it.
type syncBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
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

func TestToolsByMode(t *testing.T) {
	read := []string{"get_note", "list_notes", "list_vaults", "search_notes"}
	write := []string{"append_to_note", "create_note", "update_note"}
	for _, tc := range []struct {
		name    string
		options Options
		want    []string
	}{
		{"read-only", Options{}, read},
		{"write", Options{Write: true}, slices.Concat(write, read)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newAgent(t, tc.options)
			listed, err := a.session.ListTools(a.ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			var names []string
			for _, tool := range listed.Tools {
				names = append(names, tool.Name)
				if tool.Description == "" {
					t.Errorf("%s has no description", tool.Name)
				}
				// Read tools change nothing; none reaches beyond the vaults; one that
				// replaces what is in a note says so.
				switch hints := tool.Annotations; {
				case hints == nil || hints.OpenWorldHint == nil || *hints.OpenWorldHint || hints.DestructiveHint == nil:
					t.Errorf("%s: annotations = %+v, want them all said", tool.Name, hints)
				case hints.ReadOnlyHint != slices.Contains(read, tool.Name):
					t.Errorf("%s: ReadOnlyHint = %v", tool.Name, hints.ReadOnlyHint)
				case *hints.DestructiveHint != (tool.Name == "update_note"):
					t.Errorf("%s: DestructiveHint = %v", tool.Name, *hints.DestructiveHint)
				}
			}
			slices.Sort(names)
			if want := slices.Sorted(slices.Values(tc.want)); !slices.Equal(names, want) {
				t.Errorf("tools = %v, want %v", names, want)
			}
		})
	}

	// A write tool that is not offered cannot be called either, and nothing is made.
	a := newAgent(t, Options{})
	if res, err := a.try("create_note", args("vault", "notes", "path", "x.md", "content", "x")); err == nil {
		t.Errorf("create_note on a server without write tools = %v, want an error", res)
	}
	if files := a.snapshot(); len(files) != 0 {
		t.Errorf("files = %v, want none", files)
	}
}

const (
	alpha = "---\ntitle: Alpha Doc\ntags: [go, project/alpha]\n---\n# Alpha\nThe gopher digs.\n"
	beta  = "# Beta\nAnother gopher, with #go and #Draft in it.\n"
	bom   = "\xef\xbb\xbf"
	crlf  = bom + "---\r\ntags: [crlf]\r\n---\r\nBody  \r\n"
)

func TestListGetSearch(t *testing.T) {
	a := newAgent(t, Options{})
	a.seed("notes", "inbox/alpha.md", alpha, 1)
	a.seed("notes", "ideas/gamma.md", "Gamma has no tags.\n", 2)
	a.seed("notes", "inbox/beta.md", beta, 3)
	a.seed("notes", "root.md", "---\ntags: project\n---\nRoot.\n", 4)
	a.seed("notes", "broken.md", "---\na: [unclosed\n---\nBody #later\n", 0)
	a.seed("notes", "crlf.md", crlf, 0)
	// What is not a note that can be opened is not counted or listed.
	a.seed("notes", "pic.png", "png", 9)
	a.seed("notes", "bad.md", "\xff\xfe not UTF-8", 9)
	a.seed("notes", ".obsidian/hidden.md", "# Hidden #go\n", 9)
	a.seed("work", "plan.md", "Gopher migration plan\n", 5)
	a.seed("work", "gopher-notes.md", "Nothing here.\n", 0)

	t.Run("vaults", func(t *testing.T) {
		out := got[vaultsOut](a.at(t), "list_vaults", args())
		if len(out.Vaults) != 2 || out.Vaults[0].Name != "notes" || out.Vaults[0].Notes != 6 || out.Vaults[1].Name != "work" || out.Vaults[1].Notes != 2 {
			t.Errorf("list_vaults = %+v, want notes with 6 notes and work with 2, in the order they are configured", out.Vaults)
		}
	})

	t.Run("list", func(t *testing.T) {
		newest := []string{"root.md", "inbox/beta.md", "ideas/gamma.md", "inbox/alpha.md", "broken.md", "crlf.md"}
		both := []string{"inbox/beta.md", "inbox/alpha.md"}
		for _, tc := range []struct {
			name string
			args map[string]any
			want []string
		}{
			{"newest first", args("vault", "notes"), newest},
			{"folder", args("vault", "notes", "folder", "inbox"), both},
			{"folder with a slash", args("vault", "notes", "folder", "inbox/"), both},
			{"folder is whole names", args("vault", "notes", "folder", "in"), []string{}},
			{"folder that is not there", args("vault", "notes", "folder", "nope"), []string{}},
			{"tag", args("vault", "notes", "tag", "go"), both},
			{"tag with a # and another case", args("vault", "notes", "tag", "#GO"), both},
			{"tag with its nested tags", args("vault", "notes", "tag", "project"), []string{"root.md", "inbox/alpha.md"}},
			{"nested tag", args("vault", "notes", "tag", "project/alpha"), []string{"inbox/alpha.md"}},
			{"tag is whole names", args("vault", "notes", "tag", "proj"), []string{}},
			{"tag in the body of a note", args("vault", "notes", "tag", "later"), []string{"broken.md"}},
			{"folder and tag", args("vault", "notes", "folder", "inbox", "tag", "project"), []string{"inbox/alpha.md"}},
			{"other vault", args("vault", "work"), []string{"plan.md", "gopher-notes.md"}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				out := got[listOut](a.at(t), "list_notes", tc.args)
				if !slices.Equal(out.paths(), tc.want) || out.Total != len(tc.want) || out.NextOffset != nil {
					t.Errorf("list_notes(%v) = %v, total %d, next_offset %v; want %v, total %d and no next_offset", tc.args, out.paths(), out.Total, out.NextOffset, tc.want, len(tc.want))
				}
			})
		}

		if msg := a.at(t).fail("list_notes", args("vault", "nope")); msg != msgNoVault {
			t.Errorf("list_notes of a vault that is not there: %q, want %q", msg, msgNoVault)
		}
		first := got[listOut](a.at(t), "list_notes", args("vault", "notes")).Notes
		if want := (summaryOut{Path: "root.md", Title: "root", Modified: stamp(4), Size: len("---\ntags: project\n---\nRoot.\n"), Tags: []string{"project"}}); !reflect.DeepEqual(first[0], want) {
			t.Errorf("first note = %+v, want %+v", first[0], want)
		}
		if gamma := first[2]; gamma.Path != "ideas/gamma.md" || gamma.Tags == nil || len(gamma.Tags) != 0 {
			t.Errorf("a note without tags = %+v, want tags as an empty list", gamma)
		}
	})

	t.Run("get", func(t *testing.T) {
		a := a.at(t)
		for _, tc := range []struct {
			path   string
			hours  int
			text   string
			tags   []string
			values map[string]any
		}{
			{"inbox/alpha.md", 1, alpha, []string{"go", "project/alpha"}, map[string]any{"title": "Alpha Doc", "tags": []any{"go", "project/alpha"}}},
			{"inbox/beta.md", 3, beta, []string{"Draft", "go"}, map[string]any{}}, // no frontmatter: an empty object
			{"ideas/gamma.md", 2, "Gamma has no tags.\n", []string{}, map[string]any{}},
			{"broken.md", 0, "---\na: [unclosed\n---\nBody #later\n", []string{"later"}, map[string]any{}}, // YAML that cannot be read
			{"crlf.md", 0, crlf, []string{"crlf"}, map[string]any{"tags": []any{"crlf"}}},                  // the text is as it is on disk
		} {
			want := noteOut{
				Vault: "notes", Path: tc.path, Title: strings.TrimSuffix(filepath.Base(tc.path), ".md"), Content: tc.text,
				Revision: vault.Revision([]byte(tc.text)), Modified: stamp(tc.hours), Tags: tc.tags, Frontmatter: tc.values,
			}
			if note := got[noteOut](a, "get_note", args("vault", "notes", "path", tc.path)); !reflect.DeepEqual(note, want) {
				t.Errorf("get_note(%s) = %+v, want %+v", tc.path, note, want)
			}
		}
		if msg := a.fail("get_note", args("vault", "notes", "path", "nope.md")); msg != msgNotFound {
			t.Errorf("get_note of a note that is not there: %q, want %q", msg, msgNotFound)
		}
		if msg := a.fail("get_note", args("vault", "nope", "path", "root.md")); msg != msgNoVault {
			t.Errorf("get_note in a vault that is not there: %q, want %q", msg, msgNoVault)
		}
		// Notes that cannot be opened say why.
		for path, want := range map[string]string{"bad.md": msgNotNote, "pic.png": msgNotNote} {
			if msg := a.fail("get_note", args("vault", "notes", "path", path)); msg != want {
				t.Errorf("get_note(%s): %q, want %q", path, msg, want)
			}
		}
	})

	t.Run("search", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			args map[string]any
			want []string
		}{
			// A name that has the word ranks first in any vault, then the newest.
			{"every vault", args("query", "gopher"), []string{"work:gopher-notes.md", "work:plan.md", "notes:inbox/beta.md", "notes:inbox/alpha.md"}},
			{"one vault", args("query", "gopher", "vault", "notes"), []string{"notes:inbox/beta.md", "notes:inbox/alpha.md"}},
			{"every word", args("query", "gopher digs"), []string{"notes:inbox/alpha.md"}},
			{"a phrase", args("query", `"gopher digs"`), []string{"notes:inbox/alpha.md"}},
			{"a phrase out of order", args("query", `"digs gopher"`), []string{}},
			{"case and accents", args("query", "GÓPHER", "vault", "work"), []string{"work:gopher-notes.md", "work:plan.md"}},
			{"a path", args("query", "ideas"), []string{"notes:ideas/gamma.md"}},
			{"nothing", args("query", "zzzz"), []string{}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				out := got[searchOut](a.at(t), "search_notes", tc.args)
				if !slices.Equal(out.ids(), tc.want) || out.Total != len(tc.want) || out.NextOffset != nil {
					t.Errorf("search_notes(%v) = %v, total %d, next_offset %v; want %v, total %d and no next_offset", tc.args, out.ids(), out.Total, out.NextOffset, tc.want, len(tc.want))
				}
			})
		}

		a := a.at(t)
		// The lines that matched, as they are written, with their numbers.
		hit := got[searchOut](a, "search_notes", args("query", "digs")).Hits[0]
		if len(hit.Snippets) != 1 || hit.Snippets[0].Line != 6 || hit.Snippets[0].Text != "The gopher digs." || hit.Title != "alpha" {
			t.Errorf("hit = %+v, want the title alpha and the snippet of line 6", hit)
		}
		// A note found by its path alone has none, and the list is not null.
		if hit := got[searchOut](a, "search_notes", args("query", "ideas")).Hits[0]; hit.Snippets == nil || len(hit.Snippets) != 0 {
			t.Errorf("hit by path = %+v, want an empty list of snippets", hit)
		}
		for _, query := range []string{"", "  ", `""`} {
			if msg := a.fail("search_notes", args("query", query)); msg != msgEmptyQuery {
				t.Errorf("search_notes(%q): %q, want %q", query, msg, msgEmptyQuery)
			}
		}
		if msg := a.fail("search_notes", args("query", "a", "vault", "nope")); msg != msgNoVault {
			t.Errorf("search_notes in a vault that is not there: %q, want %q", msg, msgNoVault)
		}
	})

	t.Run("paging", testPaging)
}

// testPaging is the paging of list_notes and search_notes, a part of
// TestListGetSearch that has notes of its own.
func testPaging(t *testing.T) {
	// More notes than a search takes at most from a vault by default (50), so that
	// the totals and the pages past that are checked too.
	const notes = 120
	a := newAgent(t, Options{})
	for i := range notes {
		a.seed("notes", fmt.Sprintf("n%03d.md", i), "needle\n", i)
	}
	// Both tools page through the same notes, newest first.
	base := map[string][]any{"list_notes": {"vault", "notes"}, "search_notes": {"query", "needle"}}
	page := func(tool string, pairs ...any) (paths []string, total int, next *int) {
		arguments := args(append(slices.Clone(base[tool]), pairs...)...)
		if tool == "list_notes" {
			out := got[listOut](a, tool, arguments)
			return out.paths(), out.Total, out.NextOffset
		}
		out := got[searchOut](a, tool, arguments)
		for _, id := range out.ids() {
			paths = append(paths, strings.TrimPrefix(id, "notes:"))
		}
		return paths, out.Total, out.NextOffset
	}
	// newest returns count notes from the one at position from, newest first.
	newest := func(from, count int) []string {
		paths := []string{}
		for i := from; i < from+count; i++ {
			paths = append(paths, fmt.Sprintf("n%03d.md", notes-1-i))
		}
		return paths
	}

	for tool := range base {
		for _, tc := range []struct {
			name        string
			args        []any
			from, count int
			next        int // -1 for none
		}{
			{"the default is 20", nil, 0, 20, 20},
			{"a page in the middle", []any{"limit", 5, "offset", 10}, 10, 5, 15},
			{"a page of one", []any{"limit", 1}, 0, 1, 1},
			{"the most", []any{"limit", 100}, 0, 100, 100},
			{"the rest after the most", []any{"limit", 100, "offset", 100}, 100, 20, -1},
			{"across 50", []any{"limit", 100, "offset", 10}, 10, 100, 110},
			{"exactly the rest", []any{"offset", 100}, 100, 20, -1},
			{"one short of the rest", []any{"limit", 19, "offset", 100}, 100, 19, 119},
			{"past the end", []any{"offset", 1000000}, notes, 0, -1},
		} {
			paths, total, next := page(tool, tc.args...)
			if !slices.Equal(paths, newest(tc.from, tc.count)) || total != notes || (next == nil) != (tc.next < 0) || next != nil && *next != tc.next {
				t.Errorf("%s %s: %d notes from %.8v, total %d, next_offset %v; want %d from %.8v, total %d and next_offset %d",
					tool, tc.name, len(paths), paths, total, next, tc.count, newest(tc.from, tc.count), notes, tc.next)
			}
		}

		// Out of bounds is refused, and so is what is not an integer.
		for _, bad := range [][]any{{"limit", 0}, {"limit", -1}, {"limit", 101}, {"offset", -1}} {
			if msg := a.fail(tool, args(append(slices.Clone(base[tool]), bad...)...)); msg != msgPaging {
				t.Errorf("%s %v: %q, want %q", tool, bad, msg, msgPaging)
			}
		}
		for _, limit := range []any{1.5, "ten"} {
			a.fail(tool, args(append(slices.Clone(base[tool]), "limit", limit)...))
		}
	}
}

func TestExcludeHidesPaths(t *testing.T) {
	a := newAgent(t, Options{Write: true, Exclude: map[string][]string{
		// The patterns are not in the case of the folders they are for. Matching
		// ignores case, as a file system may: there, Transcripts and transcripts are
		// one folder, and so are the two paths of a note.
		"notes": {"Transcripts", "journal", "**/private", "**/*.secret.md"},
		"work":  {"**"},
	}})
	a.seed("notes", "inbox.md", "# Inbox\nBudget review #public\n", 1)
	a.seed("notes", "projects/plan.md", "# Plan\nThe budget plan.\n", 2)
	a.seed("notes", "projects/budget.md", "# Budget\nNumbers.\n", 3)
	type file struct{ vault, path, text string }
	hidden := []file{ // they are newer than what is visible: a leak would head the lists
		{"notes", "transcripts/call.md", "---\ntags: [confidential]\n---\n# Call\nzebrafish and the budget.\n"},
		{"notes", "transcripts/2026/deep/nested.md", "zebrafish nested\n"},
		{"notes", "Journal/entry.md", "zebrafish diary\n"},
		{"notes", "projects/Private/draft.md", "zebrafish draft\n"},
		{"notes", "projects/old.secret.md", "zebrafish old\n"},
		{"work", "w1.md", "zebrafish work\n"},
		{"work", "sub/w2.md", "zebrafish more work\n"},
	}
	texts := map[string]string{} // vault:path -> text of what is hidden
	for i, f := range hidden {
		a.seed(f.vault, f.path, f.text, 4+i)
		texts[f.vault+":"+f.path] = f.text
	}

	a.said = &strings.Builder{} // from here, all that the tools say is looked at for what is excluded

	// The counts, the lists and the searches have none of it. A vault with
	// everything excluded is empty, not an error.
	if out := got[vaultsOut](a, "list_vaults", args()); len(out.Vaults) != 2 || out.Vaults[0].Notes != 3 || out.Vaults[1].Name != "work" || out.Vaults[1].Notes != 0 {
		t.Errorf("list_vaults = %+v, want 3 notes in notes and 0 in work", out.Vaults)
	}
	if out := got[listOut](a, "list_notes", args("vault", "notes")); !slices.Equal(out.paths(), []string{"projects/budget.md", "projects/plan.md", "inbox.md"}) || out.Total != 3 {
		t.Errorf("list_notes = %v, total %d, want the three that are visible", out.paths(), out.Total)
	}
	if out := got[listOut](a, "list_notes", args("vault", "work")); len(out.Notes) != 0 || out.Total != 0 || out.Notes == nil {
		t.Errorf("list_notes of a vault that is all excluded = %+v, want none", out)
	}
	if out := got[searchOut](a, "search_notes", args("query", "budget")); !slices.Equal(out.ids(), []string{"notes:projects/budget.md", "notes:projects/plan.md", "notes:inbox.md"}) || out.Total != 3 {
		t.Errorf("search_notes budget = %v, total %d, want the three that are visible, and not the transcript that has the word", out.ids(), out.Total)
	}
	for _, word := range []string{"zebrafish", "transcripts", "call", "nested", "journal", "diary", "entry", "private", "draft", "secret", "confidential"} {
		for _, vaultName := range []string{"", "notes", "work"} {
			if out := got[searchOut](a, "search_notes", args("query", word, "vault", vaultName)); out.Total != 0 || len(out.Hits) != 0 || out.Hits == nil {
				t.Errorf("search_notes %q in %q = %v, want nothing: all that has it is excluded", word, vaultName, out.ids())
			}
		}
	}
	for _, list := range []map[string]any{
		args("vault", "notes", "folder", "transcripts"), args("vault", "notes", "folder", "TRANSCRIPTS/2026"),
		args("vault", "notes", "folder", "Journal"), args("vault", "notes", "folder", "projects/private"),
		args("vault", "notes", "tag", "confidential"),
	} {
		if out := got[listOut](a, "list_notes", list); out.Total != 0 || len(out.Notes) != 0 {
			t.Errorf("list_notes(%v) = %v, want nothing", list, out.paths())
		}
	}
	// A folder that is excluded is a folder that is not there, to the letter.
	if hiddenFolder, missing := text(a.call("list_notes", args("vault", "notes", "folder", "Transcripts"))), text(a.call("list_notes", args("vault", "notes", "folder", "nope"))); hiddenFolder != missing {
		t.Errorf("list_notes of an excluded folder = %s, want the answer for a folder that is not there, %s", hiddenFolder, missing)
	}

	// An excluded note gets, from every tool and whatever revision is sent, the
	// answer that a note that is not there gets, so that nothing tells them apart.
	// So does a note that is not there in an excluded folder.
	const absent = "projects/nothing.md"
	missing := a.fail("get_note", args("vault", "notes", "path", absent))
	if missing != msgNotFound {
		t.Fatalf("get_note of a note that is not there: %q, want %q", missing, msgNotFound)
	}
	for _, tool := range []string{"update_note", "append_to_note"} {
		if msg := a.fail(tool, onNote(tool, "notes", absent, vault.Revision(nil))); msg != missing {
			t.Fatalf("%s of a note that is not there: %q, want %q", tool, msg, missing)
		}
	}
	before := a.snapshot()
	for _, row := range []struct{ vault, request, file string }{
		{"notes", "transcripts/call.md", "transcripts/call.md"},
		{"notes", "TRANSCRIPTS/call.md", "transcripts/call.md"}, // on a file system that ignores case it is the same file
		{"notes", "Transcripts/2026/deep/nested.md", "transcripts/2026/deep/nested.md"},
		{"notes", "Journal/entry.md", "Journal/entry.md"},
		{"notes", "journal/entry.md", "Journal/entry.md"},
		{"notes", "JOURNAL/ENTRY.md", "Journal/entry.md"},
		{"notes", "projects/Private/draft.md", "projects/Private/draft.md"},
		{"notes", "projects/private/draft.md", "projects/Private/draft.md"},
		{"notes", "projects/old.secret.md", "projects/old.secret.md"},
		{"notes", "projects/OLD.Secret.md", "projects/old.secret.md"},
		{"work", "w1.md", "w1.md"},
		{"work", "sub/w2.md", "sub/w2.md"},
		// Notes that are not there, in what is excluded: the answer is the same.
		{"notes", "transcripts/never.md", ""},
		{"notes", "Journal/never.md", ""},
		{"notes", "projects/x/private/never.md", ""},
		{"notes", "projects/never.secret.md", ""},
		{"work", "never.md", ""},
	} {
		revisions := []string{strings.Repeat("0", 64)}
		if row.file != "" {
			revisions = append(revisions, vault.Revision([]byte(texts[row.vault+":"+row.file])))
		}
		for _, revision := range revisions {
			for _, tool := range noteTools {
				if msg := a.fail(tool, onNote(tool, row.vault, row.request, revision)); msg != missing {
					t.Errorf("%s of %s:%s with revision %.4s: %q, want %q, what a note that is not there gets", tool, row.vault, row.request, revision, msg, missing)
				}
			}
		}
	}
	if after := a.snapshot(); !reflect.DeepEqual(before, after) {
		t.Errorf("the vaults changed: %v, want what was excluded left as it was, and nothing made in it", after)
	}

	// What is wrong with a path itself is said whether it is excluded or not, so
	// that no answer tells that a folder is excluded.
	for _, pair := range [][2]string{
		{"transcripts/call.txt", "projects/call.txt"},
		{"transcripts/../x.md", "projects/../x.md"},
		{"Journal/.git/x.md", "projects/.git/x.md"},
	} {
		for _, tool := range noteTools {
			inside, outside := a.fail(tool, onNote(tool, "notes", pair[0], "r")), a.fail(tool, onNote(tool, "notes", pair[1], "r"))
			if inside != outside || inside == missing {
				t.Errorf("%s: %q for %s and %q for %s; want one answer that says what is wrong with the path", tool, inside, pair[0], outside, pair[1])
			}
		}
	}

	// What is not excluded works as it does without excludes.
	plan := got[noteOut](a, "get_note", args("vault", "notes", "path", "projects/plan.md"))
	got[noteOut](a, "update_note", args("vault", "notes", "path", "projects/plan.md", "content", "# Plan\nchanged\n", "revision", plan.Revision))
	got[noteOut](a, "append_to_note", args("vault", "notes", "path", "inbox.md", "text", "more\n"))
	got[noteOut](a, "create_note", args("vault", "notes", "path", "projects/new.md", "content", "new\n"))
	for path, want := range map[string]string{"projects/plan.md": "# Plan\nchanged\n", "inbox.md": "# Inbox\nBudget review #public\nmore\n", "projects/new.md": "new\n"} {
		if disk := a.onDisk("notes", path); disk != want {
			t.Errorf("%s on disk = %q, want %q", path, disk, want)
		}
	}

	// Nothing that the tools said, in an answer or an error, has any of what is
	// only in what is excluded.
	for _, secret := range []string{"zebrafish", "transcripts", "journal", "private", "secret", "confidential", "call.md", "nested", "entry", "draft", "diary", "w1", "w2"} {
		if strings.Contains(strings.ToLower(a.said.String()), secret) {
			t.Errorf("what the tools said has %q, which is only in what is excluded", secret)
		}
	}
}

// An excluded note, even one that exists, is answered as the vault answers for a
// note that is not there: with the same refusal for every fault of the request
// that the vault finds before it looks at the disk, and with not found for the
// rest. The vault itself is the reference, so no answer tells that a path is
// excluded.
func TestExcludedPathsAreAnsweredLikeMissingOnes(t *testing.T) {
	hide, err := vault.ExcludeGlobs([]string{"hidden"})
	if err != nil {
		t.Fatal(err)
	}
	answer := func(err error) string {
		if err == nil {
			return "<nil>"
		}
		return safeError(err).Error()
	}
	for _, tc := range []struct {
		name, rest, content string
		readOnly            bool
	}{
		{"a note that is not there", "x.md", "ok", false},
		{"not a note", "x.txt", "ok", false},
		{"not a valid path", "../x.md", "ok", false},
		{"a hidden name", ".git/x.md", "ok", false},
		{"too large", "x.md", strings.Repeat("x", vault.MaxNoteBytes+1), false},
		{"not UTF-8", "x.md", "\xff", false},
		{"a vault that is read-only", "x.md", "ok", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			v, err := vault.Open("notes", dir, vault.Options{ReadOnly: tc.readOnly})
			if err != nil {
				t.Fatal(err)
			}
			defer v.Close()
			s := scope{v: v, visible: hide}
			excluded, missing := "hidden/"+tc.rest, "plain/"+tc.rest
			// The excluded note is there, and the other is not.
			if !strings.Contains(tc.rest, "..") {
				if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, excluded)), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, excluded), []byte("existing\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			_, read := v.Read(missing)
			_, readExcluded := s.Read(excluded)
			_, write := v.Write(missing, tc.content, "revision")
			_, writeExcluded := s.Write(excluded, tc.content, "revision")
			// Creating a note that is not there makes it, so where nothing refuses the
			// request the answer for the excluded path is the one of every other tool.
			_, create := v.Create(missing, tc.content)
			wantCreate := answer(create)
			if create == nil {
				wantCreate = msgNotFound
			}
			_, createExcluded := s.Create(excluded, tc.content)
			for _, c := range []struct{ tool, got, want string }{
				{"read", answer(readExcluded), answer(read)},
				{"write", answer(writeExcluded), answer(write)},
				{"create", answer(createExcluded), wantCreate},
			} {
				if c.got != c.want {
					t.Errorf("%s: %q for the excluded path, want %q, as for a path that is not there", c.tool, c.got, c.want)
				}
			}
		})
	}
}

func TestWriteTools(t *testing.T) {
	a := newAgent(t, Options{Write: true})
	const path = "new/deep/Made.md"
	made := "---\r\ntags: [made]\r\n---\r\nBody  \r\n" // nothing in it may change on the way
	note := func(arguments map[string]any) noteOut { return got[noteOut](a, "get_note", arguments) }
	at := args("vault", "notes", "path", path)

	// A note is made as it is sent, with the folders above it, and can be read.
	created := got[noteOut](a, "create_note", args("vault", "notes", "path", path, "content", made))
	if modified, err := time.Parse(time.RFC3339, created.Modified); err != nil || time.Since(modified) > time.Minute || modified.After(time.Now().Add(time.Minute)) {
		t.Errorf("modified = %q, want the time of the write", created.Modified)
	}
	want := noteOut{
		Vault: "notes", Path: path, Title: "Made", Content: made, Revision: vault.Revision([]byte(made)), Modified: created.Modified,
		Tags: []string{"made"}, Frontmatter: map[string]any{"tags": []any{"made"}},
	}
	if !reflect.DeepEqual(created, want) || !reflect.DeepEqual(note(at), want) || a.onDisk("notes", path) != made {
		t.Errorf("create_note = %+v, want %+v, which get_note reads and is on disk, as sent", created, want)
	}
	// It never replaces a note.
	if msg := a.fail("create_note", args("vault", "notes", "path", path, "content", "other\n")); msg != msgExists || a.onDisk("notes", path) != made {
		t.Errorf("create_note over a note: %q with %q on disk, want %q and the note as it was", msg, a.onDisk("notes", path), msgExists)
	}

	// A note is saved over the revision that was read, and only that one.
	replaced := "# Made again\n"
	updated := got[noteOut](a, "update_note", args("vault", "notes", "path", path, "content", replaced, "revision", created.Revision))
	if updated.Content != replaced || updated.Revision != vault.Revision([]byte(replaced)) || a.onDisk("notes", path) != replaced {
		t.Errorf("update_note = %+v with %q on disk, want the new text and its revision", updated, a.onDisk("notes", path))
	}
	for _, revision := range []string{created.Revision, ""} {
		if msg := a.fail("update_note", args("vault", "notes", "path", path, "content", "lost\n", "revision", revision)); msg != msgConflict || a.onDisk("notes", path) != replaced {
			t.Errorf("update_note over revision %q: %q with %q on disk, want %q and the note as it was", revision, msg, a.onDisk("notes", path), msgConflict)
		}
	}
	if msg := a.fail("update_note", args("vault", "notes", "path", path, "content", "x")); !strings.Contains(msg, "revision") || a.onDisk("notes", path) != replaced {
		t.Errorf("update_note without a revision: %q, want it said that the revision is missing", msg)
	}
	// A note that is gone is not there, which is not a conflict, and is not made again.
	a.seed("notes", "gone.md", "# Gone\n", 1)
	loaded := note(args("vault", "notes", "path", "gone.md")).Revision
	if err := os.Remove(filepath.Join(a.dirs["notes"], "gone.md")); err != nil {
		t.Fatal(err)
	}
	if msg := a.fail("update_note", args("vault", "notes", "path", "gone.md", "content", "# Back\n", "revision", loaded)); msg != msgNotFound || a.onDisk("notes", "gone.md") != "<missing>" {
		t.Errorf("update_note of a note that is gone: %q, %q on disk; want %q and nothing made", msg, a.onDisk("notes", "gone.md"), msgNotFound)
	}

	// A newline is added when the note does not end in one, and nothing else
	// changes: the text that was there is kept to the byte.
	for _, tc := range []struct{ name, before, text, want string }{
		{"no newline at the end", "abc", "def", "abc\ndef"},
		{"newline at the end", "abc\n", "def", "abc\ndef"},
		{"two newlines at the end", "abc\n\n", "def\n", "abc\n\ndef\n"},
		{"CRLF at the end", "a\r\nb\r\n", "c", "a\r\nb\r\nc"},
		{"CRLF text without a newline at the end", "a\r\nb", "c", "a\r\nb\nc"},
		{"BOM and frontmatter", bom + "---\ntags: [a]\n---\nBody  ", "tail\n", bom + "---\ntags: [a]\n---\nBody  \ntail\n"},
		{"text with breaks of its own", "abc\n", "x\ny\n\n", "abc\nx\ny\n\n"},
		{"an empty note", "", "first\n", "first\n"},
	} {
		t.Run("append "+tc.name, func(t *testing.T) {
			a := a.at(t)
			a.seed("notes", "log.md", tc.before, 1)
			out := got[noteOut](a, "append_to_note", args("vault", "notes", "path", "log.md", "text", tc.text))
			if disk := a.onDisk("notes", "log.md"); disk != tc.want || out.Content != tc.want || out.Revision != vault.Revision([]byte(tc.want)) || !strings.HasPrefix(disk, tc.before) {
				t.Errorf("append_to_note: on disk %q, answered %q; want %q", disk, out.Content, tc.want)
			}
		})
	}
	a.seed("notes", "full.md", strings.Repeat("a", vault.MaxNoteBytes), 1)
	for _, tc := range []struct {
		name, path, text, want string
	}{
		{"a note that is not there", "nope.md", "x", msgNotFound},
		{"nothing to add", "log.md", "", msgNoText},
		{"more than a note can hold", "full.md", "x", msgTooLarge},
	} {
		before := a.snapshot()
		if msg := a.fail("append_to_note", args("vault", "notes", "path", tc.path, "text", tc.text)); msg != tc.want || !reflect.DeepEqual(before, a.snapshot()) {
			t.Errorf("append_to_note, %s: %q, want %q and nothing changed", tc.name, msg, tc.want)
		}
	}
	for _, tool := range noteTools {
		if msg := a.fail(tool, onNote(tool, "nope", "x.md", "r")); msg != msgNoVault {
			t.Errorf("%s in a vault that is not there: %q, want %q", tool, msg, msgNoVault)
		}
	}

	// A vault that is read-only says so, whatever the tools are.
	ro := newAgent(t, Options{Write: true}, vault.Options{ReadOnly: true})
	ro.seed("notes", "a.md", "# A\n", 1)
	for _, c := range []struct {
		tool string
		args map[string]any
	}{
		{"create_note", args("vault", "notes", "path", "new.md", "content", "x")},
		{"update_note", args("vault", "notes", "path", "a.md", "content", "x", "revision", vault.Revision([]byte("# A\n")))},
		{"append_to_note", args("vault", "notes", "path", "a.md", "text", "x")},
	} {
		if msg := ro.fail(c.tool, c.args); msg != msgReadOnly {
			t.Errorf("%s in a read-only vault: %q, want %q", c.tool, msg, msgReadOnly)
		}
	}
	if files := ro.snapshot(); !reflect.DeepEqual(files, map[string]string{"notes/a.md": "# A\n"}) {
		t.Errorf("files = %v, want only a.md as it was", files)
	}
}

// another is a vault where someone else changes a note right after the first
// time that it is read: the moment between the read and the write of an append,
// which a test cannot hit by chance.
type another struct {
	*vault.Vault
	change func()
	once   sync.Once
}

func (v *another) Read(p string) (vault.Note, error) {
	n, err := v.Vault.Read(p)
	v.once.Do(v.change)
	return n, err
}

// An append is written over the revision that was read, so a change made before
// the write is a conflict: the append is the one that is lost, not the change.
func TestAppendDoesNotOverwriteAChangeInBetween(t *testing.T) {
	dir := t.TempDir()
	v, err := vault.Open("notes", dir, vault.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	if _, err := v.Create("a.md", "one\n"); err != nil {
		t.Fatal(err)
	}
	other := &another{Vault: v, change: func() {
		if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("one\nother writer\n"), 0o644); err != nil {
			t.Error(err)
		}
	}}
	if _, err := appendTo(other, "a.md", "mine\n"); !errors.Is(err, vault.ErrConflict) {
		t.Errorf("appendTo = %v, want a conflict", err)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "a.md")); string(data) != "one\nother writer\n" {
		t.Errorf("note = %q, want the change of the other writer, as it was", data)
	}
}

func TestToolsRejectUnsafePaths(t *testing.T) {
	a := newAgent(t, Options{Write: true})
	a.seed("notes", "ok.md", "# Ok\n", 1)
	a.seed("notes", ".obsidian/app.md", "# Config\n", 1)
	// A folder that is a link to one outside the vault, with a note in it.
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.md"), []byte("outside-secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(a.dirs["notes"], "link")); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	before := a.snapshot()
	a.said = &strings.Builder{}

	for _, tc := range []struct{ path, want string }{
		{"../x.md", msgInvalid},
		{"a/../../x.md", msgInvalid},
		{".git/config", msgInvalid},
		{".obsidian/app.md", msgInvalid},
		{"/etc/passwd.md", msgInvalid},
		{"/etc/passwd", msgInvalid},
		{`a\b.md`, msgInvalid},
		{"a//b.md", msgInvalid},
		{"ok.md/", msgInvalid},
		{"", msgInvalid},
		{"link/secret.md", msgInvalid}, // a link on the way, which leaves the vault
		{"link/new.md", msgInvalid},
		{"ok.txt", msgNotNote},
	} {
		for _, tool := range noteTools {
			if msg := a.fail(tool, onNote(tool, "notes", tc.path, "r")); msg != tc.want {
				t.Errorf("%s(%q): %q, want %q", tool, tc.path, msg, tc.want)
			}
		}
	}
	for _, folder := range []string{"../x", ".git", ".obsidian", "/abs", "a//b", "link/.."} {
		if msg := a.fail("list_notes", args("vault", "notes", "folder", folder)); msg != msgInvalid {
			t.Errorf("list_notes folder %q: %q, want %q", folder, msg, msgInvalid)
		}
	}
	// Nothing was changed or made, in the vault or outside it, and the note
	// outside is not listed or found.
	if after := a.snapshot(); !reflect.DeepEqual(before, after) {
		t.Errorf("the vault changed: %v, want %v", after, before)
	}
	if data, err := os.ReadFile(filepath.Join(outside, "secret.md")); err != nil || string(data) != "outside-secret\n" {
		t.Errorf("the note outside the vault = %q, %v, want it as it was", data, err)
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 1 {
		t.Errorf("the folder outside the vault = %v, want only secret.md", entries)
	}
	if out := got[listOut](a, "list_notes", args("vault", "notes")); !slices.Equal(out.paths(), []string{"ok.md"}) {
		t.Errorf("list_notes = %v, want only ok.md: not the hidden folder or the link", out.paths())
	}
	if out := got[searchOut](a, "search_notes", args("query", "secret")); len(out.Hits) != 0 || strings.Contains(a.said.String(), "outside-secret") {
		t.Errorf("search_notes secret = %v, want nothing from outside the vault", out.ids())
	}
}

func TestErrorsAreSafe(t *testing.T) {
	logs := captureLog(t)

	// What each error of the vault becomes, in the one place that decides it.
	t.Run("mapping", func(t *testing.T) {
		const secret = "/home/daniel/vaults/notes"
		for _, tc := range []struct {
			err  error
			want string
		}{
			{vault.ErrNotFound, msgNotFound},
			{vault.ErrInvalidPath, msgInvalid},
			{vault.ErrNotNote, msgNotNote},
			{vault.ErrTooLarge, msgTooLarge},
			{vault.ErrExists, msgExists},
			{vault.ErrConflict, msgConflict},
			{&vault.ConflictError{Current: "abc"}, msgConflict},
			{vault.ErrReadOnly, msgReadOnly},
			{vault.ErrEmptyQuery, msgEmptyQuery},
			// A sentinel that is wrapped keeps its own message and loses what wraps it.
			{fmt.Errorf("vault %q at %s: %w", "notes", secret, vault.ErrNotFound), msgNotFound},
			{context.Canceled, "Request canceled"},
			{context.DeadlineExceeded, "Request canceled"},
			// Anything else says nothing of what failed.
			{fmt.Errorf("vault %q unavailable: stat %s: no such file or directory", "notes", secret), msgGeneric},
			{&fs.PathError{Op: "open", Path: secret + "/a.md", Err: fs.ErrPermission}, msgGeneric},
			{errors.New("disk on fire"), msgGeneric},
		} {
			if got := safeError(tc.err); got == nil || got.Error() != tc.want {
				t.Errorf("safeError(%v) = %v, want %q", tc.err, got, tc.want)
			}
		}
		if safeError(nil) != nil {
			t.Errorf("safeError(nil) = %v, want nil", safeError(nil))
		}
		// The causes of the failures are for the operator, who reads the log; a
		// client that left cancels what it asked for, and that is no failure.
		for _, cause := range []string{"disk on fire", "permission denied", "no such file or directory"} {
			if !strings.Contains(logs.String(), cause) {
				t.Errorf("log = %q, want the cause %q", logs.String(), cause)
			}
		}
		if strings.Contains(logs.String(), "canceled") {
			t.Errorf("log = %q, want a canceled request left out", logs.String())
		}
	})

	// A vault that fails, however it fails, tells nothing of where it is on disk
	// or of the system underneath.
	t.Run("failing vaults", func(t *testing.T) {
		a := newAgent(t, Options{Write: true})
		a.seed("notes", "a.md", "# A\n", 1)
		a.seed("notes", "locked.md", "# Locked\n", 1)
		dir := a.dirs["notes"]
		resolved, err := filepath.EvalSymlinks(filepath.Dir(dir))
		if err != nil {
			t.Fatal(err)
		}
		secrets := []string{dir, filepath.Dir(dir), filepath.Join(resolved, filepath.Base(dir)), resolved, a.dirs["work"]}
		osErrors := []string{"no such file", "permission denied", "not a directory", "is a directory", "file exists", "not permitted", "stat ", "open ", "openat", "errno"}
		check := func(tool string, arguments map[string]any, want string) {
			t.Helper()
			msg := a.fail(tool, arguments)
			if msg != want {
				t.Errorf("%s(%v): %q, want %q", tool, arguments, msg, want)
			}
			for _, secret := range secrets {
				if strings.Contains(msg, secret) {
					t.Errorf("%s: %q tells the vault directory %s", tool, msg, secret)
				}
			}
			for _, osError := range osErrors {
				if strings.Contains(strings.ToLower(msg), osError) {
					t.Errorf("%s: %q has the error of the system, %q", tool, msg, osError)
				}
			}
		}

		// A note that cannot be read.
		locked := filepath.Join(dir, "locked.md")
		if err := os.Chmod(locked, 0); err != nil {
			t.Fatal(err)
		}
		if _, err := os.ReadFile(locked); err != nil {
			for _, tool := range []string{"get_note", "update_note", "append_to_note"} {
				check(tool, onNote(tool, "notes", "locked.md", "r"), msgGeneric)
			}
		} else {
			t.Log("permissions mean nothing here (root?): the note that cannot be read is left out")
		}

		// A vault whose directory is gone.
		if err := os.RemoveAll(dir); err != nil {
			t.Fatal(err)
		}
		check("list_vaults", args(), msgGeneric)
		check("list_notes", args("vault", "notes"), msgGeneric)
		check("search_notes", args("query", "a"), msgGeneric)
		check("search_notes", args("query", "a", "vault", "notes"), msgGeneric)
		check("create_note", args("vault", "notes", "path", "new.md", "content", "x"), msgGeneric)
		check("get_note", args("vault", "notes", "path", "a.md"), msgNotFound)
		check("update_note", onNote("update_note", "notes", "a.md", "r"), msgNotFound)
		check("append_to_note", onNote("append_to_note", "notes", "a.md", "r"), msgNotFound)

		// The cause of those is in the log.
		if !strings.Contains(logs.String(), `vault "notes" unavailable`) {
			t.Errorf("log = %q, want the cause of the failures", logs.String())
		}
	})
}

// An exclude that cannot be read must hide more and not less. The settings of the
// binary refuse such a pattern before anything is served, so this is not reached
// from there; it is what a caller that did not check gets.
func TestInvalidExcludeHidesTheVault(t *testing.T) {
	logs := captureLog(t)
	a := newAgent(t, Options{Exclude: map[string][]string{"notes": {"/absolute"}}})
	a.seed("notes", "a.md", "# A needle\n", 1)
	a.seed("work", "b.md", "# B needle\n", 1)
	if out := got[vaultsOut](a, "list_vaults", args()); out.Vaults[0].Notes != 0 || out.Vaults[1].Notes != 1 {
		t.Errorf("list_vaults = %+v, want the vault with the pattern empty and the other as it is", out.Vaults)
	}
	if msg := a.fail("get_note", args("vault", "notes", "path", "a.md")); msg != msgNotFound {
		t.Errorf("get_note: %q, want %q", msg, msgNotFound)
	}
	if out := got[searchOut](a, "search_notes", args("query", "needle")); !slices.Equal(out.ids(), []string{"work:b.md"}) {
		t.Errorf("search_notes = %v, want only the note of the other vault", out.ids())
	}
	if !strings.Contains(logs.String(), "/absolute") {
		t.Errorf("log = %q, want the pattern that was refused", logs.String())
	}
}
