package vault

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// utf8BOM is the byte order mark Obsidian and Windows editors put at the start of
// a file. Written as bytes so that no tool can mistake it for a stray character.
const utf8BOM = "\xef\xbb\xbf"

// sha256Hex is the revision a note with this content must have. It does not
// use Revision, so the tests do not check the implementation against itself.
func sha256Hex(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// diskTree lists everything under dir, hidden files included, as sorted
// slash-separated paths with "/" appended to folders. List hides the temp files
// a failed write could leave behind, so a test that must prove there are none
// looks at the directory itself.
func diskTree(t *testing.T, dir string) []string {
	t.Helper()
	var tree []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			rel += "/"
		}
		tree = append(tree, rel)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(tree)
	return tree
}

// readDisk returns the text of dir/name straight from the filesystem.
func readDisk(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// modeOf returns the permission bits of dir/name.
func modeOf(t *testing.T, dir, name string) fs.FileMode {
	t.Helper()
	info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

// wantErr checks that err is, or wraps, want. A nil want checks for success.
func wantErr(t *testing.T, what string, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Errorf("%s: error = %v, want %v", what, err, want)
	}
}

func equalNotes(a, b Note) bool {
	return a.Path == b.Path && a.Content == b.Content && a.Revision == b.Revision &&
		a.Size == b.Size && a.Modified.Equal(b.Modified)
}

func mustRead(t *testing.T, v *Vault, path string) Note {
	t.Helper()
	n, err := v.Read(path)
	if err != nil {
		t.Fatalf("Read(%s): %v", describe(path), err)
	}
	return n
}

// setUmask sets the process umask until the test ends. The umask is process
// wide, which is fine because no test in this package runs in parallel.
func setUmask(t *testing.T, mask int) {
	t.Helper()
	old := syscall.Umask(mask)
	t.Cleanup(func() { syscall.Umask(old) })
}

// limitFileSize makes any write that grows a file past n bytes fail with EFBIG
// (file too large), part-way through the data, the way a full disk fails. It
// returns the function that lifts the limit again; the test ends with the limit
// lifted either way. The limit is process wide, so keep the window short.
func limitFileSize(t *testing.T, n uint64) (lift func()) {
	t.Helper()
	var old syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &old); err != nil {
		t.Skipf("cannot read the file size limit here: %v", err)
	}
	signal.Ignore(syscall.SIGXFSZ) // the kernel also signals the process, and the default action kills it
	limited := old
	limited.Cur = n
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &limited); err != nil {
		signal.Reset(syscall.SIGXFSZ)
		t.Skipf("cannot lower the file size limit here: %v", err)
	}
	var once sync.Once
	lift = func() {
		once.Do(func() {
			syscall.Setrlimit(syscall.RLIMIT_FSIZE, &old)
			signal.Reset(syscall.SIGXFSZ)
		})
	}
	t.Cleanup(lift)
	return lift
}

func TestRevision(t *testing.T) {
	for _, tc := range []struct{ content, want string }{
		{"", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
		{"hello", "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"},
		{"hello\n", "5891b5b522d5df086d0ff0b110fbd9d21bb4fc7163af34d08286a2e846f6be03"},
	} {
		if got := Revision([]byte(tc.content)); got != tc.want {
			t.Errorf("Revision(%q) = %s, want %s", tc.content, got, tc.want)
		}
	}
	if Revision(nil) != Revision([]byte{}) {
		t.Error("Revision(nil) differs from Revision of empty content")
	}
	// It is the hash of the bytes, so line endings and a BOM are part of it.
	unix, windows, bom := Revision([]byte("a\nb\n")), Revision([]byte("a\r\nb\r\n")), Revision([]byte(utf8BOM+"a\nb\n"))
	if unix == windows || unix == bom || windows == bom {
		t.Error("revisions of content that differs only by line endings or a BOM are equal")
	}
}

func TestReadReturnsExactBytesAndRevision(t *testing.T) {
	v, dir := openTestVault(t, Options{})
	files := []struct{ name, content string }{
		{"crlf.md", "# Title\r\n\r\nfirst line\r\nsecond line\r\n"},
		{"bom.md", utf8BOM + "# With a BOM\nbody\n"},
		{"bom-crlf.md", utf8BOM + "# Both\r\nbody\r\n"},
		{"trailing-spaces.md", "two spaces for a break  \nand a tab\t\n   \n"},
		{"no-final-newline.md", "no newline at the end"},
		{"mixed-endings.md", "unix\nwindows\r\nbare\rcr\n"},
		{"empty.md", ""},
		{"nul.md", "before\x00after\n"},
		{"unicode.md", "¿Qué es? 日本語 👋 e\xcc\x81 (e and a combining accent, not normalized)\n"},
		{"deep/er/nested.md", "nested\n"},
	}
	base := time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC)
	for i, f := range files {
		writeFile(t, dir, f.name, f.content)
		chtimes(t, dir, f.name, base.Add(time.Duration(i)*time.Hour))
	}

	for i, f := range files {
		got, err := v.Read(f.name)
		if err != nil {
			t.Errorf("Read(%q): %v", f.name, err)
			continue
		}
		want := Note{
			Path:     f.name,
			Content:  f.content,
			Revision: sha256Hex(f.content),
			Size:     int64(len(f.content)),
			Modified: base.Add(time.Duration(i) * time.Hour),
		}
		if !equalNotes(got, want) {
			t.Errorf("Read(%q) = %+v, want %+v", f.name, got, want)
		}
		if got.Revision != Revision([]byte(f.content)) {
			t.Errorf("Read(%q).Revision = %s, want Revision of the file bytes", f.name, got.Revision)
		}
	}
}

