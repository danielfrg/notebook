package vault

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"
)

// epoch is the clock of the tests that fake it; the modification times of notes
// are hours from it.
var epoch = time.Date(2025, 3, 4, 5, 6, 7, 0, time.UTC)

func hours(n int) time.Time { return epoch.Add(time.Duration(n) * time.Hour) }

func hitPaths(hits []Hit) []string {
	paths := make([]string, len(hits))
	for i, h := range hits {
		paths[i] = h.Path
	}
	return paths
}

func mustSearch(t *testing.T, v *Vault, query string, opts SearchOptions) []Hit {
	t.Helper()
	hits, err := v.Search(context.Background(), query, opts)
	if err != nil {
		t.Fatalf("Search(%q) = %v", query, err)
	}
	if hits == nil {
		t.Errorf("Search(%q) returned nil hits, want a slice that encodes as a JSON array", query)
	}
	return hits
}

func mustNotes(t *testing.T, v *Vault, visible Filter) []NoteMeta {
	t.Helper()
	notes, err := v.Notes(context.Background(), visible)
	if err != nil {
		t.Fatalf("Notes() = %v", err)
	}
	if notes == nil {
		t.Error("Notes() returned nil, want a slice that encodes as a JSON array")
	}
	return notes
}

func notePaths(notes []NoteMeta) []string {
	paths := make([]string, len(notes))
	for i, n := range notes {
		paths[i] = n.Path
	}
	return paths
}

func TestSearchAccentsCaseAndTerms(t *testing.T) {
	v, dir := openTestVault(t, Options{})
	writeFile(t, dir, "stoics.md", "Marta Peña habló del estoicismo\n")
	writeFile(t, dir, "plain.md", "La pena y la gloria\n")
	writeFile(t, dir, "multi.md", "alpha line\nbeta line\n")
	writeFile(t, dir, "Filosofía/¿Qué es el Estoicismo?.md", "nada\n")
	const lessons = "Filosofía/¿Qué es el Estoicismo?.md"

	for _, tc := range []struct {
		query string
		want  []string // in byte order of the paths: the order of the hits is not what is tested here
	}{
		// Case and accents do not matter on either side.
		{"pena", []string{"plain.md", "stoics.md"}},
		{"peña", []string{"plain.md", "stoics.md"}},
		{"PEÑA estoicismo", []string{"stoics.md"}},
		{"ESTOICISMO", []string{lessons, "stoics.md"}},
		{"que es", []string{lessons}}, // the file name has "¿Qué es"

		// All terms must match, wherever they are in the note, or in its path.
		{"pena platon", nil},
		{"alpha beta", []string{"multi.md"}},
		{"filosofia nada", []string{lessons}},
		{"mult", []string{"multi.md"}},

		// A quoted phrase is one term: in order, with nothing between.
		{`"habló del"`, []string{"stoics.md"}},
		{`"hablo DEL"`, []string{"stoics.md"}},
		{`"del habló"`, nil},
		{"del habló", []string{"stoics.md"}},
		{`"habló del" estoicismo`, []string{"stoics.md"}},
		{`"habló del" platon`, nil},
		{`"marta peña habló del estoicismo"`, []string{"stoics.md"}},
		{`"habló  del"`, []string{"stoics.md"}},
		{`"habló del`, []string{"stoics.md"}}, // a quote that is never closed runs to the end
	} {
		got := hitPaths(mustSearch(t, v, tc.query, SearchOptions{}))
		slices.Sort(got)
		if !slices.Equal(got, tc.want) {
			t.Errorf("Search(%q) = %q, want %q", tc.query, got, tc.want)
		}
	}

	hits := mustSearch(t, v, "estoicismo", SearchOptions{})
	for _, h := range hits {
		if h.Path == lessons && h.Title != "¿Qué es el Estoicismo?" {
			t.Errorf("Title of %q = %q", h.Path, h.Title)
		}
	}
}

