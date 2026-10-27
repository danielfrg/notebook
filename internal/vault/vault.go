// Package vault owns all file access for notebook. A Vault is one directory of
// Markdown notes, opened as an os.Root so that no operation can reach outside it.
package vault

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sync"
	"time"
)

// MaxNoteBytes is the size limit of a note.
const MaxNoteBytes = 2 << 20

// The errors of vault operations. Match them with errors.Is.
var (
	ErrNotFound    = errors.New("not found")
	ErrExists      = errors.New("already exists")
	ErrInvalidPath = errors.New("invalid path")
	ErrNotNote     = errors.New("not a UTF-8 markdown note")
	ErrTooLarge    = errors.New("note exceeds 2 MiB")
	ErrReadOnly    = errors.New("vault is read-only")
	ErrConflict    = errors.New("note changed on disk")
)

// ConflictError is the error of a write that lost against another writer.
// Current is the revision now on disk; it is never empty (a write to a note
// deleted since it was loaded is ErrNotFound). It matches ErrConflict with
// errors.Is.
type ConflictError struct{ Current string }

func (e *ConflictError) Error() string { return ErrConflict.Error() }

func (e *ConflictError) Is(target error) bool { return target == ErrConflict }

// Options configure a vault.
type Options struct{ ReadOnly bool }

// now is the clock for trash names and search snapshot staleness. Tests replace it.
var now = time.Now

var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

// Vault is one directory of notes. It is safe for concurrent use.
type Vault struct {
	name     string
	dir      string
	root     *os.Root
	readOnly bool
	mu       sync.Mutex  // serializes changes (writes, creations, moves, trashing), so checking and changing are one step
	search   searchCache // in-memory snapshot of the notes that Search and Notes work on, see cache.go
}

// Open opens the directory dir as a vault called name. The name must match
// ^[a-z0-9][a-z0-9_-]{0,31}$ and the directory must exist. dir is made
// absolute, so a later change of working directory does not affect the vault.
func Open(name, dir string, opts Options) (*Vault, error) {
	if !namePattern.MatchString(name) {
		return nil, fmt.Errorf("invalid vault name %q: must match %s", name, namePattern)
	}
	if dir == "" {
		return nil, fmt.Errorf("vault %q has no directory", name)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("vault %q: %w", name, err)
	}
	root, err := os.OpenRoot(abs)
	if err != nil {
		return nil, fmt.Errorf("vault %q: %w", name, err)
	}
	return &Vault{name: name, dir: abs, root: root, readOnly: opts.ReadOnly}, nil
}

// Close releases the vault directory. Every later operation fails.
func (v *Vault) Close() error { return v.root.Close() }

// Name returns the name the vault was opened with.
func (v *Vault) Name() string { return v.name }

// ReadOnly reports whether the vault rejects writes.
func (v *Vault) ReadOnly() bool { return v.readOnly }

// Ping reports whether the vault directory is still the one that was opened.
// An open os.Root keeps working on a directory that was deleted (it just looks
// empty) and on one that was replaced or unmounted (it still points at the old
// one), so Ping checks that the path leads to the same directory.
func (v *Vault) Ping() error {
	opened, err := v.root.Stat(".")
	if err != nil {
		return v.unavailable(err)
	}
	current, err := os.Stat(v.dir)
	if err != nil {
		return v.unavailable(err)
	}
	if !os.SameFile(opened, current) {
		return v.unavailable(errors.New("the directory was replaced"))
	}
	return nil
}

func (v *Vault) unavailable(cause error) error {
	return fmt.Errorf("vault %q unavailable: %w", v.name, cause)
}

// Registry is the set of vaults one instance serves, in configured order.
type Registry struct {
	vaults []*Vault
	byName map[string]*Vault
}

// NewRegistry groups vaults, which must be at least one with unique names.
// When it fails the caller still owns the vaults and must close them.
func NewRegistry(vaults ...*Vault) (*Registry, error) {
	if len(vaults) == 0 {
		return nil, errors.New("at least one vault is required")
	}
	r := &Registry{vaults: slices.Clone(vaults), byName: make(map[string]*Vault, len(vaults))}
	for _, v := range vaults {
		if v == nil {
			return nil, errors.New("nil vault")
		}
		if _, ok := r.byName[v.name]; ok {
			return nil, fmt.Errorf("duplicate vault name %q", v.name)
		}
		r.byName[v.name] = v
	}
	return r, nil
}

// Get returns the vault with the given name.
func (r *Registry) Get(name string) (*Vault, bool) {
	v, ok := r.byName[name]
	return v, ok
}

// All returns the vaults in configured order.
func (r *Registry) All() []*Vault { return slices.Clone(r.vaults) }

// Close closes every vault and reports all errors.
func (r *Registry) Close() error {
	errs := make([]error, len(r.vaults))
	for i, v := range r.vaults {
		errs[i] = v.Close()
	}
	return errors.Join(errs...)
}