func TestReadRejectsNonUTF8AndLarge(t *testing.T) {
	v, dir := openTestVault(t, Options{})
	writeFile(t, dir, "ok.md", "ok")
	writeFile(t, dir, "sub/n.md", "n")
	writeFile(t, dir, "limit.md", strings.Repeat("a", MaxNoteBytes))
	writeFile(t, dir, "big.md", strings.Repeat("a", MaxNoteBytes+1))
	writeFile(t, dir, "invalid.md", "\xff\xfe")
	writeFile(t, dir, "cut-short.md", "caf\xc3")      // a UTF-8 sequence that stops half way
	writeFile(t, dir, "surrogate.md", "\xed\xa0\x80") // an encoded surrogate half, which UTF-8 forbids
	writeFile(t, dir, "a.txt", "text")
	writeFile(t, dir, "upper.MD", "x")
	writeFile(t, dir, ".hidden/x.md", "x")
	writeFile(t, dir, "sub/.hidden.md", "x")
	mustMkdir(t, filepath.Join(dir, "folder"))
	mustMkdir(t, filepath.Join(dir, "weird.md"))
	mustSymlink(t, "ok.md", filepath.Join(dir, "link.md"))

	for _, tc := range []struct {
		path string
		want error
	}{
		{"ok.md", nil},
		{"sub/n.md", nil},
		{"limit.md", nil}, // exactly 2 MiB is allowed
		{"big.md", ErrTooLarge},
		{"invalid.md", ErrNotNote},
		{"cut-short.md", ErrNotNote},
		{"surrogate.md", ErrNotNote},
		{"a.txt", ErrNotNote},
		{"upper.MD", ErrNotNote},
		{"missing.txt", ErrNotNote}, // the name is judged before the filesystem is asked
		{"missing.md", ErrNotFound},
		{"missing/n.md", ErrNotFound},
		{"ok.md/inner.md", ErrNotFound}, // a note has nothing beneath it
		{"folder", ErrNotNote},
		{"weird.md", ErrNotNote}, // a folder that is named like a note
		{".hidden/x.md", ErrInvalidPath},
		{"sub/.hidden.md", ErrInvalidPath},
		{"link.md", ErrInvalidPath},
		{"../ok.md", ErrInvalidPath},
		{"/ok.md", ErrInvalidPath},
		{`sub\n.md`, ErrInvalidPath},
		{"", ErrInvalidPath},
	} {
		n, err := v.Read(tc.path)
		wantErr(t, fmt.Sprintf("Read(%q)", tc.path), err, tc.want)
		if tc.want != nil && !equalNotes(n, Note{}) {
			t.Errorf("Read(%q) returned %+v with its error, want the zero Note", tc.path, n)
		}
	}
}

// Opening a named pipe that has no writer blocks forever, and a Write would do
// it holding the vault's lock. A pipe called x.md is not a note, and the
// operations must say so without ever opening it.
func TestNamedPipesAreRefusedWithoutOpening(t *testing.T) {
	v, dir := openTestVault(t, Options{})
	pipe := filepath.Join(dir, "pipe.md")
	if err := syscall.Mkfifo(pipe, 0o644); err != nil {
		t.Skipf("cannot create a named pipe here: %v", err)
	}
	// Should an operation block anyway, opening the other end frees it, so a
	// failing test does not leave the suite hanging.
	t.Cleanup(func() {
		if f, err := os.OpenFile(pipe, os.O_RDWR, 0); err == nil {
			f.Close()
		}
	})

	var errs [4]error
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, errs[0] = v.Read("pipe.md")
		_, _, errs[1] = v.CurrentRevision("pipe.md")
		_, errs[2] = v.Write("pipe.md", "x", sha256Hex(""))
		_, errs[3] = v.Create("pipe.md", "x")
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("an operation on a named pipe blocked")
	}
	wantErr(t, "Read", errs[0], ErrNotNote)
	wantErr(t, "CurrentRevision", errs[1], ErrNotNote)
	wantErr(t, "Write", errs[2], ErrNotNote)
	wantErr(t, "Create", errs[3], ErrExists)
}

// os.Root follows a symlink that stays inside the vault, even in the middle of
// a path, so one Lstat of the whole path would see a note inside a symlinked
// folder as an ordinary note. Every component has to be checked, and a symlink
// anywhere makes the path invalid for every operation.
func TestSymlinksAreRefusedAnywhereInThePath(t *testing.T) {
	v, dir := openTestVault(t, Options{})
	outside := t.TempDir()
	writeFile(t, outside, "secret.md", "secret\n")
	writeFile(t, dir, "real/n.md", "note\n")
	writeFile(t, dir, "a/plain.md", "plain\n")
	mustSymlink(t, "real", filepath.Join(dir, "linkdir"))                                // a folder, inside the vault
	mustSymlink(t, "../real", filepath.Join(dir, "a", "b"))                              // the middle of a longer path
	mustSymlink(t, filepath.Join(dir, "real"), filepath.Join(dir, "abs"))                // an absolute target
	mustSymlink(t, outside, filepath.Join(dir, "escape"))                                // a folder outside the vault
	mustSymlink(t, "real/n.md", filepath.Join(dir, "link.md"))                           // a note
	mustSymlink(t, "missing.md", filepath.Join(dir, "dangling.md"))                      // a note that is not there
	mustSymlink(t, filepath.Join(outside, "secret.md"), filepath.Join(dir, "secret.md")) // a note outside the vault
	before, beforeOutside := diskTree(t, dir), diskTree(t, outside)
	revision := sha256Hex("note\n")

	for _, p := range []string{
		"linkdir/n.md", "linkdir/new.md", "linkdir/sub/new.md",
		"a/b/n.md", "a/b/new.md",
		"abs/n.md",
		"escape/secret.md", "escape/new.md",
		"link.md", "dangling.md", "secret.md",
	} {
		_, err := v.Read(p)
		wantErr(t, "Read("+p+")", err, ErrInvalidPath)
		_, _, err = v.CurrentRevision(p)
		wantErr(t, "CurrentRevision("+p+")", err, ErrInvalidPath)
		_, err = v.Write(p, "overwritten\n", revision)
		wantErr(t, "Write("+p+")", err, ErrInvalidPath)
		_, err = v.Create(p, "created\n")
		wantErr(t, "Create("+p+")", err, ErrInvalidPath)
	}

	if got := diskTree(t, dir); !slices.Equal(got, before) {
		t.Errorf("the vault changed:\n got %q\nwant %q", got, before)
	}
	if got := diskTree(t, outside); !slices.Equal(got, beforeOutside) {
		t.Errorf("the folder outside the vault changed:\n got %q\nwant %q", got, beforeOutside)
	}
	if got := readDisk(t, dir, "real/n.md"); got != "note\n" {
		t.Errorf("a note behind a symlink was changed to %q", got)
	}
	if got := readDisk(t, outside, "secret.md"); got != "secret\n" {
		t.Errorf("a note outside the vault was changed to %q", got)
	}
}

