// Package mcp serves the notes of the vaults to agents over MCP: tools to list,
// read and search them and, when they are enabled, to create and change them.
//
// Some paths can be excluded from every tool (see Options.Exclude), to keep what
// is in them, recorded conversations for example, from the models of third
// parties that agents are built on. An excluded path is, to every tool, a path
// where there is no note: it is not counted, listed or found, and asking for it
// gets the answer that a note that is not there gets.
package mcp

import (
	"cmp"
	"context"
	"errors"
	"log"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/danielfrg/notebook/internal/vault"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Options are the settings of the tools.
type Options struct {
	// Write adds the tools that create and change notes. The caller has made it
	// from NOTEBOOK_MCP_WRITE and the read-only setting of the instance: it is
	// true only if the first says so and the second does not.
	Write bool
	// Exclude holds, by the name of a vault, the patterns of vault.ExcludeGlobs
	// for the paths that the tools leave out.
	Exclude map[string][]string
}

// The input and the output of the tools. What an agent reads and sends is their
// JSON, and the schemas of the tools are made from them: the jsonschema tag of a
// field says what it is.

type NoteInput struct {
	Vault string `json:"vault" jsonschema:"Name of the vault, as list_vaults gives it"`
	Path  string `json:"path" jsonschema:"Path of the note in the vault, with / between folders, for example Projects/Plan.md"`
}

type VaultInfo struct {
	Name  string `json:"name"`
	Notes int    `json:"notes" jsonschema:"How many notes the vault has"`
}

type VaultsOutput struct {
	Vaults []VaultInfo `json:"vaults"`
}

type ListNotesInput struct {
	Paging
	Vault  string `json:"vault" jsonschema:"Name of the vault, as list_vaults gives it"`
	Folder string `json:"folder,omitempty" jsonschema:"Only the notes in this folder and the folders in it, for example Projects/2026"`
	Tag    string `json:"tag,omitempty" jsonschema:"Only the notes with this tag, for example project or #project"`
}

type NoteSummary struct {
	Path     string    `json:"path"`
	Title    string    `json:"title" jsonschema:"The name of the file without .md"`
	Modified time.Time `json:"modified" jsonschema:"When the file was last changed, in UTC"`
	Size     int64     `json:"size" jsonschema:"Bytes"`
	Tags     []string  `json:"tags" jsonschema:"Without the #; the tags of the frontmatter and those in the text"`
}

type ListNotesOutput struct {
	Notes      []NoteSummary `json:"notes" jsonschema:"Newest first"`
	Total      int           `json:"total" jsonschema:"How many notes match, on all the pages"`
	NextOffset *int          `json:"next_offset" jsonschema:"The offset of the next page; null on the last"`
}

// Note is a note as the tools that read or write one send it.
type Note struct {
	Vault       string         `json:"vault"`
	Path        string         `json:"path"`
	Title       string         `json:"title" jsonschema:"The name of the file without .md"`
	Content     string         `json:"content" jsonschema:"The text of the file exactly as it is, frontmatter included"`
	Revision    string         `json:"revision" jsonschema:"Identifies this text: update_note needs it, and fails if the note has changed since"`
	Modified    time.Time      `json:"modified" jsonschema:"When the file was last changed, in UTC"`
	Tags        []string       `json:"tags" jsonschema:"Without the #; the tags of the frontmatter and those in the text"`
	Frontmatter map[string]any `json:"frontmatter" jsonschema:"The YAML properties at the top of the note; empty if there are none"`
}

type SearchInput struct {
	Paging
	Query string `json:"query" jsonschema:"Words, and \"quoted phrases\"; every one has to match, in the path or the text"`
	Vault string `json:"vault,omitempty" jsonschema:"Name of the vault to search; all of them if left out"`
	Tag   string `json:"tag,omitempty" jsonschema:"Only notes with this tag; nested tags count, for example project matches project/alpha"`
}

type ListTagsInput struct {
	Vault string `json:"vault,omitempty" jsonschema:"Name of the vault; all of them if left out"`
}

type TagCount struct {
	Vault string `json:"vault"`
	Tag   string `json:"tag"`
	Notes int    `json:"notes" jsonschema:"Number of visible notes with this tag"`
}

type ListTagsOutput struct {
	Tags []TagCount `json:"tags" jsonschema:"Tags in alphabetical order, grouped by vault"`
}

type Hit struct {
	Vault    string          `json:"vault"`
	Path     string          `json:"path"`
	Title    string          `json:"title" jsonschema:"The name of the file without .md"`
	Snippets []vault.Snippet `json:"snippets" jsonschema:"The first lines of the note that have a term, as they are written, with their line numbers; none for a note found by its path alone"`
}

type SearchOutput struct {
	Hits       []Hit `json:"hits" jsonschema:"Best first"`
	Total      int   `json:"total" jsonschema:"How many notes match, on all the pages"`
	NextOffset *int  `json:"next_offset" jsonschema:"The offset of the next page; null on the last"`
}

type CreateInput struct {
	NoteInput
	Content string `json:"content" jsonschema:"The whole text of the note, frontmatter included"`
}

type UpdateInput struct {
	NoteInput
	Content  string `json:"content" jsonschema:"The whole new text of the note, frontmatter included"`
	Revision string `json:"revision" jsonschema:"The revision that get_note gave"`
}

type AppendInput struct {
	NoteInput
	Text string `json:"text" jsonschema:"What to add at the end of the note"`
}

// annotation says what a tool does to the world: the read tools change nothing,
// and none of them reaches beyond the vaults.
func annotation(read bool) *sdk.ToolAnnotations {
	no := false
	return &sdk.ToolAnnotations{ReadOnlyHint: read, IdempotentHint: read, DestructiveHint: &no, OpenWorldHint: &no}
}

// The errors that are the tools' own, besides those of the vault.
var (
	errNoVault = errors.New("vault not found")
	errNoText  = errors.New("text is empty")
)

// What an agent is told of an error. Only these fixed texts are relayed: any
// other error can hold the absolute path of a vault, as the errors of the file
// system do. A wrapped one is told by its own text, not that of what wraps it.
var safeMessages = []struct {
	err     error
	message string
}{
	{vault.ErrNotFound, "Note not found"},
	{vault.ErrInvalidPath, "Invalid path"},
	{vault.ErrNotNote, "Not a UTF-8 Markdown note"},
	{vault.ErrTooLarge, "Note exceeds 2 MiB"},
	{vault.ErrExists, "Note already exists"},
	{vault.ErrConflict, "Note changed since it was read; read it again and retry"},
	{vault.ErrReadOnly, "Notes are read-only"},
	{vault.ErrEmptyQuery, "Query is empty"},
	{errNoVault, "Vault not found"},
	{errNoText, "Text is empty"},
	// A client that left cancels what it asked for, and that is no failure.
	{context.Canceled, "Request canceled"},
	{context.DeadlineExceeded, "Request canceled"},
}

// safeError turns an error into what an agent is told of it. What is not known to
// be safe is logged, and the agent is told only that the notes are unavailable.
func safeError(err error) error {
	if err == nil {
		return nil
	}
	for _, known := range safeMessages {
		if errors.Is(err, known.err) {
			return errors.New(known.message)
		}
	}
	log.Printf("mcp: %v", err)
	return errors.New("Notes unavailable; try again")
}

// when is a time as the tools send it: in UTC and to the second, which is all
// that an agent can use of it.
func when(t time.Time) time.Time { return t.UTC().Truncate(time.Second) }

func present(vaultName string, n vault.Note) Note {
	frontmatter, _ := vault.ParseFrontmatter(n.Content)
	return Note{
		Vault:       vaultName,
		Path:        n.Path,
		Title:       vault.Title(n.Path),
		Content:     n.Content,
		Revision:    n.Revision,
		Modified:    when(n.Modified),
		Tags:        vault.Tags(n.Content),
		Frontmatter: frontmatter,
	}
}

// onNote runs a tool that answers with a note, on the vault called name.
func (t *tools) onNote(name string, do func(scope) (vault.Note, error)) (*sdk.CallToolResult, Note, error) {
	s, err := t.scope(name)
	if err != nil {
		return nil, Note{}, safeError(err)
	}
	n, err := do(s)
	if err != nil {
		return nil, Note{}, safeError(err)
	}
	return nil, present(name, n), nil
}

func (t *tools) listVaults(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, VaultsOutput, error) {
	out := VaultsOutput{Vaults: []VaultInfo{}}
	for _, s := range t.scopes() {
		notes, err := s.Notes(ctx)
		if err != nil {
			return nil, out, safeError(err)
		}
		out.Vaults = append(out.Vaults, VaultInfo{Name: s.Name(), Notes: len(notes)})
	}
	return nil, out, nil
}

func (t *tools) listNotes(ctx context.Context, _ *sdk.CallToolRequest, in ListNotesInput) (*sdk.CallToolResult, ListNotesOutput, error) {
	out := ListNotesOutput{Notes: []NoteSummary{}}
	if _, _, _, err := in.bounds(0); err != nil {
		return nil, out, err
	}
	s, err := t.scope(in.Vault)
	if err != nil {
		return nil, out, safeError(err)
	}
	// A folder only picks among the notes that may be shown: of one that is
	// excluded, as of one that is not there, there is nothing to list.
	folder := strings.TrimSuffix(in.Folder, "/")
	if in.Folder != "" {
		if _, err := vault.CleanPath(folder); err != nil {
			return nil, out, safeError(err)
		}
	}
	notes, err := s.Notes(ctx)
	if err != nil {
		return nil, out, safeError(err)
	}
	matching := slices.DeleteFunc(notes, func(n vault.NoteMeta) bool {
		return (in.Folder != "" && !strings.HasPrefix(n.Path, folder+"/")) || (in.Tag != "" && !hasTag(n.Tags, in.Tag))
	})
	start, end, next, _ := in.bounds(len(matching))
	out.Total, out.NextOffset = len(matching), next
	for _, n := range matching[start:end] {
		out.Notes = append(out.Notes, NoteSummary{Path: n.Path, Title: n.Title, Modified: when(n.Modified), Size: n.Size, Tags: n.Tags})
	}
	return nil, out, nil
}

// hasTag reports whether tags has tag, in any case and with or without the #, or
// a tag nested in it: project/alpha is one of project.
func hasTag(tags []string, tag string) bool {
	want := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(tag), "#"))
	return slices.ContainsFunc(tags, func(t string) bool {
		t = strings.ToLower(t)
		return t == want || strings.HasPrefix(t, want+"/")
	})
}

