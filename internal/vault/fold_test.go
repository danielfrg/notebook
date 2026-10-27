package vault

import (
	"testing"
	"unicode/utf8"
)

// Combining marks as bytes, because source text with one in it is too easily
// turned into the precomposed character by a tool or an editor, which would
// turn the decomposed cases below into the composed ones.
const (
	combiningAcute = "\xcc\x81" // U+0301
	combiningTilde = "\xcc\x83" // U+0303
)

func TestFold(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Peña", "pena"},
		{"ÉCOLE", "ecole"},
		{"¿Qué?", "¿que?"},
		{"Marta Peña habló del estoicismo", "marta pena hablo del estoicismo"},
		{"Ünïcödé Çafé ÑANDÚ", "unicode cafe nandu"},
		{"Pen" + combiningTilde + "a", "pena"}, // the same word with the tilde as a mark of its own
		{"İstanbul", "istanbul"},               // I with a dot above
		{"日本語 Ñ Привет", "日本語 n привет"},       // other scripts are lower-cased, not dropped
		{"MIXED case 123 !?", "mixed case 123 !?"},
		{"", ""},
	} {
		if got := Fold(tc.in); got != tc.want {
			t.Errorf("Fold(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// Search folds the query and the notes with the same function and never folds
// a result again, but a function that is not idempotent would still make the
// two sides disagree for a character whose fold needs a second pass.
func TestFoldIsIdempotent(t *testing.T) {
	for r := rune(0); r < 0x20000; r++ {
		if !utf8.ValidRune(r) {
			continue
		}
		for _, s := range []string{string(r), "A" + string(r), string(r) + combiningAcute} {
			once := Fold(s)
			if twice := Fold(once); twice != once {
				t.Fatalf("Fold(Fold(%+q)) = %+q, but Fold(%+q) = %+q", s, twice, s, once)
			}
		}
	}
}

// A query comes from a request and may be anything; what is folded must at
// least be text that can be compared and sent on.
func TestFoldReturnsValidUTF8(t *testing.T) {
	for _, in := range []string{"a\xffb", "\xc3", "É\xe2\x82", "plain\xff"} {
		if got := Fold(in); !utf8.ValidString(got) {
			t.Errorf("Fold(%+q) = %+q, which is not valid UTF-8", in, got)
		}
	}
}
