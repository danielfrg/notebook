package vault

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// seedEntries fills the vault directory with what the tests of this file move,
// trash, open and refuse. The hidden folder, the file that is neither a note nor
// an image, the folder named like a note and the symlinks are there on purpose.
// It returns a folder outside the vault that one of the symlinks leads to.
func seedEntries(t *testing.T, dir string) (outside string) {
	t.Helper()
	outside = t.TempDir()
	writeFile(t, outside, "secret.md", "secret\n")
	writeFile(t, dir, "a.md", utf8BOM+"# A\r\nexact bytes\r\n")
	writeFile(t, dir, "b.md", "b\n")
	writeFile(t, dir, "pic.png", "png bytes")
	writeFile(t, dir, "c.txt", "neither a note nor an image")
	writeFile(t, dir, "weird.md/inner.md", "inner\n")
	writeFile(t, dir, "sub/n.md", "n\n")
	writeFile(t, dir, "sub/deep/m.md", "m\n")
	writeFile(t, dir, "sub/pic.png", "sub png bytes")
	writeFile(t, dir, "sub/.hidden", "hidden\n")
	writeFile(t, dir, ".obsidian/x.md", "x\n")
	mustMkdir(t, filepath.Join(dir, "empty"))
	mustSymlink(t, "a.md", filepath.Join(dir, "link.md"))
	mustSymlink(t, "sub", filepath.Join(dir, "linkdir"))
	mustSymlink(t, "missing.md", filepath.Join(dir, "dangling.md"))
	mustSymlink(t, outside, filepath.Join(dir, "escape"))
	return outside
}