func (t *tools) listTags(ctx context.Context, _ *sdk.CallToolRequest, in ListTagsInput) (*sdk.CallToolResult, ListTagsOutput, error) {
	out := ListTagsOutput{Tags: []TagCount{}}
	scopes := t.scopes()
	if in.Vault != "" {
		s, err := t.scope(in.Vault)
		if err != nil {
			return nil, out, safeError(err)
		}
		scopes = []scope{s}
	}
	for _, s := range scopes {
		counts := map[string]int{}
		notes, err := s.Notes(ctx)
		if err != nil {
			return nil, out, safeError(err)
		}
		for _, n := range notes {
			for _, tag := range n.Tags {
				counts[tag]++
			}
		}
		for tag, count := range counts {
			out.Tags = append(out.Tags, TagCount{Vault: s.Name(), Tag: tag, Notes: count})
		}
	}
	slices.SortFunc(out.Tags, func(a, b TagCount) int {
		return cmp.Or(strings.Compare(strings.ToLower(a.Vault), strings.ToLower(b.Vault)), strings.Compare(strings.ToLower(a.Tag), strings.ToLower(b.Tag)), strings.Compare(a.Tag, b.Tag))
	})
	return nil, out, nil
}

func (t *tools) getNote(_ context.Context, _ *sdk.CallToolRequest, in NoteInput) (*sdk.CallToolResult, Note, error) {
	return t.onNote(in.Vault, func(s scope) (vault.Note, error) { return s.Read(in.Path) })
}

