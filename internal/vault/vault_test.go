package vault

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// openTestVault opens a vault named "notes" over a fresh temp directory and
// closes it when the test ends.
func openTestVault(t *testing.T, opts Options) (*Vault, string) {
	t.Helper()
	dir := t.TempDir()
	v, err := Open("notes", dir, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { v.Close() })
	return v, dir
}

// tryWriteFile creates dir/name, and any missing parent folders, with content.
func tryWriteFile(dir, name, content string) error {
	full := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, []byte(content), 0o644)
}

// writeFile is tryWriteFile that fails the test on error.
func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := tryWriteFile(dir, name, content); err != nil {
		t.Fatal(err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

func TestOpenValidatesNameAndDir(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "file.txt", "a regular file, not a directory")

	for _, name := range []string{"Bad", "-x", "_x", "", "a b", "a.b", "a/b", "notes\n", strings.Repeat("a", 33)} {
		if v, err := Open(name, dir, Options{}); err == nil {
			v.Close()
			t.Errorf("Open(%q) succeeded, want an invalid name error", name)
		}
	}

	for _, tc := range []struct{ what, dir string }{
		{"a missing directory", filepath.Join(dir, "missing")},
		{"a regular file", filepath.Join(dir, "file.txt")},
		{"an empty path", ""},
	} {
		if v, err := Open("notes", tc.dir, Options{}); err == nil {
			v.Close()
			t.Errorf("Open with %s as the directory succeeded", tc.what)
		}
	}

	for _, name := range []string{"a", "0", "notes", "my-vault_2", strings.Repeat("a", 32)} {
		v, err := Open(name, dir, Options{})
		if err != nil {
			t.Errorf("Open(%q): %v", name, err)
			continue
		}
		if v.Name() != name {
			t.Errorf("Name() = %q, want %q", v.Name(), name)
		}
		if v.ReadOnly() {
			t.Errorf("ReadOnly() = true for %q opened with default options", name)
		}
		if err := v.Ping(); err != nil {
			t.Errorf("Ping() = %v", err)
		}
		if err := v.Close(); err != nil {
			t.Errorf("Close() = %v", err)
		}
	}
}

func TestOpenReadOnly(t *testing.T) {
	v, _ := openTestVault(t, Options{ReadOnly: true})
	if !v.ReadOnly() {
		t.Error("ReadOnly() = false for a vault opened with Options{ReadOnly: true}")
	}
}

func TestOpenMakesDirAbsolute(t *testing.T) {
	base := t.TempDir()
	writeFile(t, base, "vault/a.md", "a")
	elsewhere := t.TempDir()

	t.Chdir(base)
	v, err := Open("notes", "vault", Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { v.Close() })

	t.Chdir(elsewhere)
	if err := v.Ping(); err != nil {
		t.Errorf("Ping() after the working directory changed: %v", err)
	}
	if got := listVault(t, v); !slices.Equal(entryPaths(got.Notes), []string{"a.md"}) {
		t.Errorf("Notes after the working directory changed = %v, want [a.md]", entryPaths(got.Notes))
	}
}

// A vault directory that is itself a symlink (a mount point, a volume link) is
// fine; only symlinks inside the vault are never followed.
func TestOpenFollowsSymlinkedVaultDir(t *testing.T) {
	target := t.TempDir()
	writeFile(t, target, "a.md", "a")
	link := filepath.Join(t.TempDir(), "link")
	mustSymlink(t, target, link)

	v, err := Open("notes", link, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { v.Close() })

	if err := v.Ping(); err != nil {
		t.Errorf("Ping() = %v", err)
	}
	if got := listVault(t, v); !slices.Equal(entryPaths(got.Notes), []string{"a.md"}) {
		t.Errorf("Notes = %v, want [a.md]", entryPaths(got.Notes))
	}
}

// An open os.Root keeps working on a deleted directory (it just looks empty),
// so Ping and List must notice that the vault directory itself is gone.
func TestPingFailsWhenRootGone(t *testing.T) {
	v, dir := openTestVault(t, Options{})
	writeFile(t, dir, "a.md", "a")
	if err := v.Ping(); err != nil {
		t.Fatalf("Ping() on a healthy vault = %v", err)
	}
	if _, err := v.List(context.Background()); err != nil {
		t.Fatalf("List() on a healthy vault = %v", err)
	}

	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := v.Ping(); err == nil {
		t.Error("Ping() after the vault directory was removed = nil, want an error")
	}
	if _, err := v.List(context.Background()); err == nil {
		t.Error("List() after the vault directory was removed = nil error, want an error")
	}
}

// A directory recreated at the same path (an unmounted volume, a restored
// backup) is a different directory, and the open handle is stale.
func TestPingFailsWhenRootReplaced(t *testing.T) {
	v, dir := openTestVault(t, Options{})
	writeFile(t, dir, "a.md", "a")

	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := v.Ping(); err == nil {
		t.Error("Ping() after the vault directory was replaced = nil, want an error")
	}
	if _, err := v.List(context.Background()); err == nil {
		t.Error("List() after the vault directory was replaced = nil error, want an error")
	}
}

func TestClosedVaultIsUnavailable(t *testing.T) {
	v, _ := openTestVault(t, Options{})
	if err := v.Close(); err != nil {
		t.Fatal(err)
	}
	if err := v.Ping(); err == nil {
		t.Error("Ping() after Close() = nil, want an error")
	}
	if _, err := v.List(context.Background()); err == nil {
		t.Error("List() after Close() = nil error, want an error")
	}
}

func TestRegistry(t *testing.T) {
	open := func(name string) *Vault {
		t.Helper()
		v, err := Open(name, t.TempDir(), Options{})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { v.Close() })
		return v
	}
	beta, alpha, duplicate := open("beta"), open("alpha"), open("alpha")

	if _, err := NewRegistry(); err == nil {
		t.Error("NewRegistry() with no vaults succeeded")
	}
	if _, err := NewRegistry(beta, alpha, duplicate); err == nil {
		t.Error("NewRegistry() with two vaults named alpha succeeded")
	}
	if _, err := NewRegistry(beta, nil); err == nil {
		t.Error("NewRegistry() with a nil vault succeeded")
	}

	reg, err := NewRegistry(beta, alpha)
	if err != nil {
		t.Fatal(err)
	}

	if got := reg.All(); len(got) != 2 || got[0] != beta || got[1] != alpha {
		t.Errorf("All() = %v, want [beta alpha] in configured order", vaultNames(got))
	}
	reg.All()[0] = nil // the returned slice is a copy
	if got := reg.All(); got[0] != beta {
		t.Error("changing the slice returned by All() changed the registry")
	}

	if v, ok := reg.Get("alpha"); !ok || v != alpha {
		t.Errorf(`Get("alpha") = %v, %v, want the alpha vault`, v, ok)
	}
	if v, ok := reg.Get("beta"); !ok || v != beta {
		t.Errorf(`Get("beta") = %v, %v, want the beta vault`, v, ok)
	}
	for _, name := range []string{"gamma", "", "Alpha"} {
		if v, ok := reg.Get(name); ok || v != nil {
			t.Errorf("Get(%q) = %v, %v, want nil, false", name, v, ok)
		}
	}

	if err := reg.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}
	for _, v := range []*Vault{beta, alpha} {
		if err := v.Ping(); err == nil {
			t.Errorf("vault %q still works after Registry.Close()", v.Name())
		}
	}
}

