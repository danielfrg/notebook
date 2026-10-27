package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net/http/httptest"
	"os"
	"path/filepath"
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