func TestWriteIsAtomicAndRevisionChecked(t *testing.T) {
	v, dir := openTestVault(t, Options{})
	const name = "folder/n.md"
	original := "# Original\r\nbody\r\n"
	writeFile(t, dir, name, original)
	full := filepath.Join(dir, "folder", "n.md")
	if err := os.Chmod(full, 0o640); err != nil {
		t.Fatal(err)
	}
	loaded := mustRead(t, v, name)

	// A reader that already has the note open keeps seeing the old bytes: the
	// file is replaced, never rewritten in place.
	reader, err := os.Open(full)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	updated := "# Updated\r\nnew body, no final newline"
	saved, err := v.Write(name, updated, loaded.Revision)
	if err != nil {
		t.Fatalf("Write with the revision that was loaded: %v", err)
	}
	if saved.Path != name || saved.Content != updated || saved.Revision != sha256Hex(updated) ||
		saved.Size != int64(len(updated)) || saved.Modified.IsZero() {
		t.Errorf("Write returned %+v, want the note as written", saved)
	}
	if saved.Revision == loaded.Revision {
		t.Error("the revision did not change although the content did")
	}
	if got := readDisk(t, dir, name); got != updated {
		t.Errorf("file = %q, want %q", got, updated)
	}
	if want := []string{"folder/", "folder/n.md"}; !slices.Equal(diskTree(t, dir), want) {
		t.Errorf("directory = %q, want only the target %q (no temp files)", diskTree(t, dir), want)
	}
	if got := modeOf(t, dir, name); got != 0o640 {
		t.Errorf("mode = %v, want 0640", got)
	}
	if old, err := io.ReadAll(reader); err != nil || string(old) != original {
		t.Errorf("a reader that opened the note before the write got %q, %v; want the old bytes %q", old, err, original)
	}
	if again := mustRead(t, v, name); !equalNotes(again, saved) {
		t.Errorf("Read after Write = %+v, want what Write returned, %+v", again, saved)
	}

	// An empty revision is not a way to skip the check.
	_, err = v.Write(name, "no revision given\n", "")
	wantErr(t, "Write with an empty revision", err, ErrConflict)

	// A revision that is stale because another program changed the file is a
	// conflict, and the error names what is on disk now.
	external := "changed by another program\n"
	writeFile(t, dir, name, external)
	n, err := v.Write(name, "my edit\n", saved.Revision)
	wantErr(t, "Write with a stale revision", err, ErrConflict)
	var conflict *ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("error = %v, want a *ConflictError", err)
	}
	if conflict.Current != sha256Hex(external) {
		t.Errorf("ConflictError.Current = %s, want the revision on disk, %s", conflict.Current, sha256Hex(external))
	}
	if !equalNotes(n, Note{}) {
		t.Errorf("Write returned %+v with a conflict, want the zero Note", n)
	}
	if got := readDisk(t, dir, name); got != external {
		t.Errorf("a conflicting write changed the file to %q, want %q", got, external)
	}
	if want := []string{"folder/", "folder/n.md"}; !slices.Equal(diskTree(t, dir), want) {
		t.Errorf("directory after a conflict = %q, want only the target (no temp files)", diskTree(t, dir))
	}

	// Saving against the revision the conflict reported replaces the disk version.
	overwritten, err := v.Write(name, "my edit\n", conflict.Current)
	if err != nil {
		t.Fatalf("Write with the revision from the conflict: %v", err)
	}
	if got := readDisk(t, dir, name); got != "my edit\n" {
		t.Errorf("file = %q, want my edit", got)
	}

	// A note that was deleted is not found, and the write does not bring it back.
	if err := os.Remove(full); err != nil {
		t.Fatal(err)
	}
	_, err = v.Write(name, "too late\n", overwritten.Revision)
	wantErr(t, "Write after the note was deleted", err, ErrNotFound)
	if errors.Is(err, ErrConflict) {
		t.Error("a deleted note is reported as a conflict, want ErrNotFound only")
	}
	if want := []string{"folder/"}; !slices.Equal(diskTree(t, dir), want) {
		t.Errorf("directory after a write to a deleted note = %q, want only the folder", diskTree(t, dir))
	}
}

// The mode of the note is copied to its replacement exactly, whatever the
// umask would have made of it.
func TestWritePreservesFileMode(t *testing.T) {
	setUmask(t, 0o077) // a temp file created with 0640 would come out 0600
	v, dir := openTestVault(t, Options{})
	for i, mode := range []fs.FileMode{0o640, 0o600, 0o644, 0o664, 0o666, 0o755} {
		name := fmt.Sprintf("n%d.md", i)
		writeFile(t, dir, name, "before\n")
		if err := os.Chmod(filepath.Join(dir, name), mode); err != nil {
			t.Fatal(err)
		}
		if _, err := v.Write(name, "after\n", sha256Hex("before\n")); err != nil {
			t.Fatalf("Write(%s): %v", name, err)
		}
		if got := modeOf(t, dir, name); got != mode {
			t.Errorf("mode of a note that was %v = %v after Write", mode, got)
		}
	}
}