func (t *tools) searchNotes(ctx context.Context, _ *sdk.CallToolRequest, in SearchInput) (*sdk.CallToolResult, SearchOutput, error) {
	out := SearchOutput{Hits: []Hit{}}
	if _, _, _, err := in.bounds(0); err != nil {
		return nil, out, err
	}
	scopes := t.scopes()
	if in.Vault != "" {
		s, err := t.scope(in.Vault)
		if err != nil {
			return nil, out, safeError(err)
		}
		scopes = []scope{s}
	}

	type found struct {
		vault string
		hit   vault.Hit
	}
	var all []found
	for _, s := range scopes {
		hits, err := s.Search(ctx, in.Query)
		if err != nil {
			return nil, out, safeError(err)
		}
		allowed := map[string]bool(nil)
		if in.Tag != "" {
			allowed = map[string]bool{}
			notes, err := s.Notes(ctx)
			if err != nil {
				return nil, out, safeError(err)
			}
			for _, n := range notes {
				if hasTag(n.Tags, in.Tag) {
					allowed[n.Path] = true
				}
			}
		}
		for _, h := range hits {
			if allowed != nil && !allowed[h.Path] {
				continue
			}
			all = append(all, found{s.Name(), h})
		}
	}
	// The hits of a vault come ranked, and the ranking does not depend on the vault
	// (see vault.Hit), so the hits of all of them are ranked the same way. The sort
	// is stable: among the notes that tie, the vaults stay in their order.
	slices.SortStableFunc(all, func(a, b found) int {
		return cmp.Or(cmp.Compare(b.hit.Score, a.hit.Score), b.hit.Modified.Compare(a.hit.Modified))
	})
	start, end, next, _ := in.bounds(len(all))
	out.Total, out.NextOffset = len(all), next
	for _, f := range all[start:end] {
		out.Hits = append(out.Hits, Hit{Vault: f.vault, Path: f.hit.Path, Title: f.hit.Title, Snippets: f.hit.Snippets})
	}
	return nil, out, nil
}

func (t *tools) createNote(_ context.Context, _ *sdk.CallToolRequest, in CreateInput) (*sdk.CallToolResult, Note, error) {
	return t.onNote(in.Vault, func(s scope) (vault.Note, error) { return s.Create(in.Path, in.Content) })
}

