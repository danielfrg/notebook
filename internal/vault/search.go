package vault

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	defaultSearchLimit = 50
	maxSnippets        = 3
	snippetRunes       = 200
	// How a note is scored, for each term of the query.
	scoreName    = 50 // the term is in the file name
	scorePath    = 20 // the term is in the path, and not in the file name
	maxScoreText = 20 // one for each time it is in the text, and no more than this
)

// ErrEmptyQuery is the error of a search for nothing: a query without a term.
var ErrEmptyQuery = errors.New("empty query")

// Snippet is a line of a note that a search matched, as written in the note and
// cut to about 200 characters around the match.
type Snippet struct {
	Line int    `json:"line"` // numbered from 1, as the editor numbers the lines of the note
	Text string `json:"text"`
}

// Hit is a note that a search found.
type Hit struct {
	Path, Title string
	// Score ranks the hits. Each term of the query adds 50 if it is in the file
	// name, else 20 if it is in the path, and 1 for each time it is in the text
	// of the note, up to 20.
	Score    int
	Modified time.Time
	// Snippets are the first three lines that hold a term, in the order of the
	// note. A note that was found only by its path has none, and the slice is
	// never nil.
	Snippets []Snippet
}

// SearchOptions are the options of Search.
type SearchOptions struct {
	// Limit is the most hits to return; 0 or less means 50.
	Limit int
	// Visible, if set, decides which notes may be found: the others are not
	// matched at all, so they do not show up in a result, take up room in the
	// limit or give away anything by their score.
	Visible Filter
}

