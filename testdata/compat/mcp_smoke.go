// mcp_smoke checks an MCP endpoint of notebook the way an agent uses it: it
// connects, lists the tools, lists the vaults and searches the notes.
//
//	go run ./testdata/compat/mcp_smoke.go [-query WORDS] [-absent WORDS] URL
//
// URL is the endpoint, for example http://127.0.0.1:8080/mcp. The search has to
// find a note: the words of -query, or by default the name of the newest note of
// the first vault that has any. With -absent it also has to find nothing for those
// words, which is how to check that a path is excluded from the tools.
// It exits with 1 and says why when something is not as expected.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// hit is a note that a search found, by the name of its vault and its path.
type hit struct{ Vault, Path string }

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "mcp_smoke: "+format+"\n", args...)
	os.Exit(1)
}

// call calls a tool that has to succeed, and decodes what it sends into out.
func call(ctx context.Context, session *sdk.ClientSession, tool string, arguments map[string]any, out any) {
	res, err := session.CallTool(ctx, &sdk.CallToolParams{Name: tool, Arguments: arguments})
	if err != nil {
		fail("%s: %v", tool, err)
	}
	if res.IsError {
		var messages []string
		for _, c := range res.Content {
			if text, ok := c.(*sdk.TextContent); ok {
				messages = append(messages, text.Text)
			}
		}
		fail("%s failed: %s", tool, strings.Join(messages, " "))
	}
	data, err := json.Marshal(res.StructuredContent)
	if err != nil {
		fail("%s: %v", tool, err)
	}
	if err := json.Unmarshal(data, out); err != nil {
		fail("%s: %s is not what is documented: %v", tool, data, err)
	}
}

func main() {
	query := flag.String("query", "", "words that the search has to find (default: the name of a note)")
	absent := flag.String("absent", "", "words that the search must not find")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: go run ./testdata/compat/mcp_smoke.go [-query WORDS] [-absent WORDS] URL")
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	session, err := sdk.NewClient(&sdk.Implementation{Name: "container-smoke", Version: "1"}, nil).
		Connect(ctx, &sdk.StreamableClientTransport{Endpoint: flag.Arg(0), DisableStandaloneSSE: true}, nil)
	if err != nil {
		fail("connecting to %s: %v", flag.Arg(0), err)
	}
	defer session.Close()

	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		fail("tools/list: %v", err)
	}
	var tools []string
	for _, tool := range listed.Tools {
		tools = append(tools, tool.Name)
	}
	for _, want := range []string{"list_vaults", "list_notes", "get_note", "search_notes"} {
		if !slices.Contains(tools, want) {
			fail("tools = %v, want %s among them", tools, want)
		}
	}

	var vaults struct {
		Vaults []struct {
			Name  string `json:"name"`
			Notes int    `json:"notes"`
		} `json:"vaults"`
	}
	call(ctx, session, "list_vaults", map[string]any{}, &vaults)
	if len(vaults.Vaults) == 0 {
		fail("list_vaults: no vaults")
	}

	// A note to find, and the path that has to be among the hits for its name.
	words, wantVault, wantPath := *query, "", ""
	if words == "" {
		for _, v := range vaults.Vaults {
			if v.Notes == 0 {
				continue
			}
			var notes struct {
				Notes []struct{ Path, Title string } `json:"notes"`
			}
			call(ctx, session, "list_notes", map[string]any{"vault": v.Name, "limit": 1}, &notes)
			if len(notes.Notes) == 0 {
				fail("list_notes %s: no notes, though list_vaults counts %d", v.Name, v.Notes)
			}
			words, wantVault, wantPath = notes.Notes[0].Title, v.Name, notes.Notes[0].Path
			break
		}
		if words == "" {
			fail("list_vaults: %+v: no vault has a note to search for, seed one or use -query", vaults.Vaults)
		}
	}

	var found struct {
		Hits  []hit `json:"hits"`
		Total int   `json:"total"`
	}
	call(ctx, session, "search_notes", map[string]any{"query": words}, &found)
	if found.Total == 0 || len(found.Hits) == 0 {
		fail("search_notes %q: no hits, want a note", words)
	}
	if wantPath != "" && !slices.Contains(found.Hits, hit{wantVault, wantPath}) {
		fail("search_notes %q: hits %+v, want %s in %s among them", words, found.Hits, wantPath, wantVault)
	}

	if *absent != "" {
		call(ctx, session, "search_notes", map[string]any{"query": *absent}, &found)
		if found.Total != 0 || len(found.Hits) != 0 {
			fail("search_notes %q: %d hits, want none: that path is meant to be excluded", *absent, found.Total)
		}
	}
	fmt.Printf("MCP initialize/list/call passed: %d tools, %d vaults, %q found\n", len(tools), len(vaults.Vaults), words)
}