func TestWriteSerializesWriters(t *testing.T) {
	for _, writers := range []int{2, 16} {
		t.Run(fmt.Sprintf("%d writers", writers), func(t *testing.T) {
			v, dir := openTestVault(t, Options{})
			writeFile(t, dir, "n.md", "base\n")
			base := mustRead(t, v, "n.md")

			type outcome struct {
				content string
				err     error
			}
			outcomes := make([]outcome, writers)
			start := make(chan struct{})
			var wg sync.WaitGroup
			for i := range writers {
				wg.Add(1)
				go func() {
					defer wg.Done()
					content := fmt.Sprintf("writer %d\n", i)
					<-start
					_, err := v.Write("n.md", content, base.Revision)
					outcomes[i] = outcome{content, err}
				}()
			}
			close(start)
			wg.Wait()

			var winner string
			var wins, conflicts int
			for _, o := range outcomes {
				switch {
				case o.err == nil:
					wins++
					winner = o.content
				case errors.Is(o.err, ErrConflict):
					conflicts++
				default:
					t.Errorf("unexpected error: %v", o.err)
				}
			}
			if wins != 1 || conflicts != writers-1 {
				t.Fatalf("%d writers using one revision: %d succeeded and %d conflicted, want exactly 1 and %d", writers, wins, conflicts, writers-1)
			}
			if got := readDisk(t, dir, "n.md"); got != winner {
				t.Errorf("file = %q, want the winner's %q", got, winner)
			}
			// Every loser saw the winner's note, never the one it loaded.
			for _, o := range outcomes {
				var conflict *ConflictError
				if errors.As(o.err, &conflict) && conflict.Current != sha256Hex(winner) {
					t.Errorf("a losing writer was told the current revision is %s, want the winner's %s", conflict.Current, sha256Hex(winner))
				}
			}
			if want := []string{"n.md"}; !slices.Equal(diskTree(t, dir), want) {
				t.Errorf("directory = %q, want only the target", diskTree(t, dir))
			}
		})
	}
}

// Readers must see the old note or the new one, whole, however many times it
// is replaced under them.
func TestReadersSeeWholeNotesWhileItIsRewritten(t *testing.T) {
	v, dir := openTestVault(t, Options{})
	versions := [2]string{strings.Repeat("a", 256<<10), strings.Repeat("b", 256<<10)}
	writeFile(t, dir, "n.md", versions[0])

	stop := make(chan struct{})
	problem := make(chan string, 1)
	var readers sync.WaitGroup
	for range 4 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				n, err := v.Read("n.md")
				var bad string
				switch {
				case err != nil:
					bad = fmt.Sprintf("Read failed: %v", err)
				case n.Content != versions[0] && n.Content != versions[1]:
					bad = fmt.Sprintf("Read returned %d bytes that are neither version of the note", len(n.Content))
				case n.Revision != sha256Hex(n.Content):
					bad = "the revision does not belong to the content that was read"
				}
				if bad != "" {
					select {
					case problem <- bad:
					default:
					}
					return
				}
			}
		}()
	}

	revision := sha256Hex(versions[0])
	for i := 1; i <= 12; i++ {
		saved, err := v.Write("n.md", versions[i%2], revision)
		if err != nil {
			close(stop)
			readers.Wait()
			t.Fatalf("Write %d: %v", i, err)
		}
		revision = saved.Revision
	}
	close(stop)
	readers.Wait()
	select {
	case bad := <-problem:
		t.Error(bad)
	default:
	}
}

func TestWriteRejectsInvalidInput(t *testing.T) {
	v, dir := openTestVault(t, Options{})
	writeFile(t, dir, "n.md", "note\n")
	writeFile(t, dir, "big.md", strings.Repeat("a", MaxNoteBytes+1))
	writeFile(t, dir, "invalid.md", "\xff\xfe")
	writeFile(t, dir, "plain.txt", "text")
	mustMkdir(t, filepath.Join(dir, "folder"))
	mustMkdir(t, filepath.Join(dir, "weird.md"))
	revision := sha256Hex("note\n")
	tooLarge := strings.Repeat("a", MaxNoteBytes+1)
	before := diskTree(t, dir)

	for _, tc := range []struct {
		what, path, content string
		want                error
	}{
		{"a hidden folder", ".hidden/n.md", "x", ErrInvalidPath},
		{"a hidden note", "sub/.n.md", "x", ErrInvalidPath},
		{"parent traversal", "../n.md", "x", ErrInvalidPath},
		{"an absolute path", "/n.md", "x", ErrInvalidPath},
		{"a backslash", `a\n.md`, "x", ErrInvalidPath},
		{"an empty path", "", "x", ErrInvalidPath},
		{"a name that is not a note", "plain.txt", "x", ErrNotNote},
		{"a folder", "folder", "x", ErrNotNote},
		{"a folder named like a note", "weird.md", "x", ErrNotNote},
		{"content over the limit", "n.md", tooLarge, ErrTooLarge},
		{"content that is not UTF-8", "n.md", "\xff\xfe", ErrNotNote},
		{"content with an encoded surrogate", "n.md", "\xed\xa0\x80", ErrNotNote},
		{"a note that is not there", "missing.md", "x", ErrNotFound},
		{"a folder that is not there", "nope/n.md", "x", ErrNotFound},
		{"a note used as a folder", "n.md/inner.md", "x", ErrNotFound},
		{"a note on disk over the limit", "big.md", "x", ErrTooLarge},
		{"a note on disk that is not UTF-8", "invalid.md", "x", ErrNotNote},
	} {
		n, err := v.Write(tc.path, tc.content, revision)
		wantErr(t, "Write to "+tc.what, err, tc.want)
		if !equalNotes(n, Note{}) {
			t.Errorf("Write to %s returned %+v with its error, want the zero Note", tc.what, n)
		}
		if got := diskTree(t, dir); !slices.Equal(got, before) {
			t.Errorf("Write to %s changed the directory:\n got %q\nwant %q", tc.what, got, before)
		}
	}
	if got := readDisk(t, dir, "n.md"); got != "note\n" {
		t.Errorf("n.md = %q after the rejected writes, want it unchanged", got)
	}

	// The limit is on the bytes: exactly 2 MiB is fine.
	atLimit := strings.Repeat("é", MaxNoteBytes/2)
	if len(atLimit) != MaxNoteBytes {
		t.Fatalf("test fixture: %d bytes, want %d", len(atLimit), MaxNoteBytes)
	}
	if _, err := v.Write("n.md", atLimit, revision); err != nil {
		t.Errorf("Write of exactly MaxNoteBytes: %v", err)
	}
}

