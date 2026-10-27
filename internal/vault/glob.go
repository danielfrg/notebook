package vault

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

// Filter reports whether a note is visible, given its vault-relative path.
type Filter func(path string) bool

// MatchGlob reports whether pattern matches path, or a folder above it, so a
// pattern that names a folder also matches everything in it. Both are split on
// "/" and matched from the top of the vault, one name at a time: "*" is any run
// of characters inside a name, "?" is one character, and a name that is "**" is
// any number of names, none included. Nothing else is special, so the other
// characters that notes have in their names ("[", "%", ",") match themselves.
// Names are compared by character and not by byte, so an accent that is one
// character in one name and a letter with a mark in the other is the same name,
// and without regard to case, which is folded in full: ſ is s, ς is σ, ß is ss and
// ﬁ is fi (see foldName). A filesystem may ignore case that way (macOS does, by
// default, and Windows does in its own way): there a folder called tranſcripts is
// the folder transcripts, and a pattern for transcripts that did not hide it would
// show what it was meant to hide. Where the filesystem does tell the names apart,
// a pattern also hides the names that differ only in case, which is the safe way
// to be wrong. Because one character can fold to several (ß to ss), "?" is one
// character of the folded name: stra??e matches straße, and stra?e does not.
// MatchGlob does not check pattern; NormalizeGlob does.
func MatchGlob(pattern, path string) bool {
	return compileGlob(pattern).matches(splitPath(path))
}

// glob is the names of a pattern, followed by a "**" that lets the match stop
// at any folder on the way to a path.
type glob []string

func compileGlob(pattern string) glob {
	return append(strings.Split(foldName(pattern), "/"), "**")
}

func splitPath(path string) []string {
	return strings.Split(foldName(path), "/")
}

// folding is Unicode full case folding. It is stateless, so it is shared.
var folding = cases.Fold()

// foldName makes names comparable the way a filesystem may compare them: the
// accents of a name are in one form (NFC), every character is folded to its full
// Unicode case folding, and the result is put in NFC again, because folding may
// leave it in another form. Lower-casing is not enough: the folding of ſ is s, of
// ς is σ, of ß is ss and of ﬁ is fi, and APFS takes those names for one as it takes
// Transcripts for transcripts. A pattern and a path are folded the same way, and
// nothing else decides whether two names are the same.
//
// The folding is followed by a lower-casing of what it leaves, for the letters of
// Cherokee: cases.Fold takes each of them for the other case (it folds Ꭰ to ꭰ and
// ꭰ to Ꭰ), so two spellings of a Cherokee name would stay two names, and an
// exclude for one would show the other. After it they are one. That changes nothing
// else: of all the characters there are, it changes only the 86 small Cherokee
// letters that folding has turned to capitals, and TestFoldNameFoldsEveryCaseOrbit
// tests all of them.
func foldName(s string) string {
	return norm.NFC.String(strings.ToLower(folding.String(norm.NFC.String(s))))
}

// matches reports whether the glob matches all the names of a path.
func (g glob) matches(names []string) bool {
	// The usual way to match a wildcard without trying every way of it: on a
	// mismatch, go back to the last "**" and let it take one more name.
	p, n := 0, 0
	star, starName := -1, 0
	for n < len(names) {
		switch {
		case p < len(g) && g[p] == "**":
			star, starName = p, n
			p++
		case p < len(g) && matchName(g[p], names[n]):
			p++
			n++
		case star >= 0:
			starName++
			p, n = star+1, starName
		default:
			return false
		}
	}
	for p < len(g) && g[p] == "**" {
		p++
	}
	return p == len(g)
}

// matchName reports whether one name of a pattern, with its "*" and "?",
// matches name.
func matchName(pattern, name string) bool {
	if !strings.ContainsAny(pattern, "*?") {
		return pattern == name
	}
	p, n := 0, 0
	star, starName := -1, 0 // after the last "*" in pattern, and where in name it has got to
	for n < len(name) {
		if p < len(pattern) {
			switch pattern[p] {
			case '*':
				for p < len(pattern) && pattern[p] == '*' {
					p++
				}
				if p == len(pattern) {
					return true
				}
				star, starName = p, n
				continue
			case '?':
				_, size := utf8.DecodeRuneInString(name[n:])
				p++
				n += size
				continue
			default:
				pr, psize := utf8.DecodeRuneInString(pattern[p:])
				nr, nsize := utf8.DecodeRuneInString(name[n:])
				if pr == nr {
					p += psize
					n += nsize
					continue
				}
			}
		}
		if star < 0 {
			return false
		}
		_, size := utf8.DecodeRuneInString(name[starName:])
		starName += size
		p, n = star, starName
	}
	for p < len(pattern) && pattern[p] == '*' {
		p++
	}
	return p == len(pattern)
}

// NormalizeGlob checks an exclude pattern and returns it in the form that is
// matched: white space around the pattern and one trailing "/" are trimmed, and
// nothing else is rewritten. What is left must not be empty, start with "/" or
// hold a backslash, and none of its "/"-separated names may be empty, "." or
// "..". Such a pattern can never match a path, and an exclude that silently hides
// nothing would show what it was meant to hide, so it is an error that names the
// pattern.
//
// This is the one definition of what an exclude pattern may be: ExcludeGlobs
// uses it, and so should the code that reads the patterns from the settings, so
// that a pattern cannot be accepted by one and rejected by the other.
func NormalizeGlob(pattern string) (string, error) {
	canonical := strings.TrimSuffix(strings.TrimSpace(pattern), "/")
	reason := ""
	switch {
	case canonical == "":
		reason = "is empty"
	case strings.HasPrefix(canonical, "/"):
		reason = "must be relative to the vault"
	case strings.Contains(canonical, `\`):
		reason = `must separate folders with "/", not a backslash`
	default:
		for name := range strings.SplitSeq(canonical, "/") {
			if name == "" || name == "." || name == ".." {
				reason = `must not contain empty, "." or ".." path segments`
				break
			}
		}
	}
	if reason != "" {
		return "", fmt.Errorf("exclude pattern %q %s", pattern, reason)
	}
	return canonical, nil
}

// ExcludeGlobs returns the filter that shows a path unless a pattern matches it
// (see MatchGlob), for the paths that must stay hidden from agents. Each pattern
// is checked by NormalizeGlob, and one that it rejects is an error and no filter
// is made, so nothing starts up half excluded.
func ExcludeGlobs(patterns []string) (Filter, error) {
	globs := make([]glob, 0, len(patterns))
	for _, pattern := range patterns {
		canonical, err := NormalizeGlob(pattern)
		if err != nil {
			return nil, err
		}
		globs = append(globs, compileGlob(canonical))
	}
	return func(path string) bool {
		names := splitPath(path)
		for _, g := range globs {
			if g.matches(names) {
				return false
			}
		}
		return true
	}, nil
}
