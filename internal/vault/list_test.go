package vault

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

var fingerprintPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// listVault lists the vault and checks the invariant every listing has: each
// path in it passes CleanPath, so everything listed can be opened.
func listVault(t *testing.T, v *Vault) Listing {
	t.Helper()
	l, err := v.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, p := range slices.Concat(entryPaths(l.Notes), entryPaths(l.Assets), l.Folders) {
		if _, err := CleanPath(p); err != nil {
			t.Errorf("List returned %s, which CleanPath rejects", describe(p))
		}
	}
	return l
}

// short describes paths for failure messages; the paths of the tests that
// probe the length limits are too long to read otherwise.
func short(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = describe(p)
	}
	return out
}

func entryPaths(entries []Entry) []string {
	paths := make([]string, len(entries))
	for i, e := range entries {
		paths[i] = e.Path
	}
	return paths
}

func chtimes(t *testing.T, dir, name string, mtime time.Time) {
	t.Helper()
	if err := os.Chtimes(filepath.Join(dir, filepath.FromSlash(name)), mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

// hookContext reports what onErr returns on each Err call, so a test can cancel
// a walk, or change the vault, part-way through it. List calls Err once for each
// entry the walk visits, the vault directory itself being the first.
type hookContext struct {
	context.Context
	calls int
	onErr func(call int) error
}

func (c *hookContext) Err() error {
	c.calls++
	return c.onErr(c.calls)
}

func TestListSkipsHiddenSymlinksAndOtherFiles(t *testing.T) {
	v, dir := openTestVault(t, Options{})
	content := map[string]string{"a.md": "# a\n", "sub/b.md": "bee", "pic.PNG": "png bytes"}
	mtime := map[string]time.Time{
		"a.md":     time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC),
		"sub/b.md": time.Date(2023, 1, 2, 3, 4, 5, 0, time.UTC),
		"pic.PNG":  time.Date(2022, 11, 12, 13, 14, 15, 0, time.UTC),
	}
	for name, text := range content {
		writeFile(t, dir, name, text)
		chtimes(t, dir, name, mtime[name])
	}
	mustMkdir(t, filepath.Join(dir, "sub", "empty"))
	writeFile(t, dir, ".obsidian/x.md", "x")
	writeFile(t, dir, ".trash/y.md", "y")
	writeFile(t, dir, "c.txt", "c")
	mustSymlink(t, "a.md", filepath.Join(dir, "link.md"))
	mustSymlink(t, "sub", filepath.Join(dir, "linkdir"))

	got := listVault(t, v)

	if want := []string{"a.md", "sub/b.md"}; !slices.Equal(entryPaths(got.Notes), want) {
		t.Errorf("Notes = %v, want %v", entryPaths(got.Notes), want)
	}
	if want := []string{"pic.PNG"}; !slices.Equal(entryPaths(got.Assets), want) {
		t.Errorf("Assets = %v, want %v", entryPaths(got.Assets), want)
	}
	if want := []string{"sub", "sub/empty"}; !slices.Equal(got.Folders, want) {
		t.Errorf("Folders = %v, want %v", got.Folders, want)
	}
	for _, e := range slices.Concat(got.Notes, got.Assets) {
		if want := int64(len(content[e.Path])); e.Size != want {
			t.Errorf("%s: Size = %d, want %d", e.Path, e.Size, want)
		}
		if !e.Modified.Equal(mtime[e.Path]) {
			t.Errorf("%s: Modified = %v, want %v", e.Path, e.Modified, mtime[e.Path])
		}
	}
}

// A hidden file must not end the walk of its folder: skipping a hidden
// directory is fs.SkipDir, skipping a hidden file is just moving on.
func TestListHiddenFileDoesNotHideSiblings(t *testing.T) {
	v, dir := openTestVault(t, Options{})
	writeFile(t, dir, ".DS_Store", "junk")
	writeFile(t, dir, "a.md", "a")
	writeFile(t, dir, "z.md", "z")
	writeFile(t, dir, "sub/.hidden.md", "h")
	writeFile(t, dir, "sub/.syncthing.b.md.tmp", "t")
	writeFile(t, dir, "sub/b.md", "b")
	writeFile(t, dir, "sub/c.md", "c")

	got := listVault(t, v)
	if want := []string{"a.md", "sub/b.md", "sub/c.md", "z.md"}; !slices.Equal(entryPaths(got.Notes), want) {
		t.Errorf("Notes = %v, want %v", entryPaths(got.Notes), want)
	}
}

func TestListSortsByPathBytes(t *testing.T) {
	v, dir := openTestVault(t, Options{})
	// Walking folder by folder would give a/b.md before a.md; byte order of the
	// whole path puts "." (0x2e) before "/" (0x2f). No two names differ only by
	// case because the temp dir may be on a case-insensitive filesystem.
	for _, name := range []string{
		"c.md", "a.md", "B.md", "a b.md", "a/z.md", "a/b.md", "sub/x.md", "sub.md", "é.md",
		"m.png", "Z.png", "a.png", "a/x.png",
	} {
		writeFile(t, dir, name, "x")
	}
	mustMkdir(t, filepath.Join(dir, "a", "deep"))
	mustMkdir(t, filepath.Join(dir, "a.d"))

	got := listVault(t, v)
	wantNotes := []string{"B.md", "a b.md", "a.md", "a/b.md", "a/z.md", "c.md", "sub.md", "sub/x.md", "é.md"}
	if !slices.Equal(entryPaths(got.Notes), wantNotes) {
		t.Errorf("Notes = %v, want %v", entryPaths(got.Notes), wantNotes)
	}
	wantAssets := []string{"Z.png", "a.png", "a/x.png", "m.png"}
	if !slices.Equal(entryPaths(got.Assets), wantAssets) {
		t.Errorf("Assets = %v, want %v", entryPaths(got.Assets), wantAssets)
	}
	if want := []string{"a", "a.d", "a/deep", "sub"}; !slices.Equal(got.Folders, want) {
		t.Errorf("Folders = %v, want %v", got.Folders, want)
	}
}

func TestListNeverFollowsSymlinksOutOfTheVault(t *testing.T) {
	v, dir := openTestVault(t, Options{})
	outside := t.TempDir()
	writeFile(t, outside, "secret.md", "secret")
	writeFile(t, outside, "deep/secret.png", "secret")
	writeFile(t, dir, "a.md", "a")
	mustSymlink(t, outside, filepath.Join(dir, "escape"))
	mustSymlink(t, filepath.Join(outside, "secret.md"), filepath.Join(dir, "secret.md"))
	mustSymlink(t, "missing.md", filepath.Join(dir, "dangling.md"))

	got := listVault(t, v)
	if want := []string{"a.md"}; !slices.Equal(entryPaths(got.Notes), want) {
		t.Errorf("Notes = %v, want %v", entryPaths(got.Notes), want)
	}
	if len(got.Assets) != 0 || len(got.Folders) != 0 {
		t.Errorf("Assets = %v, Folders = %v, want none: symlinks are never listed or followed", got.Assets, got.Folders)
	}
}

func TestListSkipsNonRegularFiles(t *testing.T) {
	v, dir := openTestVault(t, Options{})
	writeFile(t, dir, "real.md", "r")
	for _, name := range []string{"pipe.md", "pipe.png"} {
		if err := syscall.Mkfifo(filepath.Join(dir, name), 0o644); err != nil {
			t.Skipf("cannot create a named pipe here: %v", err)
		}
	}

	got := listVault(t, v)
	if want := []string{"real.md"}; !slices.Equal(entryPaths(got.Notes), want) {
		t.Errorf("Notes = %v, want %v", entryPaths(got.Notes), want)
	}
	if len(got.Assets) != 0 {
		t.Errorf("Assets = %v, want none", entryPaths(got.Assets))
	}
}

func TestListDirectoriesNamedLikeFiles(t *testing.T) {
	v, dir := openTestVault(t, Options{})
	writeFile(t, dir, "weird.md/inner.md", "i")
	writeFile(t, dir, "shots.png/x.png", "i")

	got := listVault(t, v)
	if want := []string{"weird.md/inner.md"}; !slices.Equal(entryPaths(got.Notes), want) {
		t.Errorf("Notes = %v, want %v", entryPaths(got.Notes), want)
	}
	if want := []string{"shots.png/x.png"}; !slices.Equal(entryPaths(got.Assets), want) {
		t.Errorf("Assets = %v, want %v", entryPaths(got.Assets), want)
	}
	if want := []string{"shots.png", "weird.md"}; !slices.Equal(got.Folders, want) {
		t.Errorf("Folders = %v, want %v", got.Folders, want)
	}
}

// createOrSkip creates dir/name with its parents. If the filesystem refuses the
// name (too long, not valid UTF-8, characters it does not allow) the test is
// skipped and says why: such a name cannot show up in a listing either. Any
// other error is a fault in the test.
func createOrSkip(t *testing.T, dir, name string) {
	t.Helper()
	err := tryWriteFile(dir, name, "x")
	if err == nil {
		return
	}
	if errors.Is(err, syscall.ENAMETOOLONG) || errors.Is(err, syscall.EILSEQ) || errors.Is(err, syscall.EINVAL) {
		t.Skipf("this filesystem refuses to create %s: %v", describe(name), errors.Unwrap(err))
	}
	t.Fatal(err)
}

// List never advertises what the API cannot serve: a file or folder whose path
// CleanPath rejects is skipped, and with a folder everything beneath it. A
// sub-case is skipped, and says so, when the filesystem refuses to create the
// name at all (macOS refuses invalid UTF-8, most filesystems refuse components
// over 255 bytes), in which case it cannot show up in a listing either.
func TestListSkipsNamesThatCannotBeOpened(t *testing.T) {
	for _, tc := range []struct {
		what  string
		files []string // created with their parents; none of them may be listed
	}{
		{"backslash in a file name", []string{`a\b.md`, `a\b.png`}},
		{"backslash in a file name in a folder", []string{`sub/a\b.md`}},
		{"backslash in a folder name", []string{`dir\x/inner.md`, `dir\x/pic.png`}},
		{"tab in a file name", []string{"tab\tname.md"}},
		{"newline in a file name", []string{"line\nbreak.md"}},
		{"DEL in a file name", []string{"del\x7fname.md"}},
		{"C1 control character in a file name", []string{"nel\u0085name.md"}},
		{"control character in a folder name", []string{"bell\x07dir/inner.md"}},
		{"invalid UTF-8 in a file name", []string{"bad\xffname.md"}},
		{"invalid UTF-8 in a folder name", []string{"bad\xffdir/inner.md"}},
		{"file name of 256 bytes", []string{strings.Repeat("a", 253) + ".md"}},
		{"folder name of 256 bytes", []string{strings.Repeat("b", 256) + "/inner.md"}},
		{"file name of 259 bytes in 131 characters", []string{strings.Repeat("é", 128) + ".md"}},
	} {
		t.Run(tc.what, func(t *testing.T) {
			v, dir := openTestVault(t, Options{})
			writeFile(t, dir, "ok.md", "ok")
			writeFile(t, dir, "sub/ok.md", "ok")
			writeFile(t, dir, "pic.png", "png")
			before := listVault(t, v)

			for _, name := range tc.files {
				createOrSkip(t, dir, name)
			}

			got := listVault(t, v)
			if want := []string{"ok.md", "sub/ok.md"}; !slices.Equal(entryPaths(got.Notes), want) {
				t.Errorf("Notes = %q, want %q", entryPaths(got.Notes), want)
			}
			if want := []string{"pic.png"}; !slices.Equal(entryPaths(got.Assets), want) {
				t.Errorf("Assets = %q, want %q", entryPaths(got.Assets), want)
			}
			if want := []string{"sub"}; !slices.Equal(got.Folders, want) {
				t.Errorf("Folders = %q, want %q", got.Folders, want)
			}
			if got.Fingerprint != before.Fingerprint {
				t.Error("Fingerprint changed, but it covers only what is listed and nothing listed changed")
			}
		})
	}
}

// The path limit is on the whole path, not on a component: every component
// here is short enough, the path is not.
func TestListSkipsPathsOver512Bytes(t *testing.T) {
	a, b, c := strings.Repeat("a", 200), strings.Repeat("b", 200), strings.Repeat("c", 110)
	atLimit := a + "/" + b + "/" + c // exactly 512 bytes
	if len(atLimit) != 512 {
		t.Fatalf("test fixture: %d bytes, want 512", len(atLimit))
	}
	v, dir := openTestVault(t, Options{})
	for _, name := range []string{
		"ok.md",
		a + "/" + b + "/ok.md", // 407 bytes
		atLimit + "/x.md",      // 517 bytes: a note beneath a folder that is itself fine
		atLimit + "/d/y.md",    // beneath a folder of 514 bytes
		atLimit + "/d/e/p.png", // and an image deeper still
	} {
		createOrSkip(t, dir, name)
	}

	got := listVault(t, v)
	if want := []string{a + "/" + b + "/ok.md", "ok.md"}; !slices.Equal(entryPaths(got.Notes), want) {
		t.Errorf("Notes = %q, want %q", short(entryPaths(got.Notes)), short(want))
	}
	if len(got.Assets) != 0 {
		t.Errorf("Assets = %q, want none", short(entryPaths(got.Assets)))
	}
	if want := []string{a, a + "/" + b, atLimit}; !slices.Equal(got.Folders, want) {
		t.Errorf("Folders = %q, want %q: the folders up to the 512-byte limit", short(got.Folders), short(want))
	}
}

// Skipped folders are not walked at all: a .git folder, or one with a name the
// API cannot serve, may hold thousands of files.
func TestListDoesNotWalkSkippedFolders(t *testing.T) {
	v, dir := openTestVault(t, Options{})
	writeFile(t, dir, "ok.md", "ok")
	for i := range 30 {
		writeFile(t, dir, fmt.Sprintf(".git/objects/%02d.md", i), "x")
		writeFile(t, dir, fmt.Sprintf(`bad\dir/%02d.md`, i), "x")
	}

	ctx := &hookContext{Context: context.Background(), onErr: func(int) error { return nil }}
	got, err := v.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"ok.md"}; !slices.Equal(entryPaths(got.Notes), want) {
		t.Errorf("Notes = %q, want %q", entryPaths(got.Notes), want)
	}
	if len(got.Folders) != 0 {
		t.Errorf("Folders = %q, want none", got.Folders)
	}
	// The vault folder, .git, bad\dir and ok.md are visited; 60 files lie beneath
	// the two skipped folders.
	if ctx.calls > 10 {
		t.Errorf("List visited %d entries, want 4: it walked into a skipped folder", ctx.calls)
	}
}