// What is saved is exactly what was given: line endings, a BOM, trailing
// spaces, a missing final newline and Unicode in any form all survive, whether
// the note is created with them or later saved with them.
func TestWriteAndCreateStoreExactBytes(t *testing.T) {
	v, dir := openTestVault(t, Options{})
	contents := []string{
		"# Title\r\n\r\ncrlf\r\nline\r\n",
		utf8BOM + "a BOM first\n",
		utf8BOM + "a BOM and crlf\r\n",
		"trailing spaces   \n\t\n",
		"no final newline",
		"mixed\nendings\r\nand a bare\rcr\n",
		"",
		"nul\x00byte\n",
		"e\xcc\x81 stays decomposed, ¿Qué? 日本語 👋\n",
		strings.Repeat("a", MaxNoteBytes),
	}
	for i, content := range contents {
		wantNote := Note{Content: content, Revision: sha256Hex(content), Size: int64(len(content))}

		created := fmt.Sprintf("created-%d.md", i)
		n, err := v.Create(created, content)
		if err != nil {
			t.Errorf("Create(%q): %v", created, err)
			continue
		}
		wantNote.Path = created
		wantNote.Modified = n.Modified
		if !equalNotes(n, wantNote) {
			t.Errorf("Create returned a note whose content differs from the %d bytes given (path %q)", len(content), n.Path)
		}
		if got := readDisk(t, dir, created); got != content {
			t.Errorf("%s on disk has %d bytes, want the %d given", created, len(got), len(content))
		}

		written := fmt.Sprintf("written-%d.md", i)
		writeFile(t, dir, written, "seed\n")
		n, err = v.Write(written, content, sha256Hex("seed\n"))
		if err != nil {
			t.Errorf("Write(%q): %v", written, err)
			continue
		}
		wantNote.Path = written
		wantNote.Modified = n.Modified
		if !equalNotes(n, wantNote) {
			t.Errorf("Write returned a note whose content differs from the %d bytes given (path %q)", len(content), n.Path)
		}
		if got := readDisk(t, dir, written); got != content {
			t.Errorf("%s on disk has %d bytes, want the %d given", written, len(got), len(content))
		}

		// Saving what is already there, as an editor does after an edit that was
		// undone, changes nothing.
		again, err := v.Write(written, content, n.Revision)
		if err != nil {
			t.Errorf("Write of unchanged content to %q: %v", written, err)
		} else if again.Revision != n.Revision || readDisk(t, dir, written) != content {
			t.Errorf("Write of unchanged content to %q changed the note", written)
		}
	}
}

func TestCreate(t *testing.T) {
	setUmask(t, 0o022)
	v, dir := openTestVault(t, Options{})
	writeFile(t, dir, "existing.md", "existing\n")
	writeFile(t, dir, "file.md", "a note, not a folder\n")
	mustMkdir(t, filepath.Join(dir, "folder.md"))

	content := "# New note\r\nwith CRLF and no final newline"
	created, err := v.Create("deep/new/note.md", content)
	if err != nil {
		t.Fatalf("Create with parent folders: %v", err)
	}
	if created.Path != "deep/new/note.md" || created.Content != content || created.Revision != sha256Hex(content) ||
		created.Size != int64(len(content)) || created.Modified.IsZero() {
		t.Errorf("Create returned %+v, want the note as created", created)
	}
	if got := readDisk(t, dir, "deep/new/note.md"); got != content {
		t.Errorf("file = %q, want %q", got, content)
	}
	if got := modeOf(t, dir, "deep/new/note.md"); got != 0o644 {
		t.Errorf("note mode = %v, want 0644", got)
	}
	for _, folder := range []string{"deep", "deep/new"} {
		if got := modeOf(t, dir, folder); got != 0o755 {
			t.Errorf("mode of folder %s = %v, want 0755", folder, got)
		}
	}
	if again := mustRead(t, v, "deep/new/note.md"); !equalNotes(again, created) {
		t.Errorf("Read after Create = %+v, want what Create returned, %+v", again, created)
	}
	if want := []string{"deep/", "deep/new/", "deep/new/note.md", "existing.md", "file.md", "folder.md/"}; !slices.Equal(diskTree(t, dir), want) {
		t.Errorf("directory = %q, want %q", diskTree(t, dir), want)
	}

	// Every refusal leaves the vault exactly as it was: no note, no temp file
	// and no folder that was made on the way.
	before := diskTree(t, dir)
	for _, tc := range []struct {
		what, path, content string
		want                error
	}{
		{"a note that exists", "deep/new/note.md", "other", ErrExists},
		{"another note that exists", "existing.md", "other", ErrExists},
		{"a folder in the way", "folder.md", "x", ErrExists},
		{"a note in the way of a folder", "file.md/inner.md", "x", ErrExists},
		{"a note in the way of a deeper folder", "file.md/a/b.md", "x", ErrExists},
		{"a name that is not a note", "x.txt", "x", ErrNotNote},
		{"a name that is not a note under new folders", "fresh/folders/x.txt", "x", ErrNotNote},
		{"no extension", "fresh/x", "x", ErrNotNote},
		{"content over the limit", "fresh/folders/big.md", strings.Repeat("a", MaxNoteBytes+1), ErrTooLarge},
		{"content that is not UTF-8", "fresh/folders/bad.md", "\xff\xfe", ErrNotNote},
		{"a hidden folder", ".hidden/x.md", "x", ErrInvalidPath},
		{"a hidden note", "sub/.x.md", "x", ErrInvalidPath},
		{"parent traversal", "../x.md", "x", ErrInvalidPath},
		{"an absolute path", "/x.md", "x", ErrInvalidPath},
		{"a backslash", `a\x.md`, "x", ErrInvalidPath},
		{"a trailing slash", "fresh/", "x", ErrInvalidPath},
		{"an empty path", "", "x", ErrInvalidPath},
		{"a name that is not UTF-8", "bad\xffname.md", "x", ErrInvalidPath},
	} {
		n, err := v.Create(tc.path, tc.content)
		wantErr(t, "Create of "+tc.what, err, tc.want)
		if !equalNotes(n, Note{}) {
			t.Errorf("Create of %s returned %+v with its error, want the zero Note", tc.what, n)
		}
		if got := diskTree(t, dir); !slices.Equal(got, before) {
			t.Errorf("Create of %s changed the directory:\n got %q\nwant %q", tc.what, got, before)
		}
	}
	if got := readDisk(t, dir, "deep/new/note.md"); got != content {
		t.Errorf("the first note was changed to %q by a later Create", got)
	}
	if got := readDisk(t, dir, "existing.md"); got != "existing\n" {
		t.Errorf("existing.md was changed to %q by Create", got)
	}

	// The smallest and the largest note both work.
	for _, c := range []string{"", strings.Repeat("a", MaxNoteBytes)} {
		name := fmt.Sprintf("size-%d.md", len(c))
		if _, err := v.Create(name, c); err != nil {
			t.Errorf("Create(%s): %v", name, err)
		}
	}
}