func TestSearchScore(t *testing.T) {
	for _, tc := range []struct {
		what, path, content, query string
		want                       int // -1: not a hit
	}{
		{"file name", "needle.md", "x", "needle", 50},
		{"file name in a folder", "a/needle.md", "x", "needle", 50},
		{"folder", "needle/x.md", "x", "needle", 20},
		{"path across the folder and the name", "a/b.md", "x", "a/b", 20},
		{"folder and name both have it, and the name wins", "needle/needle.md", "x", "needle", 50},
		{"text once", "x.md", "needle", "needle", 1},
		{"text three times", "x.md", "needle needle\nneedle", "needle", 3},
		{"text is capped at 20", "x.md", strings.Repeat("needle ", 30), "needle", 20},
		{"file name and text", "needle.md", "needle needle", "needle", 52},
		{"each term adds its own", "needle.md", "thread thread", "needle thread", 52},
		{"a phrase counts as one term", "x.md", "a b a b a", `"a b"`, 2},
		{"accents in the name", "Peña.md", "x", "pena", 50},
		{"the extension is not part of the name", "x.md", "y", "md", -1},
		{"nothing", "x.md", "y", "needle", -1},
	} {
		v, dir := openTestVault(t, Options{})
		writeFile(t, dir, tc.path, tc.content)
		hits := mustSearch(t, v, tc.query, SearchOptions{})
		switch {
		case tc.want < 0 && len(hits) != 0:
			t.Errorf("%s: Search(%q) found %q, want nothing", tc.what, tc.query, hitPaths(hits))
		case tc.want >= 0 && (len(hits) != 1 || hits[0].Score != tc.want):
			t.Errorf("%s: Search(%q) = %+v, want one hit with score %d", tc.what, tc.query, hits, tc.want)
		}
	}
}

func TestSearchRanking(t *testing.T) {
	v, dir := openTestVault(t, Options{})
	for _, n := range []struct {
		path, content string
		mtime         time.Time
	}{
		{"needle.md", "nothing", hours(0)}, // the oldest, and still the first
		{"dir-needle/x.md", "nothing", hours(1)},
		{"body-many.md", "needle needle needle", hours(-5)},
		{"tie-a.md", "needle", hours(2)}, // among equals the newer comes first, whatever the names
		{"tie-b.md", "needle", hours(3)},
	} {
		writeFile(t, dir, n.path, n.content)
		chtimes(t, dir, n.path, n.mtime)
	}

	hits := mustSearch(t, v, "needle", SearchOptions{})
	want := []string{"needle.md", "dir-needle/x.md", "body-many.md", "tie-b.md", "tie-a.md"}
	if !slices.Equal(hitPaths(hits), want) {
		t.Errorf("Search order = %q, want %q: file name, then path, then text, and the newest first among equals", hitPaths(hits), want)
	}
	if len(hits) != len(want) {
		t.FailNow()
	}
	if hits[0].Title != "needle" || !hits[3].Modified.Equal(hours(3)) {
		t.Errorf("hits[0].Title = %q, hits[3].Modified = %v, want needle and %v", hits[0].Title, hits[3].Modified, hours(3))
	}

	for limit, wantLen := range map[int]int{1: 1, 2: 2, 5: 5, 100: 5} {
		got := mustSearch(t, v, "needle", SearchOptions{Limit: limit})
		if len(got) != wantLen || !slices.Equal(hitPaths(got), want[:wantLen]) {
			t.Errorf("Search with Limit %d = %q, want the first %d: %q", limit, hitPaths(got), wantLen, want[:wantLen])
		}
	}
}

// Limit 0 means 50, and that is only the default: paging through the results
// asks for more.
func TestSearchLimit(t *testing.T) {
	v, dir := openTestVault(t, Options{})
	for i := range 60 {
		writeFile(t, dir, fmt.Sprintf("n%02d.md", i), "needle")
	}
	for limit, want := range map[int]int{0: 50, -1: 50, 7: 7, 60: 60, 500: 60} {
		if got := mustSearch(t, v, "needle", SearchOptions{Limit: limit}); len(got) != want {
			t.Errorf("Search with Limit %d returned %d hits, want %d", limit, len(got), want)
		}
	}
}

