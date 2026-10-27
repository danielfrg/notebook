package vault

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

// Note is a note as it is on disk. Content is the exact text of the file: line
// endings, a BOM, trailing spaces and a missing final newline are all kept.
type Note struct {
	Path     string
	Content  string
	Revision string
	Size     int64
	Modified time.Time
}

// Revision returns the revision of a note with this content: the lowercase hex
// SHA-256 of its bytes.
func Revision(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// Read returns the note at p. p must name a regular *.md file of at most
// MaxNoteBytes bytes that is valid UTF-8, and neither it nor a folder on the
// way to it may be hidden or a symlink. The errors say which of those it is
// not: ErrInvalidPath, ErrNotNote, ErrTooLarge or ErrNotFound. The text is
// returned exactly as it is on disk.
func (v *Vault) Read(p string) (Note, error) {
	n, _, err := v.readNote(p)
	return n, err
}

// CurrentRevision returns the revision and modification time that Read would
// report for the note at p, so that a note can be polled for changes without
// being sent. It fails with the errors of Read.
func (v *Vault) CurrentRevision(p string) (revision string, modified time.Time, err error) {
	n, _, err := v.readNote(p)
	if err != nil {
		return "", time.Time{}, err
	}
	return n.Revision, n.Modified, nil
}

// Write replaces the note at p with content, provided the note still has
// expectedRevision, the revision the caller loaded. Otherwise nothing is
// written and the error is a *ConflictError (it matches ErrConflict) whose
// Current is the revision on disk now. An empty or malformed expectedRevision
// never matches. A note deleted since it was loaded is ErrNotFound, not a
// conflict, and is not brought back; one that Read would refuse (over
// MaxNoteBytes, not UTF-8) cannot be written over either. Content identical to
// what is on disk is not written: the note is returned as it is and keeps its
// inode and modification time.
//
// The new text goes to a hidden temp file in the note's folder, is synced and
// renamed over the note, so a reader or a crash sees the old note or the new
// one, never a mix. The replacement is a new file: it has the old permission
// bits, but its owner is the process's user and its group is what the system
// gives a new file in that folder (the folder's on BSD and macOS and in setgid
// folders, otherwise the process's). ACLs and extended attributes are not
// carried over, other hard links keep the old text, and a read-only (0444) note
// is replaced anyway when its folder is writable.
//
// Writes through one Vault run one at a time, so two writers holding the same
// revision cannot both win. Other Vault values over the same or overlapping
// directories do not serialize with it, and neither do other programs: one that
// changes the note after the revision was checked and before the rename, by
// replacing it or editing it in place, is overwritten. That window is one write
// and sync wide.
func (v *Vault) Write(p, content, expectedRevision string) (Note, error) {
	if v.readOnly {
		return Note{}, ErrReadOnly
	}
	if err := checkNotePath(p); err != nil {
		return Note{}, err
	}
	if err := checkNoteContent(content); err != nil {
		return Note{}, err
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	current, perm, err := v.readNote(p)
	if err != nil {
		return Note{}, err
	}
	if current.Revision != expectedRevision {
		return Note{}, &ConflictError{Current: current.Revision}
	}
	// A save without edits touches nothing. Replacing the note would give it a
	// new inode and modification time, drop what a replacement does not carry
	// over, and show up as a modification to Obsidian and Syncthing.
	if content == current.Content {
		return current, nil
	}
	modified, err := v.replaceFile(p, content, perm)
	if err != nil {
		return Note{}, err
	}
	return newNote(p, content, modified), nil
}

// Create creates the note at p with content, and the folders above it that do
// not exist yet. It never replaces anything: if a file or folder is at p, or a
// note is where one of its folders would go, the error is ErrExists. Nothing is
// created when the path or the content is refused.
func (v *Vault) Create(p, content string) (Note, error) {
	if v.readOnly {
		return Note{}, ErrReadOnly
	}
	if err := checkNotePath(p); err != nil {
		return Note{}, err
	}
	if err := checkNoteContent(content); err != nil {
		return Note{}, err
	}

	// One change at a time, like Write, so that looking at the path and creating
	// the note are one step as far as this process is concerned.
	v.mu.Lock()
	defer v.mu.Unlock()

	// Anything found at p is in the way, and a symlink anywhere on the path is
	// refused, as it is by every operation. What is missing gets created below.
	switch _, err := v.lstatPath(p); {
	case err == nil:
		return Note{}, ErrExists
	case !errors.Is(err, ErrNotFound):
		return Note{}, err
	}
	if dir := path.Dir(p); dir != "." {
		if err := v.root.MkdirAll(dir, 0o755); err != nil {
			if errors.Is(err, fs.ErrExist) || errors.Is(err, syscall.ENOTDIR) {
				return Note{}, ErrExists // a file is where a folder has to go
			}
			return Note{}, err
		}
	}
	f, err := v.root.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return Note{}, ErrExists
		}
		return Note{}, err
	}
	modified, err := v.fillNew(p, f, content)
	if err != nil {
		return Note{}, err
	}
	return newNote(p, content, modified), nil
}