func TestCreateSerializesCreators(t *testing.T) {
	v, dir := openTestVault(t, Options{})
	const creators = 16
	type outcome struct {
		content string
		err     error
	}
	outcomes := make([]outcome, creators)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range creators {
		wg.Add(1)
		go func() {
			defer wg.Done()
			content := fmt.Sprintf("creator %d\n", i)
			<-start
			_, err := v.Create("fresh/same.md", content)
			outcomes[i] = outcome{content, err}
		}()
	}
	close(start)
	wg.Wait()

	var winner string
	wins := 0
	for _, o := range outcomes {
		switch {
		case o.err == nil:
			wins++
			winner = o.content
		case errors.Is(o.err, ErrExists):
		default:
			t.Errorf("unexpected error: %v", o.err)
		}
	}
	if wins != 1 {
		t.Fatalf("%d creators of one path: %d succeeded, want exactly 1 (the others ErrExists)", creators, wins)
	}
	if got := readDisk(t, dir, "fresh/same.md"); got != winner {
		t.Errorf("file = %q, want the winner's %q", got, winner)
	}
	if want := []string{"fresh/", "fresh/same.md"}; !slices.Equal(diskTree(t, dir), want) {
		t.Errorf("directory = %q, want %q", diskTree(t, dir), want)
	}
}

// A write that fails part-way, as it does on a full disk, must leave the note
// as it was and no temp file behind, and must not leave the vault stuck.
func TestFailedWriteKeepsTheNoteAndLeavesNoTemp(t *testing.T) {
	v, dir := openTestVault(t, Options{})
	original := "original\n"
	writeFile(t, dir, "sub/n.md", original)
	revision := sha256Hex(original)

	lift := limitFileSize(t, 1024) // the first 1 KiB of the new text is written, then the write fails
	n, err := v.Write("sub/n.md", strings.Repeat("x", 64<<10), revision)
	lift()

	if err == nil {
		t.Fatal("Write succeeded although the filesystem refused the data")
	}
	if !errors.Is(err, syscall.EFBIG) {
		t.Errorf("error = %v, want the file too large error of the filesystem", err)
	}
	if !equalNotes(n, Note{}) {
		t.Errorf("Write returned %+v with its error, want the zero Note", n)
	}
	if got := readDisk(t, dir, "sub/n.md"); got != original {
		t.Errorf("note = %d bytes after a failed write, want the original %q", len(got), original)
	}
	if want := []string{"sub/", "sub/n.md"}; !slices.Equal(diskTree(t, dir), want) {
		t.Errorf("directory = %q, want only the target (the temp file must be removed)", diskTree(t, dir))
	}

	saved, err := v.Write("sub/n.md", "new\n", revision)
	if err != nil {
		t.Fatalf("Write once the filesystem works again: %v", err)
	}
	if got := readDisk(t, dir, "sub/n.md"); got != "new\n" || saved.Revision != sha256Hex("new\n") {
		t.Errorf("note = %q after the retry, want %q", got, "new\n")
	}
}

func TestFailedCreateLeavesNoNote(t *testing.T) {
	v, dir := openTestVault(t, Options{})
	mustMkdir(t, filepath.Join(dir, "sub"))

	lift := limitFileSize(t, 1024)
	n, err := v.Create("sub/new.md", strings.Repeat("x", 64<<10))
	lift()

	if err == nil {
		t.Fatal("Create succeeded although the filesystem refused the data")
	}
	if !errors.Is(err, syscall.EFBIG) {
		t.Errorf("error = %v, want the file too large error of the filesystem", err)
	}
	if !equalNotes(n, Note{}) {
		t.Errorf("Create returned %+v with its error, want the zero Note", n)
	}
	if want := []string{"sub/"}; !slices.Equal(diskTree(t, dir), want) {
		t.Errorf("directory = %q, want no note: a half-written one must be removed", diskTree(t, dir))
	}

	// The failed attempt does not make the name "already exists".
	if _, err := v.Create("sub/new.md", "hello\n"); err != nil {
		t.Errorf("Create once the filesystem works again: %v", err)
	}
}

// replaceFile is the step after the revision check: it puts the new text next to
// the note, then renames it into place. When that rename fails the temp file,
// already written and synced, must not stay behind.
func TestReplaceFileRemovesItsTempFileWhenTheRenameFails(t *testing.T) {
	v, dir := openTestVault(t, Options{})
	writeFile(t, dir, "target/inner.md", "inside\n")

	// A file cannot be renamed over a folder that has something in it.
	if _, err := v.replaceFile("target", "new text\n", 0o644); err == nil {
		t.Fatal("replaceFile over a folder succeeded")
	}
	if want := []string{"target/", "target/inner.md"}; !slices.Equal(diskTree(t, dir), want) {
		t.Errorf("directory = %q, want it as it was, without a temp file", diskTree(t, dir))
	}
	if got := readDisk(t, dir, "target/inner.md"); got != "inside\n" {
		t.Errorf("inner.md = %q, want it untouched", got)
	}
}

