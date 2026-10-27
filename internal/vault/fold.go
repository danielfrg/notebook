package vault

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/runes"
	"golang.org/x/text/unicode/norm"
)

// combiningMarks are the nonspacing marks that NFD splits an accent into.
var combiningMarks = runes.In(unicode.Mn)

// Fold returns s the way search compares text: decomposed (NFD), without its
// combining marks, and lower-cased. "Peña", "PEÑA" and "pena" fold to the same
// text, and so do the composed and the decomposed spelling of an accent. Fold
// is idempotent, and what it returns is valid UTF-8 whatever s is.
func Fold(s string) string {
	// Most text is ASCII, and for that the work is a lower-casing that returns s
	// itself when there is nothing to change.
	if isASCII(s) {
		return strings.ToLower(s)
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range norm.NFD.String(s) {
		switch {
		case r < utf8.RuneSelf:
			if 'A' <= r && r <= 'Z' {
				r += 'a' - 'A'
			}
			b.WriteByte(byte(r))
		case combiningMarks.Contains(r):
		default:
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

func isASCII(s string) bool {
	for i := range len(s) {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}
