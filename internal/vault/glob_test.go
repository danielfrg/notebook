package vault

import (
	"fmt"
	"strings"
	"testing"
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
		{"transcripts", "TRANSCRIPTS/a.md", true},
		{"TRANSCRIPTS", "transcripts/a.md", true},
		{"transcripts", "Transcripts", true},
		{"**/*.Private.md", "a/X.PRIVATE.MD", true},
		{"a?c.md", "AÉC.md", true},
		{"ÉCOLE", "école/a.md", true}, // letters that are not ASCII too
		{"Reflexión", "REFLEXIÓN/a.md", true},
		{"Reflexio" + combiningAcute + "n", "REFLEXIÓN/a.md", true}, // together with the other spelling of an accent
		{"transcripts", "transcripts2/A.md", false},
	} {
		if got := MatchGlob(tc.pattern, tc.path); got != tc.want {
			t.Errorf("MatchGlob(%q, %q) = %v, want %v", tc.pattern, tc.path, got, tc.want)
		}
	}
}

// What an exclude pattern may be is decided in one place, NormalizeGlob, and
// ExcludeGlobs uses it, so that a pattern accepted by one of them cannot be
// rejected by the other: such a pattern would hide nothing, without a word, and
// a recorded conversation would reach third-party models. These are the patterns
// of TestParseExcludes in cmd/notebook, which still has a copy of the rule of its
// own until it calls NormalizeGlob; keep the two tables in step.
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