func vaultNames(vaults []*Vault) []string {
	names := make([]string, len(vaults))
	for i, v := range vaults {
		if v != nil {
			names[i] = v.Name()
		}
	}
	return names
}

func TestSentinelErrors(t *testing.T) {
	for _, tc := range []struct {
		err error
		msg string
	}{
		{ErrNotFound, "not found"},
		{ErrExists, "already exists"},
		{ErrInvalidPath, "invalid path"},
		{ErrNotNote, "not a UTF-8 markdown note"},
		{ErrTooLarge, "note exceeds 2 MiB"},
		{ErrReadOnly, "vault is read-only"},
		{ErrConflict, "note changed on disk"},
	} {
		if tc.err.Error() != tc.msg {
			t.Errorf("error %q, want %q", tc.err.Error(), tc.msg)
		}
	}
	if MaxNoteBytes != 2097152 {
		t.Errorf("MaxNoteBytes = %d, want 2 MiB (2097152)", MaxNoteBytes)
	}
}

func TestConflictError(t *testing.T) {
	var err error = &ConflictError{Current: "abc123"}
	if err.Error() != ErrConflict.Error() {
		t.Errorf("Error() = %q, want %q", err.Error(), ErrConflict.Error())
	}
	if !errors.Is(err, ErrConflict) {
		t.Error("errors.Is(err, ErrConflict) = false")
	}
	if errors.Is(err, ErrNotFound) {
		t.Error("errors.Is(err, ErrNotFound) = true")
	}

	var conflict *ConflictError
	if !errors.As(fmt.Errorf("save note: %w", err), &conflict) || conflict.Current != "abc123" {
		t.Errorf("errors.As on a wrapped conflict = %v, want Current abc123", conflict)
	}

	// Matching ErrConflict does not depend on Current. Write never leaves it
	// empty (a note deleted since it was loaded is ErrNotFound), but the zero
	// value must still match.
	if !errors.Is(&ConflictError{}, ErrConflict) {
		t.Error("errors.Is(zero ConflictError, ErrConflict) = false")
	}
}
