package vault

import (
	"fmt"
	"strings"
	"unicode/utf8"

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
// Names are compared exactly, case included, but by character and not by byte:
// an accent that is one character in one name and a letter with a mark in the
// other is the same name. MatchGlob does not check pattern; ExcludeGlobs does.
func MatchGlob(pattern, path string) bool {
	return compileGlob(pattern).matches(splitPath(path))
}

// glob is the names of a pattern, followed by a "**" that lets the match stop
// at any folder on the way to a path.
type glob []string

func compileGlob(pattern string) glob {
	return append(strings.Split(norm.NFC.String(pattern), "/"), "**")
}

func splitPath(path string) []string {
	return strings.Split(norm.NFC.String(path), "/")
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

// ExcludeGlobs returns the filter that shows a path unless a pattern matches it
// (see MatchGlob), for the paths that must stay hidden from agents. A pattern
// is a path relative to the vault: white space around it and one trailing "/"
// are trimmed, and what is left must not be empty or start with "/" or hold a
// backslash, and none of its "/"-separated names may be empty, "." or "..".
// Such a pattern can never match a path, and an exclude that silently hides
// nothing would show what it was meant to hide, so it is an error and no filter
// is made. This is the rule that parseExcludes in cmd/notebook applies when it
// reads the settings; the two have to agree.
func ExcludeGlobs(patterns []string) (Filter, error) {
	globs := make([]glob, 0, len(patterns))
	for _, pattern := range patterns {
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
			return nil, fmt.Errorf("exclude pattern %q %s", pattern, reason)
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
