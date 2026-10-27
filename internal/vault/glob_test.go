package vault

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

func TestMatchGlob(t *testing.T) {
	for _, tc := range []struct {
		pattern, path string
		want          bool
	}{
		// A pattern matches a path, or any folder above it, from the top of the vault.
		{"transcripts", "transcripts", true},
		{"transcripts", "transcripts/a.md", true},
		{"transcripts", "transcripts/deep/er/a.md", true},
		{"transcripts", "transcripts2/a.md", false},
		{"transcripts", "mytranscripts/a.md", false},
		{"transcripts", "notes/transcripts/a.md", false},
		{"a/b", "a/b/c.md", true},
		{"a/b", "a/c/b.md", false},
		{"a/b", "a.md", false},
		{"a/b/c.md", "a/b", false}, // a folder above the pattern is not matched

		// * stays inside one name.
		{"*.md", "a.md", true},
		{"*.md", "dir/a.md", false},
		{"a/*/c.md", "a/b/c.md", true},
		{"a/*/c.md", "a/c.md", false},
		{"a/*/c.md", "a/b/d/c.md", false},
		{"pre*", "prefix/x.md", true},
		{"pre*", "pre/x.md", true},
		{"pre*", "xpre/x.md", false},
		{"*fix*", "prefixes.md", true},
		{"*", "anything.md", true},

		// ? is one character, not one byte.
		{"?", "a", true},
		{"?", "ab", false},
		{"?", "é", true},
		{"a?c.md", "abc.md", true},
		{"a?c.md", "ac.md", false},
		{"a?c.md", "aéc.md", true},

		// ** is any number of folders, none included, and only as a whole name.
		{"**/*.private.md", "x.private.md", true},
		{"**/*.private.md", "a/b/x.private.md", true},
		{"**/*.private.md", "a/b/x.md", false},
		{"**/*.private.md", "x.private.md.bak", false},
		{"Private/**", "Private", true},
		{"Private/**", "Private/a.md", true},
		{"Private/**", "Private/a/b.md", true},
		{"Private/**", "private/a.md", true}, // case does not matter, see below
		{"Private/**", "PrivateNotes/a.md", false},
		{"a/**/z.md", "a/z.md", true},
		{"a/**/z.md", "a/b/c/z.md", true},
		{"a/**/z.md", "a/b/c/y.md", false},
		{"**/**/x", "p/q/x", true},
		{"**", "anything/at/all.md", true},
		{"a**b", "axxb/c.md", true}, // inside a name it is just *
		{"a**b", "a/b/c.md", false},

		// Only *, ? and ** are special, so the names notes have stay matchable.
		{"[ab]", "a", false},
		{"[ab]", "[ab]/x.md", true},
		{"a,b [c] 100% #1.md", "a,b [c] 100% #1.md", true},

		// An accent is one character however the filesystem spelled it: a folder
		// that came from a Mac may be decomposed, and an exclude must still hit it.
		{"Reflexión", "Reflexio" + combiningAcute + "n/a.md", true},
		{"Reflexio" + combiningAcute + "n", "Reflexión/a.md", true},
		{"Reflexi?n", "Reflexio" + combiningAcute + "n/a.md", true},
		{"Reflexión", "Reflexion/a.md", false}, // but an accent is a letter of its own

		// Case does not matter: a filesystem may not tell Transcripts from
		// transcripts (macOS and Windows do not, by default), and an exclude that
		// hid nothing there would show what it was meant to hide. Where the
		// filesystem does tell them apart, hiding both is the safe way to be wrong.
		// What it does not tell apart is more than lower and upper case, see the
		// full case folding below.
		{"transcripts", "TRANSCRIPTS/a.md", true},
		{"TRANSCRIPTS", "transcripts/a.md", true},
		{"transcripts", "Transcripts", true},
		{"**/*.Private.md", "a/X.PRIVATE.MD", true},
		{"a?c.md", "AÉC.md", true},
		{"ÉCOLE", "école/a.md", true}, // letters that are not ASCII too
		{"Reflexión", "REFLEXIÓN/a.md", true},
		{"Reflexio" + combiningAcute + "n", "REFLEXIÓN/a.md", true}, // together with the other spelling of an accent
		{"transcripts", "transcripts2/A.md", false},

		// The case that a filesystem ignores is the full Unicode case folding, and not
		// only lower-casing: APFS tells these spellings apart as little as it does
		// Transcripts from transcripts. An exclude for one of them has to hide the
		// others, or the folder it is for opens under another spelling.
		{"transcripts", "tranſcripts/a.md", true}, // the long s is an s
		{"tranſcripts", "transcripts/a.md", true},
		{"TRANſCRIPTS", "Transcripts/a.md", true},
		{"σοφία", "ςοφία/a.md", true}, // the final sigma is a sigma
		{"ςοφία", "σοφία/a.md", true},
		{"σοφία", "ΣΟΦΊΑ/a.md", true},
		{"ΣΟΦΊΑ", "ςοφία/a.md", true},
		{"strasse", "straße/a.md", true}, // ß is ss
		{"straße", "strasse/a.md", true},
		{"STRASSE", "Straße/a.md", true},
		{"strasse", "straẞe/a.md", true}, // and so is the capital ẞ
		{"file", "ﬁle/a.md", true},       // the ligature ﬁ is fi
		{"ﬁle", "FILE/a.md", true},
		{"offline", "oﬄine/a.md", true},                         // ﬄ is ffl
		{"Μ", "µ/a.md", true},                                   // the micro sign is a mu
		{"i" + combiningDot + "stanbul", "İstanbul/a.md", true}, // İ is an i with a dot above
		{"istanbul", "İstanbul/a.md", false},                    // which is not an i
		{"Ꭰ", "ꭰ/a.md", true},                                   // Cherokee has its two cases, which Unicode folds to one
		{"ꭰ", "Ꭰ/a.md", true},
		{cherokeeCapitals, cherokeeSmall + "/a.md", true},
		{cherokeeSmall, cherokeeCapitals + "/a.md", true},

		// A folded spelling is a whole name like any other, and other names stay shown.
		{"file", "files/a.md", false},
		{"file", "ﬁles/a.md", false},
		{"strasse", "straßen/a.md", false},
		{"transcripts", "tranſcripts2/a.md", false},
		{"σοφία", "σοφίας/a.md", false},

		// ? is one character of the folded name, and ß is two of them.
		{"stra??e", "straße/a.md", true},
		{"stra?e", "straße/a.md", false},
		{"stra?e", "strase/a.md", true},
		{"*ss*", "straße/a.md", true},
		{"*ß*", "strasse/a.md", true},
	} {
		if got := MatchGlob(tc.pattern, tc.path); got != tc.want {
			t.Errorf("MatchGlob(%q, %q) = %v, want %v", tc.pattern, tc.path, got, tc.want)
		}
	}
}