// Search finds the notes that match a query. The query is words separated by
// white space, in which a part inside double quotes is one phrase, and a quote
// that is not closed runs to the end. Case and accents do not matter, every term
// has to be in the note (in its path, which does not have ".md", or anywhere in
// its text, frontmatter included; they need not be in the same line), and a
// phrase has to be in one line, in order. The terms are searched for as they
// are, not as whole words. The hits are ordered by Score, highest first, and
// among equals the newest note first, and there are at most opts.Limit of them
// (see SearchOptions). A query without a term is ErrEmptyQuery.
//
// Nothing is indexed on disk: the notes are searched in a snapshot in memory
// that is refreshed, by size and modification time, when it is two seconds old
// or more, so a change shows up after that long at the latest. A note over 2 MiB
// or that is not UTF-8 is left out. The hits and what is in them belong to the
// caller.
func (v *Vault) Search(ctx context.Context, query string, opts SearchOptions) ([]Hit, error) {
	terms := parseQuery(query)
	if len(terms) == 0 {
		return nil, ErrEmptyQuery
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entries, err := v.search.current(ctx, v)
	if err != nil {
		return nil, err
	}

	var found []match
	counts := make([]int, len(terms))
	visited := 0
	for path, e := range entries {
		if visited++; visited%256 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		if e.skipped || (opts.Visible != nil && !opts.Visible(path)) {
			continue
		}
		if m, ok := e.find(path, terms, counts); ok {
			found = append(found, m)
		}
	}
	slices.SortFunc(found, func(a, b match) int {
		return cmp.Or(
			cmp.Compare(b.score, a.score),
			b.entry.modified.Compare(a.entry.modified),
			cmp.Compare(a.path, b.path),
		)
	})

	hits := make([]Hit, 0, min(limit, len(found)))
	for _, m := range found[:min(limit, len(found))] {
		hits = append(hits, Hit{
			Path:     m.path,
			Title:    m.entry.title,
			Score:    m.score,
			Modified: m.entry.modified,
			Snippets: m.snippets(),
		})
	}
	return hits, nil
}

// parseQuery returns the terms of a query, folded: the words, and the phrases in
// double quotes, which have their white space made single spaces. A term that is
// empty or already there is dropped.
func parseQuery(query string) []string {
	var terms []string
	var current strings.Builder
	flush := func() {
		term := strings.Join(strings.Fields(Fold(current.String())), " ")
		current.Reset()
		if term != "" && !slices.Contains(terms, term) {
			terms = append(terms, term)
		}
	}
	quoted := false
	for _, r := range query {
		switch {
		case r == '"':
			flush()
			quoted = !quoted
		case unicode.IsSpace(r) && !quoted:
			flush()
		default:
			current.WriteRune(r)
		}
	}
	flush()
	return terms
}

// match is a note that matched, with what a Hit needs of it.
type match struct {
	path  string
	entry *noteEntry
	score int
	lines [maxSnippets]lineMatch // the first lines that hold a term
	n     int                    // how many of lines are set
}

// lineMatch is the first match in a line: where it starts in the folded line,
// and how long it is, in bytes.
type lineMatch struct{ line, at, length int }

// find matches the terms, which are folded, against the note at path. counts is
// room for a number for each term.
func (e *noteEntry) find(path string, terms []string, counts []int) (m match, ok bool) {
	clear(counts)
	m = match{path: path, entry: e}
	for i, line := range e.folded {
		first := lineMatch{line: i, at: -1}
		for j, term := range terms {
			at := strings.Index(line, term)
			if at < 0 {
				continue
			}
			counts[j] += 1 + strings.Count(line[at+len(term):], term)
			if first.at < 0 || at < first.at {
				first.at, first.length = at, len(term)
			}
		}
		if first.at >= 0 && m.n < maxSnippets {
			m.lines[m.n] = first
			m.n++
		}
	}
	for j, term := range terms {
		switch {
		case strings.Contains(e.foldedTitle, term):
			m.score += scoreName
		case strings.Contains(e.foldedPath, term):
			m.score += scorePath
		case counts[j] == 0:
			return match{}, false // every term has to be somewhere
		}
		m.score += min(counts[j], maxScoreText)
	}
	return m, true
}

// snippets returns the Snippets of the match, never nil.
func (m match) snippets() []Snippet {
	snippets := make([]Snippet, 0, m.n)
	for _, l := range m.lines[:m.n] {
		snippets = append(snippets, Snippet{
			Line: l.line + 1,
			Text: snippetText(m.entry.lines[l.line], m.entry.folded[l.line], l.at, l.length),
		})
	}
	return snippets
}

// snippetText returns a line as written, or if it is longer than 200 characters
// the 200 around the match, which is at and length bytes into its folded text.
func snippetText(line, folded string, at, length int) string {
	total := utf8.RuneCountInString(line)
	if total <= snippetRunes {
		return strings.TrimSpace(line)
	}
	// Where the match is in the line as written. Fold drops the combining marks
	// of a text that has them as characters of their own, so a character of the
	// folded line is not always the character of the line at the same place.
	start := utf8.RuneCountInString(folded[:at])
	if utf8.RuneCountInString(folded) != total {
		start = originalRune(line, start)
	}
	margin := max(snippetRunes-utf8.RuneCountInString(folded[at:at+length]), 0) / 2
	first := min(max(start-margin, 0), total-snippetRunes)
	from := runeOffset(line, first)
	return strings.TrimSpace(line[from : from+runeOffset(line[from:], snippetRunes)])
}

// originalRune returns the number of the character of line that Fold turns into
// the character number folded of its result.
func originalRune(line string, folded int) int {
	seen, i := 0, 0
	for _, r := range line {
		if seen >= folded && !combiningMarks.Contains(r) {
			return i
		}
		seen += utf8.RuneCountInString(Fold(string(r)))
		i++
	}
	return i
}

// runeOffset returns the byte offset in s of its character number n, or len(s).
func runeOffset(s string, n int) int {
	for i := range s {
		if n == 0 {
			return i
		}
		n--
	}
	return len(s)
}

// NoteMeta is what is known about a note without opening it.
type NoteMeta struct {
	Path, Title string
	Size        int64
	Modified    time.Time
	Tags        []string // never nil
}

// Notes lists the notes of the vault that visible lets through (all of them if
// it is nil), newest first, and by path among notes changed at the same time. It
// lists what Search searches: the notes that can be opened, from the same
// snapshot, so it can be as much as two seconds behind the vault. What is
// returned belongs to the caller.
func (v *Vault) Notes(ctx context.Context, visible Filter) ([]NoteMeta, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entries, err := v.search.current(ctx, v)
	if err != nil {
		return nil, err
	}
	notes := make([]NoteMeta, 0, len(entries))
	for path, e := range entries {
		if e.skipped || (visible != nil && !visible(path)) {
			continue
		}
		notes = append(notes, NoteMeta{
			Path:     path,
			Title:    e.title,
			Size:     e.size,
			Modified: e.modified,
			Tags:     slices.Clone(e.tags),
		})
	}
	slices.SortFunc(notes, func(a, b NoteMeta) int {
		return cmp.Or(b.Modified.Compare(a.Modified), cmp.Compare(a.Path, b.Path))
	})
	return notes, nil
}