func TestSearchSnippets(t *testing.T) {
	advance := setClock(t, epoch)
	v, dir := openTestVault(t, Options{})
	writeFile(t, dir, "a.md", strings.Join([]string{
		"---",                    // 1
		"title: x",               // 2
		"---",                    // 3
		"first needle line",      // 4
		"no match here",          // 5
		"  second NEEDLE line  ", // 6
		"third Needle line\r",    // 7
		"fourth needle line",     // 8
		"fifth needle line",      // 9
	}, "\n"))
	writeFile(t, dir, "only-in-the-name-needle.md", "nothing\n")

	hits := mustSearch(t, v, "needle", SearchOptions{})
	byPath := map[string]Hit{}
	for _, h := range hits {
		byPath[h.Path] = h
	}
	want := []Snippet{{4, "first needle line"}, {6, "second NEEDLE line"}, {7, "third Needle line"}}
	if got := byPath["a.md"].Snippets; !slices.Equal(got, want) {
		t.Errorf("Snippets = %+v, want %+v: three at most, the first lines that match, numbered from 1 in the file, the text as written", got, want)
	}
	if got := byPath["only-in-the-name-needle.md"].Snippets; got == nil || len(got) != 0 {
		t.Errorf("a note that matches only by its name has Snippets %#v, want an empty slice", got)
	}

	// Lines are counted as the editor counts them: LF, CRLF and a bare CR each
	// end one, and a byte order mark is not part of the first line.
	writeFile(t, dir, "breaks.md", utf8BOM+"needle one\r\ntwo needle\rthree\nfour needle\n")
	advance(epoch.Add(3 * time.Second))
	want = []Snippet{{1, "needle one"}, {2, "two needle"}, {4, "four needle"}}
	for _, h := range mustSearch(t, v, "needle", SearchOptions{Limit: 100}) {
		if h.Path == "breaks.md" && !slices.Equal(h.Snippets, want) {
			t.Errorf("Snippets of breaks.md = %+v, want %+v", h.Snippets, want)
		}
	}
}

func TestSearchSnippetsAreTrimmedAroundTheMatch(t *testing.T) {
	const side = 250
	for _, tc := range []struct {
		what   string
		before string // what comes before " Needle " in the line, and after
		after  string
	}{
		{"in the middle", strings.Repeat("ñ", side), strings.Repeat("é", side)},
		{"near the start", "ñññ", strings.Repeat("é", 2*side)},
		{"near the end", strings.Repeat("ñ", 2*side), "ééé"},
		// Accents as marks of their own: the line has more characters than the
		// text the search matched on, and the match is far from where it looks.
		{"after marks", strings.Repeat("e"+combiningAcute, 150), strings.Repeat("a", 300)},
	} {
		v, dir := openTestVault(t, Options{})
		line := tc.before + " Needle " + tc.after
		writeFile(t, dir, "long.md", "short line\n"+line+"\n")
		hits := mustSearch(t, v, "needle", SearchOptions{})
		if len(hits) != 1 || len(hits[0].Snippets) != 1 {
			t.Fatalf("%s: hits = %+v, want one hit with one snippet", tc.what, hits)
		}
		s := hits[0].Snippets[0]
		if s.Line != 2 {
			t.Errorf("%s: Line = %d, want 2", tc.what, s.Line)
		}
		if n := utf8.RuneCountInString(s.Text); n != 200 {
			t.Errorf("%s: the text has %d runes, want 200", tc.what, n)
		}
		if !strings.Contains(s.Text, "Needle") || !strings.Contains(line, s.Text) {
			t.Errorf("%s: text %q should be the match and what is around it, as written in the line", tc.what, s.Text)
		}
	}

	// In the middle it is centred.
	v, dir := openTestVault(t, Options{})
	writeFile(t, dir, "long.md", strings.Repeat("ñ", side)+" Needle "+strings.Repeat("é", side)+"\n")
	hits := mustSearch(t, v, "needle", SearchOptions{})
	if len(hits) != 1 || len(hits[0].Snippets) != 1 {
		t.Fatalf("hits = %+v, want one hit with one snippet", hits)
	}
	text := hits[0].Snippets[0].Text
	if before := utf8.RuneCountInString(text[:strings.Index(text, "Needle")]); before < 90 || before > 105 {
		t.Errorf("%d characters come before the match, want about 97 of the 200", before)
	}
}

