package vault

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

const (
	// snapshotMaxAge is how long after the end of a refresh the next search
	// refreshes again. It is measured from the end, not from the start, so that a
	// refresh that takes seconds on a big vault is not followed at once by another.
	snapshotMaxAge = 2 * time.Second

	// loadWorkers is how many notes of one folder are read at the same time. What
	// a read waits for is the open, not the processor, and on a network share or
	// a Mac with security software it is long.
	loadWorkers = 8
)

// searchCache is the snapshot of the notes that Search and Notes work on, in
// memory and nowhere else: there is no index to keep up. A refresh walks the
// vault and reads only the notes whose size or modification time is not what the
// snapshot has, so one that finds nothing changed costs a stat of each note.
type searchCache struct {
	mu      sync.Mutex
	loaded  bool                  // a refresh has succeeded
	at      time.Time             // when the last refresh finished, by the clock of the package
	entries map[string]*noteEntry // by path in the vault; a refresh replaces it, it never changes
}

// noteEntry is what the snapshot knows about a note. An entry never changes
// once it is made, so a search can go on with it while a refresh replaces it.
type noteEntry struct {
	size     int64
	modified time.Time
	// skipped is a note that cannot be opened (over 2 MiB, or not UTF-8). It is
	// kept, with its stat, so that it is not read again and again, and is left
	// out of every result.
	skipped bool

	title       string   // the file name without ".md"
	foldedTitle string   // Fold of title
	foldedPath  string   // Fold of the path without ".md"
	lines       []string // the text by lines, as written: what a snippet shows
	folded      []string // Fold of each of lines: what a search matches on
	tags        []string // Tags of the text, never nil
}

// current returns the notes of the vault as the last refresh saw them. If there
// has been none, or snapshotMaxAge has passed since the last one ended, it
// refreshes first. So a change can take longer than snapshotMaxAge to show up:
// that wait starts when the last refresh ended, not when the change was made,
// and a refresh takes time too (seconds, for thousands of notes on a slow
// disk). The refresh is done by one caller at a time, so a crowd of searches
// makes one walk and not many. If the refresh fails the old snapshot stays as it
// was.
func (c *searchCache) current(ctx context.Context, v *Vault) (map[string]*noteEntry, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// A clock that was set back counts as old, rather than as young for as long
	// as it takes to catch up.
	if age := now().Sub(c.at); !c.loaded || age < 0 || age >= snapshotMaxAge {
		entries, err := v.refresh(ctx, c.entries)
		if err != nil {
			return nil, err
		}
		c.entries, c.at, c.loaded = entries, now(), true
	}
	return c.entries, nil
}

