package vault

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestCleanPath(t *testing.T) {
	accept := []string{
		"a.md",
		"a/b/c.md",
		"¿Qué es?.md",
		"100% #1.md",
		"Dario Z - El amor - Clase 1.md",
		"a,b [c].md",
		"日本語/ノート.md",
		"a b/c d.md",
		"Q1..Q2.md",
		"notes.d/x.md",
		"trailing.",
		strings.Repeat("a", 255), // the longest component
		strings.Repeat("é", 127), // 254 bytes in 127 characters
		strings.Repeat("a", 255) + "/" + strings.Repeat("b", 254) + "/c", // exactly 512 bytes
	}
	for _, p := range accept {
		got, err := CleanPath(p)
		if err != nil || got != p {
			t.Errorf("CleanPath(%s) = %q, %v, want it accepted unchanged", describe(p), got, err)
		}
	}

	reject := []string{
		"",
		"/a",
		"/",
		"../a",
		"a/../b",
		"a//b",
		"a/",
		"./a",
		`a\b`,
		`..\x`,
		`a\`,
		".hidden/x.md",
		"a/.x.md",
		".",
		"..",
		"...",
		"a/.",
		"a/..",
		".trash/x.md",
		".git/config",
		"a/.git/config",
		"a\x00b",
		strings.Repeat("a", 255) + "/" + strings.Repeat("b", 255) + "/c", // 513 bytes, every component fits
		strings.Repeat("a", 256),                // component of 256 bytes
		"d/" + strings.Repeat("é", 128) + ".md", // 259 bytes in one component: bytes count, not characters
		"a\xffb.md",                             // invalid UTF-8
		"a\nb.md",
		"a\tb.md",
		"a\x7fb.md",
		"a\u0085b.md",
	}
	for _, p := range reject {
		got, err := CleanPath(p)
		if !errors.Is(err, ErrInvalidPath) || got != "" {
			t.Errorf("CleanPath(%s) = %q, %v, want %q and ErrInvalidPath", describe(p), got, err, "")
		}
	}
}

// describe keeps failure messages readable for the long paths in the tables.
func describe(p string) string {
	if len(p) > 40 {
		return fmt.Sprintf("a path of %d bytes", len(p))
	}
	return fmt.Sprintf("%q", p)
}

func TestIsNote(t *testing.T) {
	for p, want := range map[string]bool{
		"a.md":         true,
		"a/b.md":       true,
		"¿Qué es?.md":  true,
		"a.md.md":      true,
		"a.MD":         false,
		"a.txt":        false,
		"a.md.txt":     false,
		"a.markdown":   false,
		"md":           false,
		"a.png":        false,
		"":             false,
		"a.md/b.txt":   false,
		"folder.md/b":  false,
		"folder.md/b/": false,
	} {
		if got := IsNote(p); got != want {
			t.Errorf("IsNote(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestIsAsset(t *testing.T) {
	for p, want := range map[string]bool{
		"pic.png":      true,
		"pic.PNG":      true,
		"a/b/c.jpg":    true,
		"x.JPEG":       true,
		"x.jpeg":       true,
		"x.gif":        true,
		"x.webp":       true,
		"x.WebP":       true,
		"x.avif":       true,
		"x.AVIF":       true,
		"a.b/c.png":    true,
		"x.svg":        false,
		"x.bmp":        false,
		"x.tiff":       false,
		"x.pdf":        false,
		"x.png.txt":    false,
		"x.png.md":     false,
		"png":          false,
		"a.png/b":      false,
		"":             false,
		"a.md":         false,
		"pic.png/":     false,
		"folder.jpg/x": false,
	} {
		if got := IsAsset(p); got != want {
			t.Errorf("IsAsset(%q) = %v, want %v", p, got, want)
		}
	}
}
