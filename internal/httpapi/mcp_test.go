package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// mcpClient connects an MCP client to the endpoint of the app.
func mcpClient(t *testing.T, a *app) (*sdk.ClientSession, context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	session, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil).
		Connect(ctx, &sdk.StreamableClientTransport{Endpoint: a.url + "/mcp", DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatalf("connecting to /mcp: %v", err)
	}
	t.Cleanup(func() { session.Close() })
	return session, ctx
}

// mcpTools returns the names of the tools that the app offers at /mcp.
func mcpTools(t *testing.T, a *app) []string {
	t.Helper()
	session, ctx := mcpClient(t, a)
	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	return names
}

// The MCP tools that change notes are there when NOTEBOOK_MCP_WRITE says so and
// the instance is not read-only: read-only wins.
func TestMCPWriteToolsFollowReadOnly(t *testing.T) {
	read := []string{"get_note", "list_notes", "list_tags", "list_vaults", "search_notes"}
	all := slices.Concat([]string{"append_to_note", "create_note"}, read, []string{"update_note"})
	for _, tc := range []struct {
		name    string
		options Options
		want    []string
	}{
		{"nothing set", Options{}, read},
		{"MCP write", Options{MCPWrite: true}, all},
		{"read-only", Options{ReadOnly: true}, read},
		{"MCP write on a read-only instance", Options{MCPWrite: true, ReadOnly: true}, read},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newApp(t, withOptions(tc.options))
			if got := mcpTools(t, a); !slices.Equal(got, slices.Sorted(slices.Values(tc.want))) {
				t.Fatalf("tools = %v, want %v", got, slices.Sorted(slices.Values(tc.want)))
			}

			// What is offered works, and what is not offered is not there to call.
			session, ctx := mcpClient(t, a)
			res, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "create_note", Arguments: map[string]any{"vault": "notes", "path": "made.md", "content": "# Made\n"}})
			if slices.Equal(tc.want, all) {
				if err != nil || res.IsError || a.onDisk("notes", "made.md") != "# Made\n" {
					t.Errorf("create_note = %v, %v with %q on disk, want the note made", res, err, a.onDisk("notes", "made.md"))
				}
			} else if err == nil || a.onDisk("notes", "made.md") != "<missing>" {
				t.Errorf("create_note = %v, %v with %q on disk, want it refused and nothing made", res, err, a.onDisk("notes", "made.md"))
			}
		})
	}
}

// The endpoint is mounted with what the app is configured with: the hosts that
// it answers to, and the paths that the tools leave out. It is not under the
// guard of the API, which is for changes to the vaults through /api.
func TestMCPIsMounted(t *testing.T) {
	a := newApp(t, withOptions(Options{
		ReadOnly:        true,
		MCPAllowedHosts: []string{"notes.example.net"},
		MCPExclude:      map[string][]string{"notes": {"private"}},
	}))
	a.seed("notes", "open.md", "# Open\n")
	a.seed("notes", "private/diary.md", "# Diary\n")

	session, ctx := mcpClient(t, a)
	res, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "list_vaults"})
	if err != nil || res.IsError {
		t.Fatalf("list_vaults = %v, %v", res, err)
	}
	data, _ := json.Marshal(res.StructuredContent)
	if want := `{"vaults":[{"name":"notes","notes":1},{"name":"work","notes":0}]}`; string(data) != want {
		t.Errorf("list_vaults = %s, want %s: the note in the excluded folder is not counted", data, want)
	}
	res, err = session.CallTool(ctx, &sdk.CallToolParams{Name: "get_note", Arguments: map[string]any{"vault": "notes", "path": "private/diary.md"}})
	if err != nil || !res.IsError || len(res.Content) == 0 || strings.Contains(res.Content[0].(*sdk.TextContent).Text, "Diary") {
		t.Errorf("get_note of an excluded note = %v, %v, want an error that says nothing of it", res, err)
	}

	// Any other host is refused, and so is any method but POST.
	post := func(host string) int {
		req, err := http.NewRequest("POST", a.url+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Host = host
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		res, err := a.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	for host, want := range map[string]int{"notes.example.net": 200, "127.0.0.1": 200, "localhost:8080": 200, "evil.example": 403} {
		if got := post(host); got != want {
			t.Errorf("POST /mcp for host %s = %d, want %d", host, got, want)
		}
	}
	if res := a.get("/mcp").want(t, 405); res.Header.Get("Allow") != "POST" {
		t.Errorf("GET /mcp: Allow = %q, want POST", res.Header.Get("Allow"))
	}
}