func TestTempSuffix(t *testing.T) {
	eightHex := regexp.MustCompile(`^[0-9a-f]{8}$`)
	seen := map[string]bool{}
	for range 50 {
		s := tempSuffix()
		if !eightHex.MatchString(s) {
			t.Fatalf("tempSuffix() = %q, want 8 lowercase hex digits", s)
		}
		seen[s] = true
	}
	if len(seen) < 49 { // 50 draws of 32 random bits that mostly repeat are not random
		t.Errorf("tempSuffix() gave %d different values in 50 calls", len(seen))
	}
}

// The temp file is called .<name of the note>.<8 hex digits>.tmp, in the note's
// folder, and is created with O_EXCL: a file that is already there under that
// name (left by a crash, or anybody's) is neither written into nor removed, and
// the note stays as it was.
func TestWriteNeverUsesAnExistingTempFile(t *testing.T) {
	old := tempSuffix
	tempSuffix = func() string { return "0badf00d" }
	t.Cleanup(func() { tempSuffix = old })

	v, dir := openTestVault(t, Options{})
	writeFile(t, dir, "sub/n.md", "original\n")
	writeFile(t, dir, "sub/.n.md.0badf00d.tmp", "not ours\n")

	_, err := v.Write("sub/n.md", "new\n", sha256Hex("original\n"))
	wantErr(t, "Write with a file in the way of its temp file", err, fs.ErrExist)
	if got := readDisk(t, dir, "sub/n.md"); got != "original\n" {
		t.Errorf("note = %q, want it unchanged", got)
	}
	if got := readDisk(t, dir, "sub/.n.md.0badf00d.tmp"); got != "not ours\n" {
		t.Errorf("the file in the way = %q, want it neither written into nor removed", got)
	}
	if want := []string{"sub/", "sub/.n.md.0badf00d.tmp", "sub/n.md"}; !slices.Equal(diskTree(t, dir), want) {
		t.Errorf("directory = %q, want %q", diskTree(t, dir), want)
	}
}

// A folder the process may not write into: the note stays as it was, and the
// error is the permission error, not a conflict.
func TestWriteInUnwritableFolderFailsCleanly(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission checks do not apply to root")
	}
	v, dir := openTestVault(t, Options{})
	writeFile(t, dir, "locked/n.md", "original\n")
	locked := filepath.Join(dir, "locked")
	if err := os.Chmod(locked, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })

	_, err := v.Write("locked/n.md", "new\n", sha256Hex("original\n"))
	wantErr(t, "Write into a read-only folder", err, fs.ErrPermission)
	if errors.Is(err, ErrConflict) {
		t.Error("a permission error is reported as a conflict")
	}
	if got := readDisk(t, dir, "locked/n.md"); got != "original\n" {
		t.Errorf("note = %q, want it unchanged", got)
	}
	if want := []string{"locked/", "locked/n.md"}; !slices.Equal(diskTree(t, dir), want) {
		t.Errorf("directory = %q, want no temp file", diskTree(t, dir))
	}
}

// clip keeps temp file names short enough, and a name cut in the middle of a
// character is not valid UTF-8, which some filesystems refuse.
func TestClip(t *testing.T) {
	for _, tc := range []struct {
		in   string
		n    int
		want string
	}{
		{"abc", 10, "abc"},
		{"abc", 3, "abc"},
		{"abc", 2, "ab"},
		{"", 5, ""},
		{"日日日", 9, "日日日"},
		{"日日日", 8, "日日"}, // the third character would be cut in two
		{"日日日", 7, "日日"},
		{"日日日", 6, "日日"},
		{"日日日", 2, ""},
		{"é", 1, ""},
		{"aé", 2, "a"},
		{"a👋", 4, "a"}, // the emoji takes 4 bytes
		{"a👋", 5, "a👋"},
	} {
		if got := clip(tc.in, tc.n); got != tc.want {
			t.Errorf("clip(%q, %d) = %q, want %q", tc.in, tc.n, got, tc.want)
		}
	}
}

// The temp file's name is the note's plus 14 bytes. A note whose name is at the
// 255 byte limit must still be saveable.
func TestWriteNamesNearTheLengthLimit(t *testing.T) {
	for i, name := range []string{
		strings.Repeat("a", 252) + ".md",          // 255 bytes
		strings.Repeat("é", 126) + ".md",          // 255 bytes in 129 characters
		strings.Repeat("日", 84) + ".md",           // 255 bytes, and 200 falls inside a character
		"sub/" + strings.Repeat("b", 252) + ".md", // the same in a folder
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			v, dir := openTestVault(t, Options{})
			created, err := v.Create(name, "first\n")
			if errors.Is(err, syscall.ENAMETOOLONG) || errors.Is(err, syscall.EILSEQ) || errors.Is(err, syscall.EINVAL) {
				t.Skipf("this filesystem refuses to create %s: %v", describe(name), err)
			}
			if err != nil {
				t.Fatalf("Create(%s): %v", describe(name), err)
			}
			if _, err := v.Write(name, "second\n", created.Revision); err != nil {
				t.Fatalf("Write(%s): %v", describe(name), err)
			}
			if got := readDisk(t, dir, name); got != "second\n" {
				t.Errorf("note = %q, want %q", got, "second\n")
			}
			want := []string{name}
			if folder, _, inFolder := strings.Cut(name, "/"); inFolder {
				want = []string{folder + "/", name}
			}
			if got := diskTree(t, dir); !slices.Equal(got, want) {
				t.Errorf("directory = %q, want only the note and its folder, %q", short(got), short(want))
			}
		})
	}
}

