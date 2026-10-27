package vault

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// byteOrderMark is the UTF-8 BOM that Obsidian and Windows editors may put at
// the start of a note.
const byteOrderMark = "\xef\xbb\xbf"

// SplitFrontmatter splits the YAML frontmatter off a note. A note has
// frontmatter when its first line, after an optional byte order mark, is "---"
// and a later line is "---" or "..."; the line breaks may be LF or CRLF. front
// is the text between the two fences without the line break before the closing
// one, and body is everything after the closing fence's line. Anything else
// (the fence is not the very first line, or it is never closed) is no
// frontmatter: ok is false, front is empty and body is content.
func SplitFrontmatter(content string) (front, body string, ok bool) {
	opener, rest, hasBreak := cutLine(strings.TrimPrefix(content, byteOrderMark))
	if opener != "---" || !hasBreak {
		return "", content, false
	}
	for pos := 0; pos < len(rest); {
		line, after, _ := cutLine(rest[pos:])
		if line == "---" || line == "..." {
			front = strings.TrimSuffix(strings.TrimSuffix(rest[:pos], "\n"), "\r")
			return front, after, true
		}
		pos = len(rest) - len(after)
	}
	return "", content, false
}

// cutLine returns the first line of s without its line break, and what follows
// the break. hasBreak is false when s has no "\n", and then line is all of s.
func cutLine(s string) (line, rest string, hasBreak bool) {
	i := strings.IndexByte(s, '\n')
	if i < 0 {
		return s, "", false
	}
	return strings.TrimSuffix(s[:i], "\r"), s[i+1:], true
}

// ParseFrontmatter reads the YAML frontmatter of a note (see SplitFrontmatter)
// as a mapping and returns it with the body. It does not fail: a note without
// frontmatter has no values and its whole text is the body, and a block whose
// YAML cannot be read (or is not a mapping) is still frontmatter, with no
// values and the text after it as the body. The values always encode as JSON:
// keys that are not text are written as text, and infinity and NaN become the
// strings "+Inf", "-Inf" and "NaN".
func ParseFrontmatter(content string) (values map[string]any, body string) {
	front, body, ok := SplitFrontmatter(content)
	if !ok {
		return map[string]any{}, content
	}
	var parsed map[string]any
	if err := yaml.Unmarshal([]byte(front), &parsed); err != nil || parsed == nil {
		return map[string]any{}, body
	}
	return jsonSafe(parsed).(map[string]any), body
}

// jsonSafe returns v with what encoding/json cannot encode replaced. It changes
// the maps and slices it is given, which are the freshly decoded values.
func jsonSafe(v any) any {
	switch v := v.(type) {
	case map[string]any:
		for key, item := range v {
			v[key] = jsonSafe(item)
		}
		return v
	case map[any]any: // a mapping with a key that is not text
		m := make(map[string]any, len(v))
		for key, item := range v {
			if key == nil {
				m["null"] = jsonSafe(item)
			} else {
				m[fmt.Sprint(key)] = jsonSafe(item)
			}
		}
		return m
	case []any:
		for i, item := range v {
			v[i] = jsonSafe(item)
		}
		return v
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return fmt.Sprint(v)
		}
	}
	return v
}

// Tags returns the tags of a note, without the "#", deduplicated and sorted
// (never nil). They are the ones the frontmatter lists in tags or tag, as a
// list or as a string separated by commas, and the inline #tags of the body,
// which are read as Obsidian reads them: a "#" that starts a line or follows
// white space, then letters, digits, "_", "-" and "/" (a nested tag is "a/b"),
// and at least one of them is not a digit, so "# Heading", "#123" and "a#b"
// are no tags. Code is not read: neither fenced blocks nor `inline code`.
func Tags(content string) []string {
	values, body := ParseFrontmatter(content)
	tags := []string{}
	tags = appendPropertyTags(tags, values["tags"])
	tags = appendPropertyTags(tags, values["tag"])
	tags = appendInlineTags(tags, strings.TrimPrefix(body, byteOrderMark))
	slices.Sort(tags)
	return slices.Compact(tags)
}