// fillNew writes content to f, the file that Create has just made at p, syncs
// and closes it, and returns its modification time. If anything fails, the
// half-made note is removed again.
func (v *Vault) fillNew(p string, f *os.File, content string) (time.Time, error) {
	// created identifies the file this call made, to tell it from one that
	// replaces it later.
	created, err := f.Stat()
	if err != nil {
		// O_EXCL made this file a moment ago, so it is ours to remove without
		// comparing it with anything.
		f.Close()
		v.root.Remove(p)
		return time.Time{}, err
	}
	info, err := writeAndSync(f, content)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		// Take the half-written note back, unless something else has replaced it
		// since: that is no longer ours to remove.
		if current, lerr := v.root.Lstat(p); lerr == nil && os.SameFile(created, current) {
			v.root.Remove(p)
		}
		return time.Time{}, err
	}
	return info.ModTime(), nil
}

func newNote(p, content string, modified time.Time) Note {
	return Note{
		Path:     p,
		Content:  content,
		Revision: Revision([]byte(content)),
		Size:     int64(len(content)),
		Modified: modified,
	}
}

// checkNotePath checks that p is a valid vault path that names a note.
func checkNotePath(p string) error {
	if _, err := CleanPath(p); err != nil {
		return err
	}
	if !IsNote(p) {
		return ErrNotNote
	}
	return nil
}

// checkNoteContent checks that content is something a note may hold.
func checkNoteContent(content string) error {
	if len(content) > MaxNoteBytes {
		return ErrTooLarge
	}
	if !utf8.ValidString(content) {
		return ErrNotNote
	}
	return nil
}