// snapshot describes everything under dir, hidden files included. A folder is
// its path with a trailing slash and no text, a file is its path and its text,
// and a symlink is its path and "-> target".
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	snap := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil || rel == "." {
			return err
		}
		rel = filepath.ToSlash(rel)
		switch {
		case d.IsDir():
			snap[rel+"/"] = ""
		case d.Type()&fs.ModeSymlink != 0:
			target, err := os.Readlink(p)
			snap[rel] = "-> " + target
			return err
		default:
			text, err := os.ReadFile(p)
			snap[rel] = string(text)
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

// withFolders returns snap with the folder dir, and the folders above it, added
// where they are missing.
func withFolders(snap map[string]string, dir string) map[string]string {
	out := maps.Clone(snap)
	for ; dir != "."; dir = path.Dir(dir) {
		if _, ok := out[dir+"/"]; !ok {
			out[dir+"/"] = ""
		}
	}
	return out
}

// moved returns what snap must become when the entry at from, and everything
// beneath it, goes to to, with the folders above to that were missing.
func moved(snap map[string]string, from, to string) map[string]string {
	out := make(map[string]string, len(snap))
	for p, text := range snap {
		if p == from || strings.HasPrefix(p, from+"/") {
			p = to + p[len(from):]
		}
		out[p] = text
	}
	return withFolders(out, path.Dir(to))
}

// checkSnapshot reports every difference between two snapshots.
func checkSnapshot(t *testing.T, what string, got, want map[string]string) {
	t.Helper()
	for p, w := range want {
		if g, ok := got[p]; !ok {
			t.Errorf("%s: %s is missing", what, describe(p))
		} else if g != w {
			t.Errorf("%s: %s holds %q, want %q", what, describe(p), g, w)
		}
	}
	for p := range got {
		if _, ok := want[p]; !ok {
			t.Errorf("%s: %s is there, and should not be", what, describe(p))
		}
	}
}

// expectMoved runs op and checks that the vault is as it was, except that from,
// with everything beneath it, is now at to.
func expectMoved(t *testing.T, dir, from, to string, op func() error) {
	t.Helper()
	what := fmt.Sprintf("after moving %s to %s", describe(from), describe(to))
	before := snapshot(t, dir)
	if err := op(); err != nil {
		t.Errorf("%s: %v", what, err)
		return
	}
	checkSnapshot(t, what, snapshot(t, dir), moved(before, from, to))
}

// setClock makes now report at until the test ends, and returns the function
// that moves the clock.
func setClock(t *testing.T, at time.Time) (set func(time.Time)) {
	t.Helper()
	old := now
	t.Cleanup(func() { now = old })
	now = func() time.Time { return at }
	return func(next time.Time) { at = next }
}

func TestMkdir(t *testing.T) {
	setUmask(t, 0o022)
	v, dir := openTestVault(t, Options{})
	outside := seedEntries(t, dir)

	// A folder is made with the ones above it that are missing.
	want := snapshot(t, dir)
	for _, p := range []string{"new", "x/y/z", "empty/inner", "Café ¿Qué?/niño 日本"} {
		if err := v.Mkdir(p); err != nil {
			t.Errorf("Mkdir(%q): %v", p, err)
			continue
		}
		want = withFolders(want, p)
		for q := p; q != "."; q = path.Dir(q) {
			if got := modeOf(t, dir, q); got != 0o755 {
				t.Errorf("mode of folder %s = %v, want 0755", q, got)
			}
		}
	}
	checkSnapshot(t, "after the folders were made", snapshot(t, dir), want)
	if folders := listVault(t, v).Folders; !slices.Contains(folders, "x") || !slices.Contains(folders, "x/y/z") {
		t.Errorf("Folders = %q, want the new folders listed", folders)
	}

	// Every refusal leaves the vault as it is, with no folder made on the way.
	for _, tc := range []struct {
		what, path string
		want       error
	}{
		{"a folder that exists", "sub", ErrExists},
		{"a folder named like a note", "weird.md", ErrExists},
		{"a note", "a.md", ErrExists},
		{"folders beneath a note", "a.md/x/y", ErrExists},
		{"a hidden folder", ".x", ErrInvalidPath},
		{"the trash folder", ".trash", ErrInvalidPath},
		{"a folder beneath a hidden folder", ".obsidian/sub", ErrInvalidPath},
		{"a hidden folder in new folders", "fresh/.x", ErrInvalidPath},
		{"an empty path", "", ErrInvalidPath},
		{"parent traversal", "../x", ErrInvalidPath},
		{"a trailing slash", "fresh/", ErrInvalidPath},
		{"a symlink to a folder", "linkdir", ErrInvalidPath},
		{"a dangling symlink", "dangling.md", ErrInvalidPath},
		{"folders beneath a symlink to a folder", "linkdir/a/b", ErrInvalidPath},
		{"a folder beneath a symlink to a folder outside the vault", "escape/new", ErrInvalidPath},
	} {
		wantErr(t, "Mkdir of "+tc.what, v.Mkdir(tc.path), tc.want)
		checkSnapshot(t, "after Mkdir of "+tc.what, snapshot(t, dir), want)
	}
	if got := diskTree(t, outside); !slices.Equal(got, []string{"secret.md"}) {
		t.Errorf("the folder outside the vault = %q, want it untouched", got)
	}
}

func TestMoveNoteAndFolder(t *testing.T) {
	t.Run("moves", func(t *testing.T) {
		for _, tc := range []struct{ what, from, to string }{
			{"a note into new folders", "a.md", "other/deeper/new.md"},
			{"a note renamed in its folder", "sub/n.md", "sub/renamed.md"},
			{"a note into an existing folder", "b.md", "empty/b.md"},
			{"a note to a name with accents and punctuation", "b.md", "Café ¿Qué?/100% #1, [b].md"},
			{"a folder, with everything in it", "sub", "renamed"},
			{"a folder into an existing folder", "sub", "empty/sub"},
			{"a folder into new folders", "sub/deep", "x/y/deep"},
			{"an empty folder", "empty", "gone"},
			{"a folder named like a note", "weird.md", "plain"},
			{"an image", "pic.png", "img/pic.jpg"},
			{"only the case of a note's name", "b.md", "B.md"},
			{"only the case of a folder's name", "sub", "Sub"},
		} {
			t.Run(tc.what, func(t *testing.T) {
				v, dir := openTestVault(t, Options{})
				seedEntries(t, dir)
				expectMoved(t, dir, tc.from, tc.to, func() error { return v.Move(tc.from, tc.to) })
			})
		}
	})

	t.Run("a note keeps its text, modification time and mode", func(t *testing.T) {
		v, dir := openTestVault(t, Options{})
		seedEntries(t, dir)
		text := utf8BOM + "# A\r\nexact bytes\r\n"
		old := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
		chtimes(t, dir, "a.md", old)
		if err := os.Chmod(filepath.Join(dir, "a.md"), 0o640); err != nil {
			t.Fatal(err)
		}

		if err := v.Move("a.md", "other/a2.md"); err != nil {
			t.Fatal(err)
		}
		info := fileInfo(t, dir, "other/a2.md")
		if !info.ModTime().Equal(old) || info.Mode().Perm() != 0o640 {
			t.Errorf("the moved note has modification time %v and mode %v, want %v and 0640", info.ModTime(), info.Mode().Perm(), old)
		}
		if n := mustRead(t, v, "other/a2.md"); n.Content != text || n.Revision != sha256Hex(text) {
			t.Errorf("Read of the moved note = %+v, want the same text and revision", n)
		}
	})

	t.Run("refusals", func(t *testing.T) {
		v, dir := openTestVault(t, Options{})
		outside := seedEntries(t, dir)
		before := snapshot(t, dir)

		// Every refusal leaves the vault as it is, with no folder made on the way.
		for _, tc := range []struct {
			what, from, to string
			want           error
		}{
			{"a note onto a note", "a.md", "b.md", ErrExists},
			{"a note onto itself", "a.md", "a.md", ErrExists},
			{"a note onto a folder named like a note", "a.md", "weird.md", ErrExists},
			{"a folder onto an empty folder", "sub", "empty", ErrExists},
			{"a folder onto itself", "sub", "sub", ErrExists},
			{"a folder onto a note", "sub", "a.md", ErrExists},
			{"an image onto an image", "pic.png", "sub/pic.png", ErrExists},
			{"a note beneath a note", "a.md", "b.md/x.md", ErrExists},
			{"a note beneath a note, in new folders", "a.md", "b.md/x/y.md", ErrExists},

			{"a note to a name that is not a note", "a.md", "x.txt", ErrInvalidPath},
			{"a note to an upper case extension", "a.md", "a.MD", ErrInvalidPath},
			{"a note to a refused name in new folders", "a.md", "fresh/folders/x.txt", ErrInvalidPath},
			{"an image to a name that is not an image", "pic.png", "pic.txt", ErrInvalidPath},
			{"an image to a note's name", "pic.png", "pic.md", ErrInvalidPath},
			{"a folder into its own subfolder", "sub", "sub/inner", ErrInvalidPath},
			{"a folder into a subfolder that is new", "sub", "sub/deep/x/y", ErrInvalidPath},
			{"a file that is neither a note nor an image", "c.txt", "d.txt", ErrInvalidPath},
			{"a file that is neither a note nor an image, to a note", "c.txt", "c.md", ErrInvalidPath},
			{"a hidden folder's note", ".obsidian/x.md", "x.md", ErrInvalidPath},
			{"a note to a hidden folder", "a.md", ".trash/a.md", ErrInvalidPath},
			{"a note to a hidden folder in new folders", "a.md", "fresh/.x/a.md", ErrInvalidPath},
			{"a symlink", "link.md", "x.md", ErrInvalidPath},
			{"a symlink to a folder", "linkdir", "x", ErrInvalidPath},
			{"a dangling symlink", "dangling.md", "x.md", ErrInvalidPath},
			{"a note in a symlinked folder", "linkdir/n.md", "x.md", ErrInvalidPath},
			{"a note onto a symlink", "a.md", "link.md", ErrInvalidPath},
			{"a note onto a dangling symlink", "a.md", "dangling.md", ErrInvalidPath},
			{"a note into a symlinked folder", "a.md", "linkdir/new.md", ErrInvalidPath},
			{"a note into new folders in a symlinked folder", "a.md", "linkdir/new/x.md", ErrInvalidPath},
			{"a note into a symlinked folder outside the vault", "a.md", "escape/new.md", ErrInvalidPath},
			{"an empty source", "", "x.md", ErrInvalidPath},
			{"an empty destination", "a.md", "", ErrInvalidPath},
			{"parent traversal", "a.md", "../a.md", ErrInvalidPath},
			{"a trailing slash", "sub", "sub2/", ErrInvalidPath},

			{"a source that is not there", "missing.md", "x.md", ErrNotFound},
			{"a source in a folder that is not there", "missing/n.md", "x.md", ErrNotFound},
			{"a source beneath a note", "a.md/inner.md", "x.md", ErrNotFound},
			{"a source that is not there, to new folders", "missing.md", "fresh/x.md", ErrNotFound},
		} {
			wantErr(t, "Move of "+tc.what, v.Move(tc.from, tc.to), tc.want)
			checkSnapshot(t, "after Move of "+tc.what, snapshot(t, dir), before)
		}
		if got := diskTree(t, outside); !slices.Equal(got, []string{"secret.md"}) {
			t.Errorf("the folder outside the vault = %q, want it untouched", got)
		}
	})

	// rename(2) does nothing, and succeeds, when both names are one file, so a
	// move onto the other name of a hard link would be reported and not happen.
	t.Run("two names of one file", func(t *testing.T) {
		v, dir := openTestVault(t, Options{})
		seedEntries(t, dir)
		if err := os.Link(filepath.Join(dir, "b.md"), filepath.Join(dir, "hard.md")); err != nil {
			t.Skipf("cannot make a hard link here: %v", err)
		}
		before := snapshot(t, dir)

		wantErr(t, "Move onto another name of the same file", v.Move("b.md", "hard.md"), ErrExists)
		wantErr(t, "Move onto the first name of the same file", v.Move("hard.md", "b.md"), ErrExists)
		checkSnapshot(t, "after the refused moves", snapshot(t, dir), before)
	})
}

func TestTrash(t *testing.T) {
	const stamp = "20261026-210000"
	// Each subtest that names trashed entries starts its clock at this time.
	start := time.Date(2026, 10, 26, 21, 0, 0, 0, time.UTC)

	t.Run("goes to .trash under the time and name", func(t *testing.T) {
		set := setClock(t, start)
		v, dir := openTestVault(t, Options{})
		seedEntries(t, dir)
		trash := func(p, lands string) {
			t.Helper()
			expectMoved(t, dir, p, lands, func() error { return v.Trash(p) })
		}

		trash("a.md", ".trash/"+stamp+"-a.md")
		// The same name in the same second gets a number, and so does the same name
		// from another folder, which the trash flattens.
		writeFile(t, dir, "a.md", "another a\n")
		trash("a.md", ".trash/"+stamp+"-a-2.md")
		writeFile(t, dir, "sub/a.md", "a in a folder\n")
		trash("sub/a.md", ".trash/"+stamp+"-a-3.md")
		// A folder goes whole, hidden files included.
		trash("sub", ".trash/"+stamp+"-sub")
		mustMkdir(t, filepath.Join(dir, "sub"))
		trash("sub", ".trash/"+stamp+"-sub-2")
		trash("pic.png", ".trash/"+stamp+"-pic.png")
		// A folder has no extension, whatever its name looks like: the number goes last.
		trash("weird.md", ".trash/"+stamp+"-weird.md")
		mustMkdir(t, filepath.Join(dir, "weird.md"))
		trash("weird.md", ".trash/"+stamp+"-weird.md-2")

		// Trashed entries are gone from the listing, and so is .trash itself.
		l := listVault(t, v)
		if !slices.Equal(entryPaths(l.Notes), []string{"b.md"}) || len(l.Assets) != 0 || !slices.Equal(l.Folders, []string{"empty"}) {
			t.Errorf("List = notes %q, assets %q, folders %q; want only b.md and the folder empty",
				entryPaths(l.Notes), entryPaths(l.Assets), l.Folders)
		}

		// The name has the UTC time, whatever zone the clock is in, and a new
		// second needs no number.
		set(time.Date(2026, 10, 26, 23, 0, 1, 0, time.FixedZone("UTC+2", 2*60*60)))
		trash("b.md", ".trash/20261026-210001-b.md")
	})

	t.Run("a name in the trash that is taken", func(t *testing.T) {
		setClock(t, start)
		v, dir := openTestVault(t, Options{})
		seedEntries(t, dir)
		mustMkdir(t, filepath.Join(dir, ".trash", stamp+"-a-2.md")) // a folder
		mustSymlink(t, "nowhere", filepath.Join(dir, ".trash", stamp+"-a.md"))

		expectMoved(t, dir, "a.md", ".trash/"+stamp+"-a-3.md", func() error { return v.Trash("a.md") })
	})

	// A name takes up to 255 bytes, and the time and a number come with it.
	t.Run("names at the length limit", func(t *testing.T) {
		for _, tc := range []struct{ what, name, lands, second string }{
			{"ASCII", strings.Repeat("a", 252) + ".md",
				stamp + "-" + strings.Repeat("a", 236) + ".md",
				stamp + "-" + strings.Repeat("a", 234) + "-2.md"},
			{"multi-byte", strings.Repeat("日", 84) + ".md", // cut between characters, not through one
				stamp + "-" + strings.Repeat("日", 78) + ".md",
				stamp + "-" + strings.Repeat("日", 78) + "-2.md"},
		} {
			t.Run(tc.what, func(t *testing.T) {
				setClock(t, start)
				v, dir := openTestVault(t, Options{})
				createOrSkip(t, dir, tc.name)
				expectMoved(t, dir, tc.name, ".trash/"+tc.lands, func() error { return v.Trash(tc.name) })
				createOrSkip(t, dir, tc.name)
				expectMoved(t, dir, tc.name, ".trash/"+tc.second, func() error { return v.Trash(tc.name) })
			})
		}
	})

	t.Run("refusals", func(t *testing.T) {
		v, dir := openTestVault(t, Options{})
		seedEntries(t, dir)
		before := snapshot(t, dir)

		// Every refusal leaves the vault as it is, and does not make a .trash.
		for _, tc := range []struct {
			what, path string
			want       error
		}{
			{"an empty path", "", ErrInvalidPath},
			{"the trash folder", ".trash", ErrInvalidPath},
			{"something in the trash", ".trash/x", ErrInvalidPath},
			{"a note in a hidden folder", ".obsidian/x.md", ErrInvalidPath},
			{"parent traversal", "../a.md", ErrInvalidPath},
			{"a file that is neither a note nor an image", "c.txt", ErrInvalidPath},
			{"a symlink", "link.md", ErrInvalidPath},
			{"a dangling symlink", "dangling.md", ErrInvalidPath},
			{"a note in a symlinked folder", "linkdir/n.md", ErrInvalidPath},
			{"a path with nothing at it", "missing.md", ErrNotFound},
			{"a path beneath a note", "a.md/inner.md", ErrNotFound},
		} {
			wantErr(t, "Trash of "+tc.what, v.Trash(tc.path), tc.want)
			checkSnapshot(t, "after Trash of "+tc.what, snapshot(t, dir), before)
		}
	})

	// A .trash that is not a folder is the vault's fault, not the caller's, and
	// nothing may be moved into whatever it is, or through it.
	for _, tc := range []struct {
		what string
		make func(t *testing.T, dir string)
	}{
		{"a symlink", func(t *testing.T, dir string) { mustSymlink(t, "sub", filepath.Join(dir, ".trash")) }},
		{"a file", func(t *testing.T, dir string) { writeFile(t, dir, ".trash", "not a folder") }},
	} {
		t.Run(".trash is "+tc.what, func(t *testing.T) {
			v, dir := openTestVault(t, Options{})
			seedEntries(t, dir)
			tc.make(t, dir)
			before := snapshot(t, dir)

			err := v.Trash("a.md")
			if err == nil {
				t.Error("Trash succeeded although .trash is not a folder")
			}
			for _, caller := range []error{ErrInvalidPath, ErrNotFound, ErrExists} {
				if errors.Is(err, caller) {
					t.Errorf("Trash = %v, want an error that does not blame the path it was given", err)
				}
			}
			checkSnapshot(t, "after the refused Trash", snapshot(t, dir), before)
		})
	}
}

func TestOpenAsset(t *testing.T) {
	v, dir := openTestVault(t, Options{})
	outside := seedEntries(t, dir)
	assets := []string{"pic.png", "sub/pic.png", "UP.PNG", "photo.jpg", "a.jpeg", "b.gif", "img/c.webp", "d.avif"}
	for _, name := range assets {
		writeFile(t, dir, name, "bytes of "+name)
	}
	writeFile(t, dir, "x.svg", "<svg/>")
	writeFile(t, dir, ".obsidian/pic.png", "hidden")
	writeFile(t, dir, "sub/.pic.png", "hidden")
	writeFile(t, outside, "outside.png", "outside the vault")
	mustMkdir(t, filepath.Join(dir, "shots.png"))
	mustSymlink(t, "pic.png", filepath.Join(dir, "link.png"))
	mustSymlink(t, filepath.Join(outside, "outside.png"), filepath.Join(dir, "outside-link.png"))
	pipe := filepath.Join(dir, "pipe.png")
	pipeErr := syscall.Mkfifo(pipe, 0o644)
	// Should OpenAsset open the pipe anyway, opening the other end frees it, so
	// a failing test does not leave the suite hanging.
	t.Cleanup(func() {
		if f, err := os.OpenFile(pipe, os.O_RDWR, 0); err == nil {
			f.Close()
		}
	})

	open := func(p string) (*os.File, fs.FileInfo, error) {
		t.Helper()
		type result struct {
			f    *os.File
			info fs.FileInfo
			err  error
		}
		done := make(chan result, 1)
		go func() {
			f, info, err := v.OpenAsset(p)
			done <- result{f, info, err}
		}()
		select {
		case r := <-done:
			return r.f, r.info, r.err
		case <-time.After(10 * time.Second):
			t.Fatalf("OpenAsset(%s) blocked", describe(p))
			return nil, nil, nil
		}
	}

	for _, name := range assets {
		f, info, err := open(name)
		if err != nil {
			t.Errorf("OpenAsset(%q): %v", name, err)
			continue
		}
		got, rerr := io.ReadAll(f)
		f.Close()
		if rerr != nil || string(got) != "bytes of "+name {
			t.Errorf("OpenAsset(%q) read %q, %v; want its bytes", name, got, rerr)
		}
		if info.Name() != path.Base(name) || info.Size() != int64(len("bytes of "+name)) || !info.Mode().IsRegular() {
			t.Errorf("OpenAsset(%q) info = %v, %d bytes, mode %v; want the file's", name, info.Name(), info.Size(), info.Mode())
		}
	}

	type refusal struct {
		what, path string
		want       error
	}
	refusals := []refusal{
		{"a note", "a.md", ErrInvalidPath},
		{"an svg", "x.svg", ErrInvalidPath},
		{"a hidden folder", ".obsidian/pic.png", ErrInvalidPath},
		{"a hidden name", "sub/.pic.png", ErrInvalidPath},
		{"a symlink", "link.png", ErrInvalidPath},
		{"a symlink to an image outside the vault", "outside-link.png", ErrInvalidPath},
		{"an image in a symlinked folder", "linkdir/pic.png", ErrInvalidPath},
		{"an image in a symlinked folder outside the vault", "escape/outside.png", ErrInvalidPath},
		{"a folder named like an image", "shots.png", ErrInvalidPath},
		{"an empty path", "", ErrInvalidPath},
		{"parent traversal", "../pic.png", ErrInvalidPath},
		{"an image that is not there", "missing.png", ErrNotFound},
		{"an image in a folder that is not there", "missing/pic.png", ErrNotFound},
		{"an image beneath an image", "pic.png/inner.png", ErrNotFound},
	}
	if pipeErr != nil {
		t.Logf("no named pipe here (%v): the pipe is not tried", pipeErr)
	} else {
		refusals = append(refusals, refusal{"a named pipe", "pipe.png", ErrInvalidPath})
	}
	for _, tc := range refusals {
		f, info, err := open(tc.path)
		wantErr(t, "OpenAsset of "+tc.what, err, tc.want)
		if f != nil || info != nil {
			t.Errorf("OpenAsset of %s returned %v and %v with its error, want nil", tc.what, f, info)
			if f != nil {
				f.Close()
			}
		}
	}
}

func TestReadOnlyVaultRejectsEntryMutations(t *testing.T) {
	v, dir := openTestVault(t, Options{ReadOnly: true})
	seedEntries(t, dir)
	before := snapshot(t, dir)

	// The policy is checked before the path is, so these fail the same way
	// whatever they name.
	for _, tc := range []struct {
		what string
		err  error
	}{
		{"Mkdir", v.Mkdir("new")},
		{"Mkdir of an invalid path", v.Mkdir("../x")},
		{"Move", v.Move("a.md", "x.md")},
		{"Move of a note that is not there", v.Move("missing.md", "x.md")},
		{"Trash", v.Trash("a.md")},
		{"Trash of a note that is not there", v.Trash("missing.md")},
		{"Trash of an invalid path", v.Trash("")},
	} {
		wantErr(t, tc.what, tc.err, ErrReadOnly)
	}
	checkSnapshot(t, "after the refused changes", snapshot(t, dir), before)

	// Reading still works.
	if n := mustRead(t, v, "b.md"); n.Content != "b\n" {
		t.Errorf("Read = %+v, want the note", n)
	}
	f, _, err := v.OpenAsset("pic.png")
	if err != nil {
		t.Fatalf("OpenAsset: %v", err)
	}
	f.Close()
}

// Each of these checks that a path is free and then renames into it. Were two
// of them to run at once, both could find the path free, and the second rename
// would replace what the first had put there.
func TestEntryChangesSerialize(t *testing.T) {
	const workers = 16
	// parallel runs f(0) to f(workers-1) at the same moment and returns what they return.
	parallel := func(f func(i int) error) []error {
		errs := make([]error, workers)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := range workers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				errs[i] = f(i)
			}()
		}
		close(start)
		wg.Wait()
		return errs
	}

	t.Run("moves to one destination", func(t *testing.T) {
		v, dir := openTestVault(t, Options{})
		for i := range workers {
			writeFile(t, dir, fmt.Sprintf("n%d.md", i), fmt.Sprintf("note %d\n", i))
		}
		errs := parallel(func(i int) error { return v.Move(fmt.Sprintf("n%d.md", i), "dst.md") })

		winner := -1
		for i, err := range errs {
			switch {
			case err == nil && winner < 0:
				winner = i
			case err == nil:
				t.Errorf("moves %d and %d both succeeded", winner, i)
			case !errors.Is(err, ErrExists):
				t.Errorf("move %d: %v, want ErrExists", i, err)
			}
		}
		// Every note is still there: the winner's at dst.md, the others where they were.
		for i := range workers {
			name := fmt.Sprintf("n%d.md", i)
			if i == winner {
				name = "dst.md"
			}
			if got := readDisk(t, dir, name); got != fmt.Sprintf("note %d\n", i) {
				t.Errorf("note %d is %q at %s", i, got, name)
			}
		}
		if got := diskTree(t, dir); len(got) != workers {
			t.Errorf("directory = %q, want %d notes", got, workers)
		}
	})

	t.Run("trashes of one name", func(t *testing.T) {
		setClock(t, time.Date(2026, 10, 26, 21, 0, 0, 0, time.UTC)) // before the workers start: they only read it
		v, dir := openTestVault(t, Options{})
		for i := range workers {
			writeFile(t, dir, fmt.Sprintf("d%d/a.md", i), fmt.Sprintf("note %d\n", i))
		}
		for i, err := range parallel(func(i int) error { return v.Trash(fmt.Sprintf("d%d/a.md", i)) }) {
			if err != nil {
				t.Errorf("trash %d: %v", i, err)
			}
		}

		// Each note is in the trash once, under a name of its own.
		var texts, want []string
		for _, p := range diskTree(t, dir) {
			if strings.HasPrefix(p, ".trash/") && !strings.HasSuffix(p, "/") { // the files, not the folder
				texts = append(texts, readDisk(t, dir, p))
			}
		}
		for i := range workers {
			want = append(want, fmt.Sprintf("note %d\n", i))
		}
		slices.Sort(texts)
		slices.Sort(want)
		if !slices.Equal(texts, want) {
			t.Errorf("the trash holds %q, want each of the %d notes once", texts, workers)
		}
	})
}
