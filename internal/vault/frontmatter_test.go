package vault

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestSplitFrontmatter(t *testing.T) {
	for _, tc := range []struct {
		name, content string
		front, body   string // for a content that has no frontmatter: "" and the whole content
		ok            bool
	}{
		{"LF", "---\ntitle: A\ntags: [x]\n---\nBody text\n", "title: A\ntags: [x]", "Body text\n", true},
		{"CRLF", "---\r\ntitle: A\r\n---\r\nBody\r\n", "title: A", "Body\r\n", true},
		{"CRLF keeps the breaks inside", "---\r\na: 1\r\nb: 2\r\n---\r\n", "a: 1\r\nb: 2", "", true},
		{"BOM", utf8BOM + "---\ntitle: A\n---\nBody", "title: A", "Body", true},
		{"BOM and CRLF", utf8BOM + "---\r\ntitle: A\r\n---\r\nBody", "title: A", "Body", true},
		{"dots close it too", "---\ntitle: A\n...\nBody", "title: A", "Body", true},
		{"empty", "---\n---\nBody", "", "Body", true},
		{"one blank line", "---\n\n---\nBody", "", "Body", true},
		{"closed by the end of the file", "---\na: 1\n---", "a: 1", "", true},
		{"only the first closer counts", "---\na: 1\n---\ntext\n---\nmore\n", "a: 1", "text\n---\nmore\n", true},
		{"dashes inside a value are not a closer", "---\nrule: ----\nsep: a --- b\n---\nBody", "rule: ----\nsep: a --- b", "Body", true},

		{"not closed", "---\ntitle: A\nBody text\n", "", "---\ntitle: A\nBody text\n", false},
		{"a closer with a trailing space is no closer", "---\na: 1\n--- \nBody", "", "---\na: 1\n--- \nBody", false},
		{"only the opener", "---\n", "", "---\n", false},
		{"only dashes", "---", "", "---", false},
		{"blank line before the fence", "\n---\na: 1\n---\nBody", "", "\n---\na: 1\n---\nBody", false},
		{"text before the fence", "Intro\n---\na: 1\n---\nBody", "", "Intro\n---\na: 1\n---\nBody", false},
		{"space before the fence", " ---\na: 1\n---\n", "", " ---\na: 1\n---\n", false},
		{"text before the fence after a BOM", utf8BOM + "intro\n---\na\n---\n", "", utf8BOM + "intro\n---\na\n---\n", false},
		{"text after the opening dashes", "--- title\na: 1\n---\n", "", "--- title\na: 1\n---\n", false},
		{"four dashes", "----\na: 1\n---\n", "", "----\na: 1\n---\n", false},
		{"a bare CR is no line break", "---\ra: 1\r---\rBody", "", "---\ra: 1\r---\rBody", false},
		{"empty content", "", "", "", false},
		{"BOM only", utf8BOM, "", utf8BOM, false},
	} {
		front, body, ok := SplitFrontmatter(tc.content)
		if front != tc.front || body != tc.body || ok != tc.ok {
			t.Errorf("%s: SplitFrontmatter(%q) = %q, %q, %v, want %q, %q, %v",
				tc.name, tc.content, front, body, ok, tc.front, tc.body, tc.ok)
		}
	}
}

func TestParseFrontmatterTolerant(t *testing.T) {
	none := map[string]any{}
	for _, tc := range []struct {
		name, content string
		values        map[string]any
		body          string
	}{
		{"values of every kind",
			"---\ntitle: Hi\ncount: 3\nok: true\ntags: [a, b]\nnested:\n  k: v\n---\nBody",
			map[string]any{"title": "Hi", "count": 3, "ok": true, "tags": []any{"a", "b"}, "nested": map[string]any{"k": "v"}},
			"Body"},
		{"BOM and CRLF", utf8BOM + "---\r\ntitle: Hi\r\n---\r\nBody", map[string]any{"title": "Hi"}, "Body"},
		{"dots close it too", "---\ntitle: Hi\n...\nBody", map[string]any{"title": "Hi"}, "Body"},

		// Anything that is not frontmatter leaves the whole text as the body.
		{"no frontmatter", "# Heading\n\ntext", none, "# Heading\n\ntext"},
		{"not closed", "---\ntitle: Hi\n", none, "---\ntitle: Hi\n"},
		{"fence not at byte 0", "\n---\ntitle: Hi\n---\nBody", none, "\n---\ntitle: Hi\n---\nBody"},

		// A block whose YAML cannot be read is still frontmatter: it has no
		// values, and the body is what follows it.
		{"unclosed flow list", "---\ntitle: [unclosed\n---\nBody", none, "Body"},
		{"a colon in a plain value", "---\ntitle: a: b\n---\nBody", none, "Body"},
		{"duplicate keys", "---\na: 1\na: 2\n---\nBody", none, "Body"},
		{"a scalar is no mapping", "---\njust some text\n---\nBody", none, "Body"},
		{"a list is no mapping", "---\n- a\n- b\n---\nBody", none, "Body"},
		{"empty", "---\n---\nBody", none, "Body"},
		{"only a comment", "---\n# only a comment\n---\nBody", none, "Body"},
	} {
		values, body := ParseFrontmatter(tc.content)
		if values == nil || !reflect.DeepEqual(values, tc.values) || body != tc.body {
			t.Errorf("%s: ParseFrontmatter(%q) = %#v, %q, want %#v, %q",
				tc.name, tc.content, values, body, tc.values, tc.body)
		}
	}
}