func TestListEmptyVault(t *testing.T) {
	v, dir := openTestVault(t, Options{})

	got := listVault(t, v)
	if got.Notes == nil || got.Assets == nil || got.Folders == nil {
		t.Errorf("an empty vault must list empty slices, not nil, so they encode as JSON arrays: %#v", got)
	}
	if n := len(got.Notes) + len(got.Assets) + len(got.Folders); n != 0 {
		t.Errorf("an empty vault listed %d entries", n)
	}
	if !fingerprintPattern.MatchString(got.Fingerprint) {
		t.Errorf("Fingerprint = %q, want 64 lowercase hex digits", got.Fingerprint)
	}

	// Hidden content does not make a vault non-empty.
	writeFile(t, dir, ".obsidian/app.json", "{}")
	if again := listVault(t, v); again.Fingerprint != got.Fingerprint || len(again.Folders) != 0 {
		t.Errorf("a hidden folder changed the listing of an empty vault: %#v", again)
	}
}

func TestListFingerprint(t *testing.T) {
	v, dir := openTestVault(t, Options{})
	base := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	writeFile(t, dir, "a.md", "alpha")
	writeFile(t, dir, "sub/b.md", "beta")
	chtimes(t, dir, "a.md", base)
	chtimes(t, dir, "sub/b.md", base)
	at := func(name string) string { return filepath.Join(dir, filepath.FromSlash(name)) }

	original := listVault(t, v).Fingerprint
	if !fingerprintPattern.MatchString(original) {
		t.Fatalf("Fingerprint = %q, want 64 lowercase hex digits", original)
	}
	if again := listVault(t, v).Fingerprint; again != original {
		t.Fatalf("Fingerprint changed between two calls with nothing changed: %s then %s", original, again)
	}

	changes := []struct {
		what          string
		apply, revert func()
	}{
		{"a file's mtime",
			func() { chtimes(t, dir, "a.md", base.Add(time.Hour)) },
			func() { chtimes(t, dir, "a.md", base) }},
		{"a file's size with the mtime kept",
			func() { writeFile(t, dir, "a.md", "alphabet"); chtimes(t, dir, "a.md", base) },
			func() { writeFile(t, dir, "a.md", "alpha"); chtimes(t, dir, "a.md", base) }},
		{"an empty folder",
			func() { mustMkdir(t, at("empty")) },
			func() { os.Remove(at("empty")) }},
		{"a new note",
			func() { writeFile(t, dir, "new.md", "n") },
			func() { os.Remove(at("new.md")) }},
		{"a new image",
			func() { writeFile(t, dir, "pic.png", "p") },
			func() { os.Remove(at("pic.png")) }},
		{"a renamed note",
			func() { os.Rename(at("a.md"), at("renamed.md")) },
			func() { os.Rename(at("renamed.md"), at("a.md")) }},
		{"a removed note",
			func() { os.Remove(at("a.md")) },
			func() { writeFile(t, dir, "a.md", "alpha"); chtimes(t, dir, "a.md", base) }},
	}
	for _, c := range changes {
		c.apply()
		if got := listVault(t, v).Fingerprint; got == original {
			t.Errorf("Fingerprint did not change after %s", c.what)
		}
		c.revert()
		if got := listVault(t, v).Fingerprint; got != original {
			t.Fatalf("Fingerprint after undoing %s = %s, want the original %s", c.what, got, original)
		}
	}

	// Things the listing does not show must not change the fingerprint: Obsidian
	// and sync tools rewrite hidden files constantly, and folder mtimes churn
	// whenever a child is created or removed.
	noise := []struct {
		what  string
		apply func()
	}{
		{"hidden files and folders", func() {
			writeFile(t, dir, ".obsidian/workspace.json", "{}")
			writeFile(t, dir, ".DS_Store", "junk")
			writeFile(t, dir, "sub/.syncthing.b.md.tmp", "tmp")
		}},
		{"files that are neither notes nor images", func() { writeFile(t, dir, "c.txt", "c") }},
		{"symlinks", func() {
			mustSymlink(t, "a.md", at("link.md"))
			mustSymlink(t, "sub", at("linkdir"))
		}},
		{"a folder's own mtime", func() { chtimes(t, dir, "sub", base.Add(48*time.Hour)) }},
	}
	for _, n := range noise {
		n.apply()
		if got := listVault(t, v).Fingerprint; got != original {
			t.Errorf("Fingerprint changed after %s: %s, want %s", n.what, got, original)
		}
	}
}