func TestSearchVisibleFilter(t *testing.T) {
	v, dir := openTestVault(t, Options{})
	for _, name := range []string{
		"transcripts/needle.md", // the best match of all, and hidden
		"transcripts/deep/call.md",
		"transcripts2/b.md",
		"notes/a.md",
		"notes/secret.private.md",
	} {
		writeFile(t, dir, name, "needle in "+name)
	}
	visible, err := ExcludeGlobs([]string{"transcripts", "**/*.private.md"})
	if err != nil {
		t.Fatal(err)
	}

	if all := mustSearch(t, v, "needle", SearchOptions{}); len(all) != 5 {
		t.Errorf("Search without a filter = %q, want all 5 notes", hitPaths(all))
	}
	hits := mustSearch(t, v, "needle", SearchOptions{Visible: visible})
	got := hitPaths(hits)
	slices.Sort(got)
	if want := []string{"notes/a.md", "transcripts2/b.md"}; !slices.Equal(got, want) {
		t.Errorf("Search with Visible = %q, want %q", got, want)
	}
	// A note that is hidden must not use up the limit either.
	hits = mustSearch(t, v, "needle", SearchOptions{Visible: visible, Limit: 1})
	if len(hits) != 1 || strings.HasPrefix(hits[0].Path, "transcripts/") || strings.HasSuffix(hits[0].Path, ".private.md") {
		t.Errorf("Search with Visible and Limit 1 = %q, want the best of the notes that are shown", hitPaths(hits))
	}
	// And a note is not found by what only its hidden path says.
	if hits := mustSearch(t, v, "call", SearchOptions{Visible: visible}); len(hits) != 0 {
		t.Errorf("Search(call) with Visible found %q, want nothing", hitPaths(hits))
	}

	notes := notePaths(mustNotes(t, v, visible))
	slices.Sort(notes)
	if want := []string{"notes/a.md", "transcripts2/b.md"}; !slices.Equal(notes, want) {
		t.Errorf("Notes with a filter = %q, want %q", notes, want)
	}
	if all := mustNotes(t, v, nil); len(all) != 5 {
		t.Errorf("Notes without a filter = %q, want all 5", notePaths(all))
	}
}

// The snapshot is refreshed by stat, at most every two seconds.
func TestSearchRefreshesOnChange(t *testing.T) {
	advance := setClock(t, epoch)
	v, dir := openTestVault(t, Options{})
	writeFile(t, dir, "a.md", "alpha")
	writeFile(t, dir, "b.md", "beta")
	writeFile(t, dir, "c.md", "gamma")
	found := func(query string) []string {
		paths := hitPaths(mustSearch(t, v, query, SearchOptions{}))
		slices.Sort(paths)
		return paths
	}
	check := func(what string, wants map[string][]string) {
		t.Helper()
		for query, want := range wants {
			if got := found(query); !slices.Equal(got, want) {
				t.Errorf("%s: Search(%q) = %q, want %q", what, query, got, want)
			}
		}
	}
	check("first", map[string][]string{"alpha": {"a.md"}, "beta": {"b.md"}, "omega": nil})

	// Changes made while the snapshot is young are not seen yet, until two
	// seconds after it was taken.
	writeFile(t, dir, "a.md", "alpha omega")
	writeFile(t, dir, "d.md", "alpha new")
	if err := os.Remove(filepath.Join(dir, "b.md")); err != nil {
		t.Fatal(err)
	}
	advance(epoch.Add(2*time.Second - time.Nanosecond))
	check("just under two seconds", map[string][]string{"omega": nil, "alpha": {"a.md"}, "beta": {"b.md"}})
	advance(epoch.Add(2 * time.Second))
	check("two seconds", map[string][]string{"omega": {"a.md"}, "alpha": {"a.md", "d.md"}, "beta": nil})

	// A note that is moved is found at its new path.
	if err := os.Rename(filepath.Join(dir, "a.md"), filepath.Join(dir, "z.md")); err != nil {
		t.Fatal(err)
	}
	advance(epoch.Add(5 * time.Second))
	check("renamed", map[string][]string{"omega": {"z.md"}})

	// A note whose size and modification time have not changed is not read
	// again, whatever it holds now. That is what makes a refresh cheap.
	mtime := fileInfo(t, dir, "c.md").ModTime()
	writeFile(t, dir, "c.md", "delta") // the same 5 bytes
	chtimes(t, dir, "c.md", mtime)
	advance(epoch.Add(8 * time.Second))
	check("same size and time", map[string][]string{"gamma": {"c.md"}, "delta": nil})
	chtimes(t, dir, "c.md", mtime.Add(time.Second))
	advance(epoch.Add(11 * time.Second))
	check("same size, new time", map[string][]string{"gamma": nil, "delta": {"c.md"}})

	// Either one is enough to see a change: the size, with the time kept...
	mtime = fileInfo(t, dir, "c.md").ModTime()
	writeFile(t, dir, "c.md", "epsilon") // 7 bytes, not 5
	chtimes(t, dir, "c.md", mtime)
	advance(epoch.Add(14 * time.Second))
	check("new size, same time", map[string][]string{"delta": nil, "epsilon": {"c.md"}})

	// ...and a clock that was set back does not make the snapshot young for as
	// long as it takes to catch up.
	writeFile(t, dir, "e.md", "zeta")
	advance(epoch.Add(-time.Hour))
	check("clock set back", map[string][]string{"zeta": {"e.md"}})
}

