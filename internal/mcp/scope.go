package mcp

import (
	"context"
	"log"
	"maps"
	"math"
	"slices"

	"github.com/danielfrg/notebook/internal/vault"
)

// tools holds what the handlers of the tools share.
type tools struct {
	reg     *vault.Registry
	visible map[string]vault.Filter // by vault name: what the tools may show of it
}

func newTools(reg *vault.Registry, exclude map[string][]string) *tools {
	t := &tools{reg: reg, visible: map[string]vault.Filter{}}
	for _, v := range reg.All() {
		visible, err := vault.ExcludeGlobs(exclude[v.Name()])
		if err != nil {
			// The settings refuse such a pattern with the same function before
			// anything is served, so the binary does not get here. If it is reached
			// anyway, an exclude that cannot be read must hide more and not less.
			log.Printf("mcp: vault %q: %v; none of its notes is shown", v.Name(), err)
			visible = func(string) bool { return false }
		}
		t.visible[v.Name()] = visible
	}
	// An exclude for a vault that is not served hides nothing, which is what a
	// mistyped name gets. The settings refuse an unknown vault, so the binary does
	// not get here; if it is reached anyway it is not let pass in silence, because
	// an exclude that is not applied shows what it was meant to hide.
	for _, name := range slices.Sorted(maps.Keys(exclude)) {
		if _, ok := reg.Get(name); !ok {
			log.Printf("mcp: exclude for the vault %q, which is not served: it hides nothing", name)
		}
	}
	return t
}

// scope is a vault as the tools may see it: without what Options.Exclude leaves
// out. It is the only way that the tools reach a vault. Its methods are the ones
// of the vault that they use, and each looks at the exclude of a path before the
// vault is asked about it, and hands the vault the exclude to leave out of what
// it lists and finds. So a tool cannot forget it.
type scope struct {
	v       *vault.Vault
	visible vault.Filter
}

func (t *tools) scope(name string) (scope, error) {
	v, ok := t.reg.Get(name)
	if !ok {
		return scope{}, errNoVault
	}
	return scope{v, t.visible[name]}, nil
}

// scopes returns every vault, in the order that they are configured.
func (t *tools) scopes() []scope {
	var all []scope
	for _, v := range t.reg.All() {
		all = append(all, scope{v, t.visible[v.Name()]})
	}
	return all
}

func (s scope) Name() string { return s.v.Name() }

// Notes lists the notes that may be shown, newest first.
func (s scope) Notes(ctx context.Context) ([]vault.NoteMeta, error) {
	return s.v.Notes(ctx, s.visible)
}

// Search finds the notes that may be shown. It returns all the hits and not a
// page of them: the caller cuts the pages, to say how many there are in all.
func (s scope) Search(ctx context.Context, query string) ([]vault.Hit, error) {
	return s.v.Search(ctx, query, vault.SearchOptions{Limit: math.MaxInt, Visible: s.visible})
}

// Read reads a note, unless it is excluded: then it is not found, as a note that
// is not there is. What is wrong with the path itself is told first, by the check
// that the vault makes first, for it is the same for every path with that fault
// and says nothing of what is excluded.
func (s scope) Read(p string) (vault.Note, error) {
	if err := vault.CheckNotePath(p); err != nil {
		return vault.Note{}, err
	}
	if !s.visible(p) {
		return vault.Note{}, vault.ErrNotFound
	}
	return s.v.Read(p)
}

// Write saves a note over the revision that was read, unless it is excluded: then
// it is not found.
func (s scope) Write(p, content, expectedRevision string) (vault.Note, error) {
	if err := s.checkWrite(p, content); err != nil {
		return vault.Note{}, err
	}
	return s.v.Write(p, content, expectedRevision)
}

// Create makes a note, unless the path is excluded: then it is not found, and
// nothing is made there, or said about what is.
func (s scope) Create(p, content string) (vault.Note, error) {
	if err := s.checkWrite(p, content); err != nil {
		return vault.Note{}, err
	}
	return s.v.Create(p, content)
}

// checkWrite is what the vault checks before it looks at the disk, by its own
// CheckWrite and so in its own order, and then that p is not excluded. What is
// wrong with the request that has nothing to do with the exclude is told first
// because it is the same for every path with that fault: it says nothing of what
// is excluded. Past that, an excluded path is not found, which is what the vault
// says of a note that is not there: whatever is asked of it, there is no way to
// tell it from one, and no check that the vault comes to make there can change
// that, since it is made here too.
func (s scope) checkWrite(p, content string) error {
	if err := s.v.CheckWrite(p, content); err != nil {
		return err
	}
	if !s.visible(p) {
		return vault.ErrNotFound
	}
	return nil
}