func TestListHonorsContext(t *testing.T) {
	v, dir := openTestVault(t, Options{})
	for i := range 10 {
		writeFile(t, dir, fmt.Sprintf("n%02d.md", i), "x")
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := v.List(cancelled); !errors.Is(err, context.Canceled) {
		t.Errorf("List(cancelled context) error = %v, want context.Canceled", err)
	}

	expired, stop := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer stop()
	if _, err := v.List(expired); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("List(expired context) error = %v, want context.DeadlineExceeded", err)
	}

	// A cancellation that arrives once the walk is under way is noticed too.
	midWalk := &hookContext{Context: context.Background(), onErr: func(call int) error {
		if call > 4 {
			return context.Canceled
		}
		return nil
	}}
	if _, err := v.List(midWalk); !errors.Is(err, context.Canceled) {
		t.Errorf("List(context cancelled mid-walk) error = %v after %d checks, want context.Canceled", err, midWalk.calls)
	}
}

// Sync tools rename and delete files while the vault is being listed. A folder
// is read in one go, so entries that vanish afterwards must be skipped quietly
// rather than failing the whole listing.
func TestListToleratesEntriesRemovedMidWalk(t *testing.T) {
	v, dir := openTestVault(t, Options{})
	for _, name := range []string{"a.md", "b.md", "c.md", "z.md", "sub/x.md"} {
		writeFile(t, dir, name, name)
	}

	// Root is checked first, then a.md, then b.md: by the third check the root
	// folder has been read, so c.md, sub and z.md vanish before they are visited.
	removed := false
	ctx := &hookContext{Context: context.Background(), onErr: func(call int) error {
		if call == 3 {
			for _, name := range []string{"c.md", "sub", "z.md"} {
				if err := os.RemoveAll(filepath.Join(dir, name)); err != nil {
					t.Error(err)
				}
			}
			removed = true
		}
		return nil
	}}
	got, err := v.List(ctx)
	if err != nil {
		t.Fatalf("List() while entries disappeared = %v", err)
	}
	if !removed {
		t.Fatalf("nothing was removed during the walk (List made %d context checks)", ctx.calls)
	}
	if want := []string{"a.md", "b.md"}; !slices.Equal(entryPaths(got.Notes), want) {
		t.Errorf("Notes = %v, want %v", entryPaths(got.Notes), want)
	}
}