func TestSearchSkipsUnreadableNotes(t *testing.T) {
	v, dir := openTestVault(t, Options{})
	writeFile(t, dir, "ok.md", "needle ok")
	writeFile(t, dir, "big.md", "needle "+strings.Repeat("x", MaxNoteBytes))
	writeFile(t, dir, "binary.md", "needle \xff\xfe bytes")
	// The largest note there can be is still a note.
	writeFile(t, dir, "limit.md", strings.Repeat("x", MaxNoteBytes-len(" needle"))+" needle")
	if size := fileInfo(t, dir, "limit.md").Size(); size != MaxNoteBytes {
		t.Fatalf("fixture: limit.md has %d bytes, want %d", size, MaxNoteBytes)
	}

	advance := setClock(t, epoch)
	got := hitPaths(mustSearch(t, v, "needle", SearchOptions{}))
	slices.Sort(got)
	if want := []string{"limit.md", "ok.md"}; !slices.Equal(got, want) {
		t.Errorf("Search = %q, want %q: a note over 2 MiB and one that is not UTF-8 are skipped", got, want)
	}
	notes := notePaths(mustNotes(t, v, nil))
	slices.Sort(notes)
	if want := []string{"limit.md", "ok.md"}; !slices.Equal(notes, want) {
		t.Errorf("Notes = %q, want %q: only notes that can be opened", notes, want)
	}

	// Fixing a note brings it back.
	writeFile(t, dir, "binary.md", "needle fixed")
	advance(epoch.Add(3 * time.Second))
	if got := hitPaths(mustSearch(t, v, "fixed", SearchOptions{})); !slices.Equal(got, []string{"binary.md"}) {
		t.Errorf("Search(fixed) = %q, want binary.md", got)
	}
}

// A note that cannot be read just now is not remembered as unreadable: it
// counts again as soon as it can be, though nothing about it changed that a
// stat shows.
func TestSearchRetriesNotesThatCouldNotBeRead(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission checks do not apply to root")
	}
	advance := setClock(t, epoch)
	v, dir := openTestVault(t, Options{})
	writeFile(t, dir, "ok.md", "needle ok")
	writeFile(t, dir, "locked.md", "needle locked")
	if err := os.Chmod(filepath.Join(dir, "locked.md"), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(filepath.Join(dir, "locked.md"), 0o644) })

	if got := hitPaths(mustSearch(t, v, "needle", SearchOptions{})); !slices.Equal(got, []string{"ok.md"}) {
		t.Fatalf("Search = %q, want only ok.md: locked.md cannot be read", got)
	}
	if err := os.Chmod(filepath.Join(dir, "locked.md"), 0o644); err != nil {
		t.Fatal(err)
	}
	advance(epoch.Add(3 * time.Second))
	got := hitPaths(mustSearch(t, v, "needle", SearchOptions{}))
	slices.Sort(got)
	if want := []string{"locked.md", "ok.md"}; !slices.Equal(got, want) {
		t.Errorf("Search after the permissions were fixed = %q, want %q", got, want)
	}
}