// refresh walks the vault and returns its notes, taking the ones whose stat is
// what old has from old and reading the others.
func (v *Vault) refresh(ctx context.Context, old map[string]*noteEntry) (map[string]*noteEntry, error) {
	entries := make(map[string]*noteEntry, len(old))
	err := v.walk(ctx, nil, func(dir *os.Root, files []walkedFile) error {
		var changed []walkedFile
		for _, f := range files {
			if !IsNote(f.path) {
				continue
			}
			if e := old[f.path]; e != nil && e.size == f.info.Size() && e.modified.Equal(f.info.ModTime()) {
				entries[f.path] = e
				continue
			}
			changed = append(changed, f)
		}
		loaded, err := loadNotes(ctx, dir, changed)
		for i, e := range loaded {
			if e != nil {
				entries[changed[i].path] = e
			}
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return entries, nil
}

// loadNotes reads the notes, which are in the folder that dir is open on, a few
// at a time, and returns their entries in the same order. The entry of a note
// that is to be skipped (see loadNote) is nil. The first note that fails makes
// the others stop, and its error is returned.
func loadNotes(ctx context.Context, dir *os.Root, files []walkedFile) ([]*noteEntry, error) {
	entries := make([]*noteEntry, len(files))
	errs := make([]error, len(files))
	var next atomic.Int64
	var failed atomic.Bool
	var wg sync.WaitGroup
	for range min(loadWorkers, len(files)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil && !failed.Load() {
				i := int(next.Add(1)) - 1
				if i >= len(files) {
					return
				}
				if entries[i], errs[i] = loadNote(dir, files[i]); errs[i] != nil {
					failed.Store(true)
				}
			}
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return entries, ctx.Err()
}

// loadNote reads one note through the root of its folder. A note that is to be
// skipped has a nil entry and no error: it is gone, this process may not read
// it, or it is no regular file any more because it was replaced since the walk
// by a folder or a link. That is not remembered, so the next refresh tries
// again, and a note that was made readable shows up though nothing a stat can
// see has changed. A note that can be read but is not one (see
// noteEntry.skipped) is remembered. Any other error (a failing disk, no file
// descriptors left) is returned, as an error about the path of the note in the
// vault, and fails the refresh: such a note must not vanish from the results
// while List still shows it.
func loadNote(dir *os.Root, f walkedFile) (*noteEntry, error) {
	entry := &noteEntry{size: f.info.Size(), modified: f.info.ModTime()}
	if entry.size > MaxNoteBytes {
		entry.skipped = true
		return entry, nil
	}
	file, err := dir.Open(f.name)
	if err != nil {
		return nil, unreadable(dir, f, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, unreadable(dir, f, err)
	}
	if !info.Mode().IsRegular() {
		return nil, nil // replaced since the walk
	}
	// The limit is enforced again on what is read: the note may have grown.
	data, err := io.ReadAll(io.LimitReader(file, MaxNoteBytes+1))
	if err != nil {
		return nil, unreadable(dir, f, err)
	}
	// The stat of the open note goes with what was read from it: if the note
	// changed since the walk, the next refresh sees that and reads it again.
	entry.size, entry.modified = info.Size(), info.ModTime()
	if len(data) > MaxNoteBytes || !utf8.Valid(data) {
		entry.skipped = true
		return entry, nil
	}
	entry.fill(f.path, string(data))
	return entry, nil
}

// unreadable decides what err, which came from reading the note f in the folder
// that dir is open on, means. It returns nil if the note is to be skipped (see
// loadNote), and otherwise err, about the path of the note in the vault.
func unreadable(dir *os.Root, f walkedFile, err error) error {
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) {
		return nil
	}
	// Replaced since the walk by a link that leaves the vault, which os.Root
	// refuses to follow, or by something else that cannot be read like a note.
	switch info, lerr := dir.Lstat(f.name); {
	case errors.Is(lerr, fs.ErrNotExist), errors.Is(lerr, fs.ErrPermission):
		return nil
	case lerr == nil && !info.Mode().IsRegular():
		return nil
	}
	return withPath(err, f.path)
}

// fill makes the entry of the note at path, which has this text.
func (e *noteEntry) fill(path, text string) {
	e.title = Title(path)
	e.foldedTitle = Fold(e.title)
	e.foldedPath = Fold(strings.TrimSuffix(path, ".md"))
	e.lines = splitLines(strings.TrimPrefix(text, byteOrderMark))
	e.folded = make([]string, len(e.lines))
	for i, line := range e.lines {
		e.folded[i] = Fold(line)
	}
	e.tags = Tags(text)
}

// splitLines splits text into lines as the editor does, so that a line has the
// same number in a result as in the editor: LF, CRLF and a bare CR each end one.
// The last line is what follows the last break, and is empty if the text ends
// with one.
func splitLines(text string) []string {
	if strings.IndexByte(text, '\r') < 0 {
		return strings.Split(text, "\n")
	}
	var lines []string
	start := 0
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '\n':
			lines = append(lines, text[start:i])
			start = i + 1
		case '\r':
			lines = append(lines, text[start:i])
			if i+1 < len(text) && text[i+1] == '\n' {
				i++
			}
			start = i + 1
		}
	}
	return append(lines, text[start:])
}
