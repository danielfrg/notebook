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
		{"Private/**", "private/a.md", false}, // names are compared exactly, case included
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
		{"Reflexión", "Reflexion/a.md", false},
	} {
		if got := MatchGlob(tc.pattern, tc.path); got != tc.want {
			t.Errorf("MatchGlob(%q, %q) = %v, want %v", tc.pattern, tc.path, got, tc.want)
		}
	}
}

// ExcludeGlobs and parseExcludes in cmd/notebook state one rule, and an exclude
// that one of them accepts and the other rejects would hide nothing without a
// word: a recorded conversation would reach third-party models. These are the
// patterns of TestParseExcludes there; keep the two tables in step.
var (
	// Accepted, and the path each one hides. One trailing "/" and white space
	// around a pattern are trimmed and nothing else is rewritten, so a pattern
	// that is accepted is one that matches.
	acceptedExcludes = []struct{ pattern, hides string }{
		{"transcripts", "transcripts/a.md"},
		{"transcripts/", "transcripts/a.md"},
		{" transcripts/ ", "transcripts/a.md"},
		{"Private/**", "Private/a.md"},
		{"Private/**/", "Private/a.md"},
		{"**/*.private.md", "a/b.private.md"},
		{"a/*/c.md", "a/b/c.md"},
		{"a b/c?", "a b/cd"},
		{"Q1..Q2", "Q1..Q2/x.md"}, // dots inside a name are not "." or ".." segments
		{"..data", "..data/x.md"},
		{".obsidian", ".obsidian/x.md"},
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

func TestExcludeGlobsAcceptsAndRejectsLikeParseExcludes(t *testing.T) {
	for _, tc := range acceptedExcludes {
		visible, err := ExcludeGlobs([]string{tc.pattern})
		if err != nil {
			t.Errorf("ExcludeGlobs(%q) = %v, want it accepted", tc.pattern, err)
		} else if visible(tc.hides) {
			t.Errorf("ExcludeGlobs(%q) shows %q, want it hidden", tc.pattern, tc.hides)
		}
	}
	for _, pattern := range rejectedExcludes {
		if _, err := ExcludeGlobs([]string{pattern}); err == nil {
			t.Errorf("ExcludeGlobs(%q) was accepted, want an error", pattern)
		} else if !strings.Contains(err.Error(), fmt.Sprintf("%q", pattern)) {
			t.Errorf("ExcludeGlobs(%q) = %q, want an error that names the pattern", pattern, err)
		}
	}
	// One bad pattern among good ones fails them all: nothing may start up half
	// excluded.
	if visible, err := ExcludeGlobs([]string{"transcripts", "./oops"}); err == nil || visible != nil {
		t.Errorf("ExcludeGlobs with one bad pattern = %v, %v, want nil and an error", visible != nil, err)
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
		"x.private.md":           false,
		"deep/er/x.private.md":   false,
		"x.private.mdx":          true,
		"Private":                false,
		"Private/a.md":           false,
		"private/a.md":           true,
		"a/b/c.md":               false,
		"a/c.md":                 true,
		"xy/z.md":                false,
		"x/z.md":                 true,
		"Done":                   false, // a trailing slash means the folder, as written
		"Done/a.md":              false,
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
