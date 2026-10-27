package vault

import (
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxPathBytes      = 512
	maxComponentBytes = 255
)

// CleanPath checks a vault-relative path and returns it unchanged, or
// ErrInvalidPath. A valid path is slash-separated and at most 512 bytes of
// UTF-8, with no backslash or control character, and its components are
// non-empty, at most 255 bytes and do not start with "." (which makes them
// hidden, and covers "." and ".."). It never normalizes: "a//b", "./a" and
// "a/" are invalid rather than rewritten, so what a caller validated is exactly
// what reaches the filesystem.
func CleanPath(p string) (string, error) {
	if p == "" || len(p) > maxPathBytes || !utf8.ValidString(p) {
		return "", ErrInvalidPath
	}
	for _, r := range p {
		if r == '\\' || unicode.IsControl(r) {
			return "", ErrInvalidPath
		}
	}
	for part := range strings.SplitSeq(p, "/") {
		if part == "" || part[0] == '.' || len(part) > maxComponentBytes {
			return "", ErrInvalidPath
		}
	}
	return p, nil
}

// IsNote reports whether p names a Markdown note. It only looks at the name.
func IsNote(p string) bool { return strings.HasSuffix(p, ".md") }

// IsAsset reports whether p names an image the app serves. It only looks at the name.
func IsAsset(p string) bool {
	switch strings.ToLower(path.Ext(p)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".avif":
		return true
	}
	return false
}