// What an exclude pattern may be is decided in one place, NormalizeGlob, and
// ExcludeGlobs uses it, so that a pattern accepted by one of them cannot be
// rejected by the other: such a pattern would hide nothing, without a word, and
// a recorded conversation would reach third-party models. cmd/notebook reads the
// patterns of the settings with NormalizeGlob, and its TestParseExcludes covers
// the same ones.
var (
	// Accepted: the form that is matched, which is the pattern with white space
	// around it and one trailing "/" trimmed and nothing else rewritten, and a
	// path that the pattern hides.
	acceptedExcludes = []struct{ pattern, canonical, hides string }{
		{"transcripts", "transcripts", "transcripts/a.md"},
		{"transcripts/", "transcripts", "transcripts/a.md"},
		{" transcripts/ ", "transcripts", "transcripts/a.md"},
		{"Private/**", "Private/**", "Private/a.md"},
		{"Private/**/", "Private/**", "Private/a.md"},
		{"**/*.private.md", "**/*.private.md", "a/b.private.md"},
		{"a/*/c.md", "a/*/c.md", "a/b/c.md"},
		{"a b/c?", "a b/c?", "a b/cd"},
		{"Q1..Q2", "Q1..Q2", "Q1..Q2/x.md"}, // dots inside a name are not "." or ".." segments
		{"..data", "..data", "..data/x.md"},
		{".obsidian", ".obsidian", ".obsidian/x.md"},
	}
	rejectedExcludes = []string{
		"",              // empty
		" ",             // empty once white space is trimmed
		"/",             // nothing but a slash
		"/abs",          // absolute
		"/abs/",         // absolute with a trailing slash
		"../x",          // escapes the vault
		"../up",         // escapes the vault
		"a/../b",        // parent segment in the middle
		"..",            // parent segment alone
		"a/..",          // parent segment at the end
		"./transcripts", // "." segment
		"a/./b",         // "." segment in the middle
		".",             // "." segment alone
		"a/.",           // "." segment at the end
		"a//b",          // empty segment
		"a//",           // empty segment left after one trailing slash is trimmed
		`a\b`,           // backslash
		`a\`,            // trailing backslash
	}
)

func TestNormalizeGlob(t *testing.T) {
	for _, tc := range acceptedExcludes {
		got, err := NormalizeGlob(tc.pattern)
		if err != nil || got != tc.canonical {
			t.Errorf("NormalizeGlob(%q) = %q, %v, want %q", tc.pattern, got, err, tc.canonical)
		}
		// What comes out is a pattern in its own form, so normalizing twice is
		// the same as once: a settings parser may store it and hand it back.
		if again, err := NormalizeGlob(got); err != nil || again != got {
			t.Errorf("NormalizeGlob(%q) = %q, %v, want it unchanged", got, again, err)
		}
	}
	for _, pattern := range rejectedExcludes {
		got, err := NormalizeGlob(pattern)
		if err == nil || got != "" {
			t.Errorf("NormalizeGlob(%q) = %q, %v, want an error", pattern, got, err)
		} else if !strings.Contains(err.Error(), fmt.Sprintf("%q", pattern)) {
			t.Errorf("NormalizeGlob(%q) = %q, want an error that names the pattern", pattern, err)
		}
	}
}

// ExcludeGlobs has no rule of its own: what NormalizeGlob accepts is matched in
// the form it returns, and one pattern it rejects fails them all, because
// nothing may start up half excluded.
func TestExcludeGlobsUsesNormalizeGlob(t *testing.T) {
	for _, tc := range acceptedExcludes {
		visible, err := ExcludeGlobs([]string{tc.pattern})
		if err != nil {
			t.Errorf("ExcludeGlobs(%q) = %v, want it accepted", tc.pattern, err)
		} else if visible(tc.hides) {
			t.Errorf("ExcludeGlobs(%q) shows %q, want it hidden", tc.pattern, tc.hides)
		}
	}
	for _, pattern := range []string{"./oops", " ", `a\b`} {
		visible, err := ExcludeGlobs([]string{"transcripts", pattern})
		if err == nil || visible != nil {
			t.Errorf("ExcludeGlobs with %q among good patterns = %v, %v, want nil and an error", pattern, visible != nil, err)
		}
	}
}

func TestExcludeGlobs(t *testing.T) {
	visible, err := ExcludeGlobs([]string{"transcripts", "**/*.private.md", "Private/**", "a/*/c.md", "x?", "Done/"})
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]bool{
		"transcripts":            false,
		"transcripts/a.md":       false,
		"transcripts/deep/a.md":  false,
		"transcripts2/a.md":      true,
		"notes/transcripts/a.md": true,
		"TRANSCRIPTS/a.md":       false, // case does not matter
		"Transcripts/deep/a.md":  false,
		"Transcripts2/a.md":      true,
		"x.private.md":           false,
		"deep/er/x.private.md":   false,
		"Deep/X.Private.MD":      false,
		"x.private.mdx":          true,
		"Private":                false,
		"Private/a.md":           false,
		"private/a.md":           false,
		"PRIVATE/a.md":           false,
		"a/b/c.md":               false,
		"a/c.md":                 true,
		"xy/z.md":                false,
		"XY/z.md":                false,
		"x/z.md":                 true,
		"Done":                   false, // a trailing slash means the folder, as written
		"Done/a.md":              false,
		"done/a.md":              false,
		"Done.md":                true,
		"notes.md":               true,
	} {
		if got := visible(path); got != want {
			t.Errorf("visible(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestExcludeGlobsWithNothingToExclude(t *testing.T) {
	for _, patterns := range [][]string{nil, {}} {
		visible, err := ExcludeGlobs(patterns)
		if err != nil || visible == nil {
			t.Fatalf("ExcludeGlobs(%v) = %v, %v, want a filter and no error", patterns, visible != nil, err)
		}
		if !visible("transcripts/a.md") || !visible("a.md") {
			t.Errorf("ExcludeGlobs(%v) hides something", patterns)
		}
	}
}

// combiningDot is U+0307, the dot above that İ is made of.
const combiningDot = "\xcc\x87"

// The word Tsalagi in Cherokee, in capitals and in small letters, which are other
// characters (U+13E3 is U+ABB3 in small): it is one name.
const (
	cherokeeCapitals = "\u13e3\u13b3\u13a9"
	cherokeeSmall    = "\uabb3\uab83\uab79"
)

// An exclude is for the folder as a filesystem sees it. Where case is folded in
// full, as APFS does, the folder opens under every spelling that folds to its
// name, so a filter that told them apart would leave it open to whoever asks for it
// by another spelling.
func TestExcludeGlobsFoldCaseInFull(t *testing.T) {
	for _, tc := range []struct {
		patterns  []string
		spellings []string // all one name, and all of them hidden
		other     string   // a longer name, which is another and shown
	}{
		{[]string{"transcripts", "TRANSCRIPTS", "tranſcripts", "TRANſCRIPTS"}, []string{"transcripts", "Transcripts", "tranſcripts", "TRANSCRIPTS"}, "transcripts2"},
		{[]string{"σοφία", "ςοφία", "ΣΟΦΊΑ", "Σοφία"}, []string{"σοφία", "ςοφία", "ΣΟΦΊΑ", "Σοφία"}, "σοφίας"},
		{[]string{"strasse", "straße", "STRASSE", "straẞe"}, []string{"strasse", "straße", "Straße", "STRASSE", "straẞe"}, "strassen"},
		{[]string{"file", "ﬁle", "FILE", "ﬁLE"}, []string{"file", "ﬁle", "FILE", "File"}, "files"},
		{[]string{cherokeeCapitals, cherokeeSmall}, []string{cherokeeCapitals, cherokeeSmall}, cherokeeCapitals + "\u13a0"},
	} {
		for _, pattern := range tc.patterns {
			visible, err := ExcludeGlobs([]string{pattern})
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range tc.spellings {
				for _, path := range []string{name, name + "/a.md", name + "/deep/er/a.md"} {
					if visible(path) {
						t.Errorf("ExcludeGlobs(%q) shows %q, want it hidden: it is the same name", pattern, path)
					}
				}
			}
			if !visible(tc.other+"/a.md") || !visible("x/"+tc.spellings[0]+"/a.md") {
				t.Errorf("ExcludeGlobs(%q) hides %q or a folder below another one, want them shown", pattern, tc.other)
			}
		}
	}
}

// Every spelling in the case orbit of a character is one name, for every
// character there is. The orbits are those of unicode.SimpleFold, the simple case
// foldings; the foldings of one character to several (ß to ss) are in the rows of
// TestMatchGlob. It is what keeps a script that the folding gets wrong from
// letting a folder open under another spelling: cases.Fold alone takes the two
// cases of the Cherokee letters for two names.
func TestFoldNameFoldsEveryCaseOrbit(t *testing.T) {
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if !utf8.ValidRune(r) {
			continue
		}
		want := foldName(string(r))
		for q := unicode.SimpleFold(r); q != r; q = unicode.SimpleFold(q) {
			if got := foldName(string(q)); got != want {
				t.Fatalf("%q (U+%04X) folds to %+q and %q (U+%04X) to %+q, want one name for the two", r, r, want, q, q, got)
			}
		}
	}
}

// Whatever the filesystem that the tests run on takes for one name, an exclude for
// one spelling hides the other. On macOS that is what it was written for; on a
// filesystem that tells the spellings apart there is nothing to check but the
// table above.
func TestExcludeGlobsHideWhatTheFilesystemTakesForOneName(t *testing.T) {
	same := 0
	for _, pair := range [][2]string{
		{"Transcripts", "transcripts"},
		{"transcripts", "tranſcripts"},
		{"σοφία", "ςοφία"},
		{"strasse", "straße"},
		{"file", "ﬁle"},
		{"Reflexión", "Reflexio" + combiningAcute + "n"},
	} {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, pair[0]), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(dir, pair[1])); err != nil {
			continue // this filesystem tells them apart
		}
		same++
		for i := range pair {
			visible, err := ExcludeGlobs([]string{pair[i]})
			if err != nil {
				t.Fatal(err)
			}
			if visible(pair[1-i] + "/a.md") {
				t.Errorf("the filesystem opens %q as %q, and ExcludeGlobs(%q) shows it", pair[1-i], pair[i], pair[i])
			}
		}
	}
	t.Logf("%d of the pairs are one name on this filesystem", same)
}