// Search sees the notes that List lists, and nothing else.
func TestSearchNeverSeesWhatListSkips(t *testing.T) {
	v, dir := openTestVault(t, Options{})
	outside := t.TempDir()
	writeFile(t, outside, "secret.md", "needle outside")
	writeFile(t, dir, "a.md", "needle")
	writeFile(t, dir, ".obsidian/x.md", "needle")
	writeFile(t, dir, ".trash/y.md", "needle")
	writeFile(t, dir, "sub/.hidden.md", "needle")
	writeFile(t, dir, "c.txt", "needle")
	writeFile(t, dir, "pic.png", "needle")
	writeFile(t, dir, "real/inner.md", "needle")
	mustSymlink(t, "a.md", filepath.Join(dir, "link.md"))
	mustSymlink(t, "real", filepath.Join(dir, "linked-folder"))
	mustSymlink(t, filepath.Join(outside, "secret.md"), filepath.Join(dir, "escape.md"))
	mustSymlink(t, outside, filepath.Join(dir, "escape-folder"))

	got := hitPaths(mustSearch(t, v, "needle", SearchOptions{}))
	slices.Sort(got)
	if want := []string{"a.md", "real/inner.md"}; !slices.Equal(got, want) {
		t.Errorf("Search = %q, want %q", got, want)
	}
	notes := notePaths(mustNotes(t, v, nil))
	slices.Sort(notes)
	if want := []string{"a.md", "real/inner.md"}; !slices.Equal(notes, want) {
		t.Errorf("Notes = %q, want %q", notes, want)
	}
}

func TestSearchEmptyQuery(t *testing.T) {
	v, dir := openTestVault(t, Options{})
	writeFile(t, dir, "a.md", "text")
	for _, query := range []string{"", "   ", "\t\n", `""`, `" "`, ` "" `} {
		hits, err := v.Search(context.Background(), query, SearchOptions{})
		if !errors.Is(err, ErrEmptyQuery) || hits != nil {
			t.Errorf("Search(%q) = %v, %v, want ErrEmptyQuery", query, hits, err)
		}
	}
}

func TestSearchAndNotesHonorContext(t *testing.T) {
	advance := setClock(t, epoch)
	v, dir := openTestVault(t, Options{})
	writeFile(t, dir, "a.md", "needle")

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	// Before there is a snapshot, and with a snapshot that is up to date: a
	// request that is no longer wanted is not answered.
	for _, when := range []string{"before the first search", "with a snapshot"} {
		if _, err := v.Search(cancelled, "needle", SearchOptions{}); !errors.Is(err, context.Canceled) {
			t.Errorf("Search(cancelled context) %s = %v, want context.Canceled", when, err)
		}
		if _, err := v.Notes(cancelled, nil); !errors.Is(err, context.Canceled) {
			t.Errorf("Notes(cancelled context) %s = %v, want context.Canceled", when, err)
		}
		// A search that was cut short does not leave a half-made snapshot behind.
		if got := hitPaths(mustSearch(t, v, "needle", SearchOptions{})); !slices.Equal(got, []string{"a.md"}) {
			t.Errorf("Search after a cancelled one %s = %q, want a.md", when, got)
		}
	}

	// Nor does a refresh that is cut short in the middle of the walk.
	writeFile(t, dir, "b.md", "needle")
	advance(epoch.Add(5 * time.Second))
	midWalk := &hookContext{Context: context.Background(), onErr: func(call int) error {
		if call > 2 {
			return context.Canceled
		}
		return nil
	}}
	if _, err := v.Search(midWalk, "needle", SearchOptions{}); !errors.Is(err, context.Canceled) {
		t.Errorf("Search(context cancelled mid-walk) = %v, want context.Canceled", err)
	}
	got := hitPaths(mustSearch(t, v, "needle", SearchOptions{}))
	slices.Sort(got)
	if want := []string{"a.md", "b.md"}; !slices.Equal(got, want) {
		t.Errorf("Search after the refresh that was cancelled = %q, want %q", got, want)
	}
}

// What the snapshot knows is not served for a vault that is gone: a refresh
// that cannot walk it fails, as List does.
func TestSearchFailsWhenVaultGone(t *testing.T) {
	advance := setClock(t, epoch)
	v, dir := openTestVault(t, Options{})
	writeFile(t, dir, "a.md", "needle")
	mustSearch(t, v, "needle", SearchOptions{})

	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	advance(epoch.Add(3 * time.Second))
	if hits, err := v.Search(context.Background(), "needle", SearchOptions{}); err == nil {
		t.Errorf("Search after the vault directory was removed = %q, want an error", hitPaths(hits))
	}
	if notes, err := v.Notes(context.Background(), nil); err == nil {
		t.Errorf("Notes after the vault directory was removed = %q, want an error", notePaths(notes))
	}
}