func TestReadOnlyVaultRejectsWrites(t *testing.T) {
	v, dir := openTestVault(t, Options{ReadOnly: true})
	writeFile(t, dir, "n.md", "note\n")
	before := diskTree(t, dir)
	revision := sha256Hex("note\n")

	n, err := v.Write("n.md", "changed\n", revision)
	wantErr(t, "Write", err, ErrReadOnly)
	if !equalNotes(n, Note{}) {
		t.Errorf("Write returned %+v with its error, want the zero Note", n)
	}
	n, err = v.Create("new.md", "x")
	wantErr(t, "Create", err, ErrReadOnly)
	if !equalNotes(n, Note{}) {
		t.Errorf("Create returned %+v with its error, want the zero Note", n)
	}
	_, err = v.Create("fresh/folder/new.md", "x")
	wantErr(t, "Create under new folders", err, ErrReadOnly)

	if got := diskTree(t, dir); !slices.Equal(got, before) {
		t.Errorf("a read-only vault changed:\n got %q\nwant %q", got, before)
	}
	if got := readDisk(t, dir, "n.md"); got != "note\n" {
		t.Errorf("n.md = %q, want it unchanged", got)
	}

	// Reading still works.
	read, err := v.Read("n.md")
	if err != nil || read.Content != "note\n" || read.Revision != revision {
		t.Errorf("Read = %+v, %v; want the note", read, err)
	}
	rev, _, err := v.CurrentRevision("n.md")
	if err != nil || rev != revision {
		t.Errorf("CurrentRevision = %q, %v; want %q", rev, err, revision)
	}
}

func TestCurrentRevision(t *testing.T) {
	v, dir := openTestVault(t, Options{})
	writeFile(t, dir, "sub/n.md", "one\n")
	first := time.Date(2023, 4, 5, 6, 7, 8, 0, time.UTC)
	chtimes(t, dir, "sub/n.md", first)

	rev, modified, err := v.CurrentRevision("sub/n.md")
	if err != nil {
		t.Fatal(err)
	}
	if rev != sha256Hex("one\n") || !modified.Equal(first) {
		t.Errorf("CurrentRevision = %s, %v; want %s, %v", rev, modified, sha256Hex("one\n"), first)
	}
	if n := mustRead(t, v, "sub/n.md"); n.Revision != rev || !n.Modified.Equal(modified) {
		t.Errorf("Read says %s, %v but CurrentRevision says %s, %v", n.Revision, n.Modified, rev, modified)
	}

	// It follows the disk: a change made by another program shows at once.
	second := first.Add(time.Hour)
	writeFile(t, dir, "sub/n.md", "two\n")
	chtimes(t, dir, "sub/n.md", second)
	rev, modified, err = v.CurrentRevision("sub/n.md")
	if err != nil || rev != sha256Hex("two\n") || !modified.Equal(second) {
		t.Errorf("CurrentRevision after an external edit = %s, %v, %v; want %s, %v", rev, modified, err, sha256Hex("two\n"), second)
	}

	// The same refusals as Read, with nothing returned.
	writeFile(t, dir, "big.md", strings.Repeat("a", MaxNoteBytes+1))
	writeFile(t, dir, "invalid.md", "\xff\xfe")
	writeFile(t, dir, "a.txt", "text")
	mustMkdir(t, filepath.Join(dir, "folder"))
	mustSymlink(t, "sub/n.md", filepath.Join(dir, "link.md"))
	for _, tc := range []struct {
		path string
		want error
	}{
		{"missing.md", ErrNotFound},
		{"big.md", ErrTooLarge},
		{"invalid.md", ErrNotNote},
		{"a.txt", ErrNotNote},
		{"folder", ErrNotNote},
		{".hidden/x.md", ErrInvalidPath},
		{"link.md", ErrInvalidPath},
		{"../n.md", ErrInvalidPath},
	} {
		rev, modified, err := v.CurrentRevision(tc.path)
		wantErr(t, fmt.Sprintf("CurrentRevision(%q)", tc.path), err, tc.want)
		if rev != "" || !modified.IsZero() {
			t.Errorf("CurrentRevision(%q) = %q, %v with its error, want zero values", tc.path, rev, modified)
		}
	}
}

// Review Focus 4: names with accents, spaces, ?, #, %, commas and brackets work
// for every operation, and are stored under exactly the name given.
func TestUnicodePaths(t *testing.T) {
	v, dir := openTestVault(t, Options{})
	names := []string{
		"¿Qué es el Estoicismo?.md",
		"100% #1.md",
		"Dario Z - El amor - Clase 1.md",
		"a,b [c].md",
		"Café ¿Qué?/a,b [c].md",
	}
	for _, name := range names {
		created, err := v.Create(name, "first\n")
		if err != nil {
			t.Errorf("Create(%q): %v", name, err)
			continue
		}
		if read := mustRead(t, v, name); !equalNotes(read, created) {
			t.Errorf("Read(%q) = %+v, want what Create returned, %+v", name, read, created)
		}
		rev, modified, err := v.CurrentRevision(name)
		if err != nil || rev != created.Revision || !modified.Equal(created.Modified) {
			t.Errorf("CurrentRevision(%q) = %q, %v, %v; want %q, %v", name, rev, modified, err, created.Revision, created.Modified)
		}
		saved, err := v.Write(name, "second\n", rev)
		if err != nil {
			t.Errorf("Write(%q): %v", name, err)
			continue
		}
		if saved.Path != name || saved.Revision != sha256Hex("second\n") {
			t.Errorf("Write(%q) returned %+v", name, saved)
		}
		if rev, _, err := v.CurrentRevision(name); err != nil || rev != saved.Revision {
			t.Errorf("CurrentRevision(%q) after Write = %q, %v; want %q", name, rev, err, saved.Revision)
		}
		if got := readDisk(t, dir, name); got != "second\n" {
			t.Errorf("%q on disk = %q, want %q", name, got, "second\n")
		}
	}

	want := append([]string{"Café ¿Qué?/"}, names...)
	slices.Sort(want)
	if got := diskTree(t, dir); !slices.Equal(got, want) {
		t.Errorf("directory = %q, want exactly these names and no temp files: %q", got, want)
	}
	notes := slices.Clone(names)
	slices.Sort(notes)
	if got := entryPaths(listVault(t, v).Notes); !slices.Equal(got, notes) {
		t.Errorf("List = %q, want %q", got, notes)
	}
}