// Everything goes through the opened os.Root. fs.DirEntry.Info is a plain
// lstat of the path, which would follow a vault directory that was moved or
// replaced mid-walk and report files that are not in the vault.
func TestListReadsMetadataThroughTheRoot(t *testing.T) {
	v, dir := openTestVault(t, Options{})
	writeFile(t, dir, "a.md", "aaaa")
	writeFile(t, dir, "b.md", "bbbb")
	moved := dir + ".moved"
	t.Cleanup(func() { os.RemoveAll(moved) })

	// By the third check both entries have been read; b.md is looked at next.
	swapped := false
	ctx := &hookContext{Context: context.Background(), onErr: func(call int) error {
		if call == 3 {
			if err := os.Rename(dir, moved); err != nil {
				t.Error(err)
			}
			writeFile(t, dir, "b.md", "a different, longer file at the same path")
			swapped = true
		}
		return nil
	}}
	got, err := v.List(ctx)
	if err != nil {
		t.Fatalf("List() = %v", err)
	}
	if !swapped {
		t.Fatalf("the vault directory was never swapped (List made %d context checks)", ctx.calls)
	}
	if len(got.Notes) != 2 || got.Notes[1].Path != "b.md" || got.Notes[1].Size != 4 {
		t.Errorf("Notes = %+v, want a.md and b.md with b.md still 4 bytes, as in the opened directory", got.Notes)
	}
}