func TestNotesMeta(t *testing.T) {
	v, dir := openTestVault(t, Options{})
	notes := []struct {
		path, content string
		mtime         time.Time
		title         string
		tags          []string
	}{
		{"a.md", "---\ntags: [x]\n---\nbody", hours(0), "a", []string{"x"}},
		{"sub/B Note.md", "#inline and #x\n", hours(2), "B Note", []string{"inline", "x"}},
		{"sub/deeper/c.md", "no tags", hours(1), "c", []string{}},
		{"z-tie.md", "tie", hours(1), "z-tie", []string{}}, // the same time as c.md: by path
	}
	for _, n := range notes {
		writeFile(t, dir, n.path, n.content)
		chtimes(t, dir, n.path, n.mtime)
	}
	writeFile(t, dir, "pic.png", "not a note")
	writeFile(t, dir, ".obsidian/hidden.md", "hidden")

	got := mustNotes(t, v, nil)
	order := []int{1, 2, 3, 0} // B Note, c, z-tie, a
	if len(got) != len(notes) {
		t.Fatalf("Notes = %q, want %d notes", notePaths(got), len(notes))
	}
	for i, idx := range order {
		want := notes[idx]
		g := got[i]
		if g.Path != want.path || g.Title != want.title || g.Size != int64(len(want.content)) ||
			!g.Modified.Equal(want.mtime) || !slices.Equal(g.Tags, want.tags) || g.Tags == nil {
			t.Errorf("Notes[%d] = %+v, want %s %q size %d, modified %v, tags %q (newest first)",
				i, g, want.path, want.title, len(want.content), want.mtime, want.tags)
		}
	}

	// What is returned is the caller's to change.
	got[0].Tags[0] = "changed"
	if again := mustNotes(t, v, nil); again[0].Tags[0] != "inline" {
		t.Errorf("a change to the Tags of a result showed up in the next one: %q", again[0].Tags)
	}
}

func TestNotesOfAnEmptyVault(t *testing.T) {
	v, _ := openTestVault(t, Options{})
	if got := mustNotes(t, v, nil); len(got) != 0 {
		t.Errorf("Notes of an empty vault = %+v", got)
	}
	if got := mustSearch(t, v, "anything", SearchOptions{}); len(got) != 0 {
		t.Errorf("Search in an empty vault = %+v", got)
	}
}

// Searches and refreshes run together under the race detector, as requests do.
func TestSearchIsSafeForConcurrentUse(t *testing.T) {
	var seconds atomic.Int64
	old := now
	t.Cleanup(func() { now = old })
	now = func() time.Time { return epoch.Add(time.Duration(seconds.Load()) * time.Second) }

	v, dir := openTestVault(t, Options{})
	for i := range 20 {
		writeFile(t, dir, fmt.Sprintf("n%02d.md", i), "needle")
	}
	var wg sync.WaitGroup
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 40 {
				if _, err := v.Search(context.Background(), "needle", SearchOptions{}); err != nil {
					t.Error(err)
					return
				}
				if _, err := v.Notes(context.Background(), nil); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := range 40 {
			if err := tryWriteFile(dir, fmt.Sprintf("n%02d.md", i%20), fmt.Sprintf("needle edit %d", i)); err != nil {
				t.Error(err) // not Fatal: this is not the goroutine of the test
				return
			}
			seconds.Add(3)
		}
	}()
	wg.Wait()

	seconds.Add(3)
	if got := mustSearch(t, v, "needle", SearchOptions{Limit: 100}); len(got) != 20 {
		t.Errorf("Search after the changes found %d notes, want 20", len(got))
	}
}