// readNote reads and checks the note at p. It also returns the permission bits
// of the file, for Write to hand on to the file that replaces it.
func (v *Vault) readNote(p string) (Note, fs.FileMode, error) {
	if err := checkNotePath(p); err != nil {
		return Note{}, 0, err
	}
	info, err := v.lstatPath(p)
	if err != nil {
		return Note{}, 0, err
	}
	// Anything that is not a regular file is no note, and a named pipe must not
	// be opened at all: that blocks until something writes to it.
	if !info.Mode().IsRegular() {
		return Note{}, 0, ErrNotNote
	}
	f, err := v.root.Open(p)
	if err != nil {
		return Note{}, 0, orNotFound(err)
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil {
		return Note{}, 0, err
	}
	if !info.Mode().IsRegular() {
		return Note{}, 0, ErrNotNote // replaced by something else since the Lstat
	}
	if info.Size() > MaxNoteBytes {
		return Note{}, 0, ErrTooLarge
	}
	// The limit is enforced again on what is read: the file may have grown.
	data, err := io.ReadAll(io.LimitReader(f, MaxNoteBytes+1))
	if err != nil {
		return Note{}, 0, err
	}
	if len(data) > MaxNoteBytes {
		return Note{}, 0, ErrTooLarge
	}
	if !utf8.Valid(data) {
		return Note{}, 0, ErrNotNote
	}
	n := Note{
		Path:     p,
		Content:  string(data),
		Revision: Revision(data),
		Size:     int64(len(data)),
		Modified: info.ModTime(),
	}
	return n, info.Mode().Perm(), nil
}

// lstatPath looks at every component of p through the root, from the top, and
// returns the FileInfo of the last one. os.Root keeps everything inside the
// vault, but it does follow a symlink that stays inside, even in the middle of a
// path, so one Lstat of p would take a note in a symlinked folder for an
// ordinary note. A symlink at any level is ErrInvalidPath. A component that is
// missing, or is a file although the path goes on beneath it, is ErrNotFound.
// p must have passed CleanPath.
func (v *Vault) lstatPath(p string) (fs.FileInfo, error) {
	parts := strings.Split(p, "/")
	var info fs.FileInfo
	for i := range parts {
		var err error
		if info, err = v.root.Lstat(strings.Join(parts[:i+1], "/")); err != nil {
			return nil, orNotFound(err)
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return nil, ErrInvalidPath
		}
	}
	return info, nil
}

// orNotFound turns the errors that say a file, or a folder on the way to it, is
// not there into ErrNotFound, and returns any other error as it is.
func orNotFound(err error) error {
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
		return ErrNotFound
	}
	return err
}

// maxTempBase is how much of a note's name its temp file's name keeps. The temp
// name adds 14 bytes (two dots, 8 hex digits and ".tmp") and names are limited
// to 255 bytes, so a note with a long name still has room for one.
const maxTempBase = 200

// tempSuffix returns the random part of a temp file's name, 8 hex digits. Tests
// replace it to know the name.
var tempSuffix = func() string {
	var random [4]byte
	rand.Read(random[:]) // never fails: it crashes the program if the system has no randomness
	return hex.EncodeToString(random[:])
}

// replaceFile puts content at p, a regular file that exists, by writing a hidden
// temp file next to it, syncing it and renaming it over p. The temp file is
// called .<name of p>.<8 hex digits>.tmp and has mode perm exactly, whatever the
// umask says. It is removed if anything fails. It returns the modification time
// of the new file.
func (v *Vault) replaceFile(p, content string, perm fs.FileMode) (time.Time, error) {
	dir, base := path.Split(p)
	tmpName := dir + "." + clip(base, maxTempBase) + "." + tempSuffix() + ".tmp"

	// O_EXCL, so a file that is already there under that name is never written into.
	tmp, err := v.root.OpenFile(tmpName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return time.Time{}, err
	}
	renamed := false
	defer func() {
		if !renamed {
			tmp.Close()
			v.root.Remove(tmpName)
		}
	}()

	info, err := writeAndSync(tmp, content)
	if err != nil {
		return time.Time{}, err
	}
	if info.Mode().Perm() != perm { // the umask cleared bits of it
		if err := tmp.Chmod(perm); err != nil {
			return time.Time{}, err
		}
	}
	if err := tmp.Close(); err != nil {
		return time.Time{}, err
	}
	if err := v.root.Rename(tmpName, p); err != nil {
		return time.Time{}, err
	}
	renamed = true
	return info.ModTime(), nil
}

// writeAndSync writes content to f and syncs it, and returns the FileInfo of f
// as it is after the sync.
func writeAndSync(f *os.File, content string) (fs.FileInfo, error) {
	if _, err := f.WriteString(content); err != nil {
		return nil, err
	}
	if err := f.Sync(); err != nil {
		return nil, err
	}
	return f.Stat()
}

// clip returns s cut to at most n bytes, without cutting a character in two.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