// The values go to agents as JSON. YAML allows what JSON does not (keys that are
// not text, infinity), and a note must not become unreadable for that.
func TestParseFrontmatterValuesEncodeAsJSON(t *testing.T) {
	content := strings.Join([]string{
		"---",
		"plain: x",
		"when: 2024-05-06",
		"nested:",
		"  1: one",
		"  true: yes",
		"  ~: nothing",
		"  deeper:",
		"    2.5: x",
		"unbounded: .inf",
		"unknown: .nan",
		"list: [1, 2.5, null, {3: c}]",
		"---",
		"Body",
	}, "\n")
	values, body := ParseFrontmatter(content)
	if body != "Body" {
		t.Errorf("body = %q, want Body", body)
	}
	if _, err := json.Marshal(values); err != nil {
		t.Fatalf("json.Marshal(%#v) = %v", values, err)
	}

	nested, _ := values["nested"].(map[string]any)
	for key, want := range map[string]any{"1": "one", "true": "yes", "null": "nothing"} {
		if nested[key] != want {
			t.Errorf("nested[%q] = %#v, want %#v", key, nested[key], want)
		}
	}
	if deeper, _ := nested["deeper"].(map[string]any); deeper["2.5"] != "x" {
		t.Errorf("nested.deeper = %#v, want a mapping with the key 2.5", nested["deeper"])
	}
	if values["unbounded"] != "+Inf" || values["unknown"] != "NaN" {
		t.Errorf("unbounded = %#v, unknown = %#v, want the strings +Inf and NaN", values["unbounded"], values["unknown"])
	}
	list, _ := values["list"].([]any)
	if len(list) != 4 {
		t.Fatalf("list = %#v, want 4 items", values["list"])
	}
	if item, _ := list[3].(map[string]any); item["3"] != "c" {
		t.Errorf("list[3] = %#v, want a mapping with the key 3", list[3])
	}
}

func TestTags(t *testing.T) {
	for _, tc := range []struct {
		name, content string
		want          []string
	}{
		{"frontmatter list", "---\ntags:\n  - a\n  - b/c\n---\nbody", []string{"a", "b/c"}},
		{"frontmatter flow list", "---\ntags: [a, \"b c\"]\n---\n", []string{"a", "b c"}},
		{"frontmatter comma string", "---\ntags: x, y\n---\n", []string{"x", "y"}},
		{"frontmatter tag key", "---\ntag: z\n---\n", []string{"z"}},
		{"frontmatter hash prefix", "---\ntags: [\"#x\", y]\n---\n", []string{"x", "y"}},
		{"frontmatter values that are not text", "---\ntags: [1, true, 2.5, {a: b}, [c], null, x]\n---\n", []string{"1", "2.5", "true", "x"}},
		{"frontmatter other keys are not tags", "---\ntitle: '#notatag'\ncategory: books\n---\nbody", []string{}},
		{"frontmatter is not scanned for inline tags", "---\nnote: see #nope\n---\n#yes", []string{"yes"}},
		{"frontmatter that cannot be read still has a body", "---\ntags: [unclosed\n---\n#inline", []string{"inline"}},

		{"inline", "Some #idea here.\n#second at the start\n", []string{"idea", "second"}},
		{"nested inline", "see #a/b and #a/b/c.", []string{"a/b", "a/b/c"}},
		{"punctuation ends a tag", "#one, #two. #three; #four) #five!", []string{"five", "four", "one", "three", "two"}},
		{"unicode", "#título #日本語 #Ñandú", []string{"título", "Ñandú", "日本語"}},
		{"digits need a letter", "#2024 #y2024 #2024-q1", []string{"2024-q1", "y2024"}},
		{"not tags", "# Heading\n## Another\n#123 and a#b and C# and http://x.y/#frag and &#39; and #\n", []string{}},
		{"fenced code", "before\n```go\n#include <x>\n#define Y\n```\nafter #real\n~~~\n#tilde\n~~~\n", []string{"real"}},
		{"a fence that is not closed holds the rest", "#a\n```\n#b\n", []string{"a"}},
		{"inline code", "run `make test #fast` and ``a ` #b`` then #real", []string{"real"}},
		{"a backtick that is not matched is just a backtick", "price ` is #c", []string{"c"}},
		{"CRLF and BOM", utf8BOM + "---\r\ntags: [fm]\r\n---\r\n#inl\r\n", []string{"fm", "inl"}},
		{"BOM without frontmatter", utf8BOM + "#first\n", []string{"first"}},
		{"what looks like frontmatter after text is body", "\n---\ntags: [x]\n---\n#y", []string{"y"}},

		{"merged, deduplicated and sorted", "---\ntags: [b, a]\n---\n#a #c #b #a", []string{"a", "b", "c"}},
		{"no tags", "plain text", []string{}},
		{"empty", "", []string{}},
	} {
		// reflect.DeepEqual tells nil from an empty slice, and Tags never
		// returns nil: its result becomes a JSON array.
		if got := Tags(tc.content); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: Tags(%q) = %#v, want %#v", tc.name, tc.content, got, tc.want)
		}
	}
}

func TestTitle(t *testing.T) {
	for path, want := range map[string]string{
		"a.md":                "a",
		"dir/sub/¿Qué es?.md": "¿Qué es?",
		"Marta Peña.md":       "Marta Peña",
		"x.md.md":             "x.md",
		"notes.txt.md":        "notes.txt",
		"dir.md/inner.md":     "inner",
		"no-extension":        "no-extension",
		"dir/no-extension":    "no-extension",
		"a,b [c] 100% #1.md":  "a,b [c] 100% #1",
		"":                    "",
	} {
		if got := Title(path); got != want {
			t.Errorf("Title(%q) = %q, want %q", path, got, want)
		}
	}
}