// largeNote is a note of about 1 KiB the way notes are in a vault: frontmatter
// with tags, a heading, and paragraphs wrapped at 72 columns, some of it with
// accents. Every 20th one says "needle".
func largeNote(i int, rng *rand.Rand) string {
	words := strings.Fields("the of and to in a is that for it as was with be by on not this are or from at which " +
		"but have an had they you were their one all we can her has there been if more when will would who so " +
		"habló estoicismo reflexión peña también según más años día niño señal canción corazón")
	var b strings.Builder
	fmt.Fprintf(&b, "---\ntitle: Note %04d\ntags: [t%d, t%d]\ncreated: 2025-01-%02d\n---\n# Note uniq%04d\n\n", i, i%7, i%13, i%28+1, i)
	for b.Len() < 1000 {
		var line strings.Builder
		for line.Len() < 72 {
			line.WriteString(words[rng.IntN(len(words))])
			line.WriteByte(' ')
		}
		b.WriteString(strings.TrimSpace(line.String()))
		b.WriteByte('\n')
	}
	if i%20 == 0 {
		b.WriteString("needle\n")
	}
	return b.String()
}

// Review focus 5: 5,000 notes of 1 KiB, in folders three deep. The first search
// reads them all, and the ones after it must not.
//
// The time here is the real one; the clock of the vault is faked. One thing is
// measured with care. A Mac that runs security software can stall every file
// open for a second or two, once, soon after 5,000 files have been written
// (plain os.ReadFile stalls the same way, as does reading through an os.Root;
// where it happens varies from run to run, from the first pass over the files
// to the fourth). That says nothing about the code, and a single timed search
// that happens to land in it would fail a bound it should meet. So the first
// search is timed on a fresh Vault, whose snapshot is empty, as often as it
// takes to meet the bound, for up to 20 seconds: code that is too slow is too
// slow every time, and fails them all. The attempts are logged.
func TestSearchLargeVault(t *testing.T) {
	if testing.Short() {
		t.Skip("writes 5,000 files")
	}
	advance := setClock(t, epoch)
	dir := t.TempDir()
	rng := rand.New(rand.NewPCG(1, 2))
	const notes = 5000
	for i := range notes {
		writeFile(t, dir, fmt.Sprintf("area-%02d/topic-%02d/note-%04d.md", i/500, (i/50)%10, i), largeNote(i, rng))
	}

	timed := func(v *Vault, query string) ([]Hit, time.Duration) {
		start := time.Now()
		hits := mustSearch(t, v, query, SearchOptions{Limit: 1000})
		return hits, time.Since(start)
	}

	const firstBound, laterBound = 2 * time.Second, 500 * time.Millisecond
	var v *Vault // the one whose first search was the fastest, which has its snapshot
	var first time.Duration
	var took []time.Duration
	for began := time.Now(); first == 0 || (first > firstBound && time.Since(began) < 20*time.Second); {
		fresh, err := Open("notes", dir, Options{})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { fresh.Close() })
		hits, d := timed(fresh, "needle")
		if len(hits) != notes/20 {
			t.Fatalf("first search found %d notes, want %d", len(hits), notes/20)
		}
		took = append(took, d.Round(time.Millisecond))
		if v == nil || d < first {
			v, first = fresh, d
		}
	}

	// The second search is served from the snapshot.
	var second time.Duration
	for range 3 {
		hits, d := timed(v, "needle")
		if len(hits) != notes/20 {
			t.Fatalf("second search found %d notes, want %d", len(hits), notes/20)
		}
		if second == 0 || d < second {
			second = d
		}
	}

	// Once the snapshot is old enough it is refreshed: every note is looked at,
	// and none is read again.
	var refresh time.Duration
	for attempt := 1; attempt <= 3 && (refresh == 0 || refresh > laterBound); attempt++ {
		advance(epoch.Add(time.Duration(3*attempt) * time.Second))
		hits, d := timed(v, "UNIQ0042")
		if want := "area-00/topic-00/note-0042.md"; len(hits) != 1 || hits[0].Path != want {
			t.Fatalf("search for the one note = %+v, want %s", hits, want)
		}
		if refresh == 0 || d < refresh {
			refresh = d
		}
	}

	t.Logf("5,000 notes: first search %v (the attempts took %v), second %v, search that refreshes %v", first, took, second, refresh)
	if first > firstBound {
		t.Errorf("first search took %v at best, want under %v", first, firstBound)
	}
	if second > laterBound {
		t.Errorf("second search took %v at best, want under %v", second, laterBound)
	}
	if refresh > laterBound {
		t.Errorf("search that refreshes took %v at best, want under %v", refresh, laterBound)
	}
}