// appendPropertyTags appends the tags that a frontmatter value holds: a list of
// them, or one string of them separated by commas. A number or a boolean is
// taken as its text, and anything else (a mapping, a nested list, nothing) is
// no tag.
func appendPropertyTags(tags []string, value any) []string {
	switch v := value.(type) {
	case string:
		for tag := range strings.SplitSeq(v, ",") {
			tags = appendTag(tags, tag)
		}
	case []any:
		for _, item := range v {
			switch item.(type) {
			case map[string]any, []any, nil:
			default:
				tags = appendTag(tags, fmt.Sprint(item))
			}
		}
	case map[string]any, nil:
	default:
		tags = appendTag(tags, fmt.Sprint(v))
	}
	return tags
}

// appendTag appends tag, as a frontmatter wrote it, without white space around
// it or a "#" in front of it, unless that leaves nothing.
func appendTag(tags []string, tag string) []string {
	tag = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(tag), "#"))
	if tag == "" {
		return tags
	}
	return append(tags, tag)
}

// appendInlineTags appends the #tags in text, which is the body of a note.
func appendInlineTags(tags []string, text string) []string {
	fence := "" // the opening fence while inside a fenced block
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimLeft(strings.TrimSuffix(line, "\r"), " \t")
		if fence != "" {
			if closesFence(line, fence) {
				fence = ""
			}
			continue
		}
		if fence = opensFence(line); fence == "" && strings.IndexByte(line, '#') >= 0 {
			tags = appendLineTags(tags, line)
		}
	}
	return tags
}

// opensFence returns the fence that line opens, a run of at least three
// backticks or tildes, or "" if it opens none. The text after backticks may
// not hold another, because then it is code on one line.
func opensFence(line string) string {
	if line == "" || (line[0] != '`' && line[0] != '~') {
		return ""
	}
	n := runLength(line, line[0])
	if n < 3 || (line[0] == '`' && strings.IndexByte(line[n:], '`') >= 0) {
		return ""
	}
	return line[:n]
}

// closesFence reports whether line closes a block opened by fence: a run of the
// same character that is at least as long, and nothing else.
func closesFence(line string, fence string) bool {
	n := runLength(line, fence[0])
	return n >= len(fence) && strings.TrimRight(line[n:], " \t") == ""
}

// runLength is how many times c is repeated at the start of s.
func runLength(s string, c byte) int {
	n := 0
	for n < len(s) && s[n] == c {
		n++
	}
	return n
}

// appendLineTags appends the tags in one line of text that is not in a fenced block.
func appendLineTags(tags []string, line string) []string {
	boundary := true // at the start of the line, or just after white space
	for i := 0; i < len(line); {
		switch c := line[i]; {
		case c == '`':
			// A code span runs from a run of backticks to the next run as long. A
			// run that is not matched is just backticks.
			n := runLength(line[i:], '`')
			i += n
			if end := closingRun(line[i:], n); end >= 0 {
				i += end + n
			}
			boundary = false
		case c == '#' && boundary:
			tag, size := scanTag(line[i+1:])
			if tag != "" {
				tags = append(tags, tag)
			}
			i += 1 + size
			boundary = false
		default:
			r, size := utf8.DecodeRuneInString(line[i:])
			boundary = unicode.IsSpace(r)
			i += size
		}
	}
	return tags
}

// closingRun returns where the first run of exactly n backticks starts in s, or -1.
func closingRun(s string, n int) int {
	for i := 0; i < len(s); {
		if s[i] != '`' {
			i++
			continue
		}
		run := runLength(s[i:], '`')
		if run == n {
			return i
		}
		i += run
	}
	return -1
}

// scanTag reads the tag that s starts with, s being the text after a "#", and
// returns it with how many bytes it took up. The tag is "" when what follows the
// "#" is not one.
func scanTag(s string) (tag string, size int) {
	for size < len(s) {
		r, n := utf8.DecodeRuneInString(s[size:])
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r) || r == '_' || r == '-' || r == '/') {
			break
		}
		size += n
	}
	tag = strings.TrimRight(s[:size], "/")
	if strings.HasPrefix(tag, "/") || !strings.ContainsFunc(tag, func(r rune) bool { return !unicode.IsDigit(r) }) {
		return "", size
	}
	return tag, size
}

// Title returns the title of the note at p: its file name without ".md".
func Title(p string) string {
	return strings.TrimSuffix(p[strings.LastIndexByte(p, '/')+1:], ".md")
}