// Unlike a subfolder, the vault directory itself must be readable: listing it
// as empty would look like an empty vault instead of a broken one.
func TestListFailsWhenVaultDirUnreadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission checks do not apply to root")
	}
	lock := func(t *testing.T, dir string) {
		t.Helper()
		if err := os.Chmod(dir, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(dir, 0o755) })
	}

	t.Run("before the walk", func(t *testing.T) {
		v, dir := openTestVault(t, Options{})
		writeFile(t, dir, "a.md", "a")
		lock(t, dir)
		if got, err := v.List(context.Background()); err == nil {
			t.Errorf("List() = %+v, nil; want an error", got)
		}
	})

	// The permissions change after Ping and before the vault directory is read,
	// so the walk itself has to report it.
	t.Run("during the walk", func(t *testing.T) {
		v, dir := openTestVault(t, Options{})
		writeFile(t, dir, "a.md", "a")
		ctx := &hookContext{Context: context.Background(), onErr: func(call int) error {
			if call == 1 {
				lock(t, dir)
			}
			return nil
		}}
		if got, err := v.List(ctx); err == nil {
			t.Errorf("List() = %+v, nil; want an error", got)
		}
	})
}

// A folder the process may not read (a root-owned lost+found at the top of a
// mounted disk) must not make the whole vault unlistable.
func TestListSkipsUnreadableFolders(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission checks do not apply to root")
	}
	v, dir := openTestVault(t, Options{})
	writeFile(t, dir, "a.md", "a")
	writeFile(t, dir, "locked/secret.md", "s")
	writeFile(t, dir, "z.md", "z")
	locked := filepath.Join(dir, "locked")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })

	got, err := v.List(context.Background())
	if err != nil {
		t.Fatalf("List() with an unreadable folder = %v", err)
	}
	if want := []string{"a.md", "z.md"}; !slices.Equal(entryPaths(got.Notes), want) {
		t.Errorf("Notes = %v, want %v", entryPaths(got.Notes), want)
	}
	if want := []string{"locked"}; !slices.Equal(got.Folders, want) {
		t.Errorf("Folders = %v, want %v", got.Folders, want)
	}
}