func (t *tools) updateNote(_ context.Context, _ *sdk.CallToolRequest, in UpdateInput) (*sdk.CallToolResult, Note, error) {
	return t.onNote(in.Vault, func(s scope) (vault.Note, error) { return s.Write(in.Path, in.Content, in.Revision) })
}

// editor is what an append needs of a vault, and no more: to read a note and to
// write it back over the revision that it read. With nothing else to ask the
// vault, an append cannot write over a revision that it did not read.
type editor interface {
	Read(p string) (vault.Note, error)
	Write(p, content, expectedRevision string) (vault.Note, error)
}

// appendTo adds text at the end of the note at p. The text that was there is kept
// to the byte, and a line break is added only to start the new text on a line of
// its own: when the note is not empty and does not end in one. The note is
// written back over the revision that was read, so a change that comes between
// is a conflict and is not overwritten.
func appendTo(v editor, p, text string) (vault.Note, error) {
	n, err := v.Read(p)
	if err != nil {
		return vault.Note{}, err
	}
	content := n.Content
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return v.Write(p, content+text, n.Revision)
}

func (t *tools) appendToNote(_ context.Context, _ *sdk.CallToolRequest, in AppendInput) (*sdk.CallToolResult, Note, error) {
	if in.Text == "" {
		return nil, Note{}, safeError(errNoText)
	}
	return t.onNote(in.Vault, func(s scope) (vault.Note, error) { return appendTo(s, in.Path, in.Text) })
}

// NewHandler serves the tools over the vaults of reg as Streamable HTTP, at the
// path that it is mounted at. hosts are the names besides localhost that the Host
// of a request may have.
func NewHandler(reg *vault.Registry, options Options, hosts []string) http.Handler {
	t := newTools(reg, options.Exclude)
	server := sdk.NewServer(&sdk.Implementation{Name: "notebook", Version: "0.1.0"}, nil)
	sdk.AddTool(server, &sdk.Tool{
		Name:        "list_vaults",
		Description: "List the vaults, with the number of notes in each.",
		Annotations: annotation(true),
	}, t.listVaults)
	sdk.AddTool(server, &sdk.Tool{
		Name: "list_tags",
		Description: "List the tags used by visible notes and how many notes use each tag. Searches every vault unless vault is given. " +
			"Tags come from frontmatter and inline #tags; a changed note may take a couple of seconds to show.",
		Annotations: annotation(true),
	}, t.listTags)
	sdk.AddTool(server, &sdk.Tool{
		Name: "list_notes",
		Description: "List the notes of a vault, newest first, with their tags. folder limits the list to a folder and the folders in it, " +
			"and tag to the notes with that tag (nested tags count: project matches project/alpha). " +
			"A note changed in the last couple of seconds may not show yet.",
		Annotations: annotation(true),
	}, t.listNotes)
	sdk.AddTool(server, &sdk.Tool{
		Name: "get_note",
		Description: "Read a note. content is the text of the file exactly as stored, frontmatter its YAML properties as an object, " +
			"and revision is what update_note needs.",
		Annotations: annotation(true),
	}, t.getNote)
	sdk.AddTool(server, &sdk.Tool{
		Name: "search_notes",
		Description: "Search the notes for words and \"quoted phrases\"; all of them must match, in the path or the text, ignoring case and accents. Optional tag narrows results to notes with that tag; nested tags count. " +
			"Best matches first, with the first lines that match. Searches every vault unless vault is given. " +
			"A note changed in the last couple of seconds may not show yet.",
		Annotations: annotation(true),
	}, t.searchNotes)
	if options.Write {
		sdk.AddTool(server, &sdk.Tool{
			Name:        "create_note",
			Description: "Create a note, and any missing folders above it. Never replaces a note: fails if there is one at the path. path ends in .md.",
			Annotations: annotation(false),
		}, t.createNote)
		// Of the tools that write, it is the one that replaces what was there.
		replaces := annotation(false)
		yes := true
		replaces.DestructiveHint = &yes
		sdk.AddTool(server, &sdk.Tool{
			Name: "update_note",
			Description: "Replace the whole text of a note. revision must be the one get_note returned: " +
				"if the note has changed since, nothing is written and the call fails, and the note must be read again.",
			Annotations: replaces,
		}, t.updateNote)
		sdk.AddTool(server, &sdk.Tool{
			Name: "append_to_note",
			Description: "Add text at the end of a note, leaving the rest of it as it is. If the note is not empty and does not end in a newline, " +
				"one newline (LF) is added first. Fails if the note changes while it is being written.",
			Annotations: annotation(false),
		}, t.appendToNote)
	}
	return transport(server, hosts)
}
