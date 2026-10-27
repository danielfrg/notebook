package vault

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"strconv"
	"strings"
	"syscall"
)

// trashFolder is the hidden folder that Trash moves entries into.
const trashFolder = ".trash"

// trashTimeLayout is the layout of the UTC time that starts the name of a
// trashed entry.
const trashTimeLayout = "20060102-150405"

// Mkdir creates the folder p, and the folders above it that do not exist yet.
// It changes nothing that is there: if anything is at p, or a file is where one
// of its folders would go, the error is ErrExists. A hidden p, and a symlink at
// p or on the way to it, are ErrInvalidPath. Nothing is created when it fails
// for one of those reasons.
func (v *Vault) Mkdir(p string) error {
	if v.readOnly {
		return ErrReadOnly
	}
	if _, err := CleanPath(p); err != nil {
		return err
	}

	// One change at a time, like Write, so that looking at the path and creating
	// the folder are one step as far as this process is concerned.
	v.mu.Lock()
	defer v.mu.Unlock()

	switch _, err := v.lstatPath(p); {
	case err == nil:
		return ErrExists
	case !errors.Is(err, ErrNotFound):
		return err
	}
	return v.makeFolders(p)
}

// Move renames the note, image or folder at from to to, and creates the folders
// above to that do not exist yet. A folder takes everything in it along. It
// never replaces anything: if anything is at to, even an empty folder, the error
// is ErrExists. A name that differs from from only in case is a rename, even on
// a filesystem that ignores case and so finds from at to.
//
// A note must stay a note and an image an image (the new name ends in .md, or in
// an image extension), because the app lists nothing else, and a folder cannot
// go into itself. Those, a hidden from or to, a symlink at either or on the way
// to it, and a from that is no note, image or folder (a text file the app does
// not list) are ErrInvalidPath. A from with nothing at it is ErrNotFound.
// Nothing is created when the move is refused for one of those reasons; if the
// rename itself then fails, the folders made for it stay. Wikilinks that point
// at from are not rewritten.
func (v *Vault) Move(from, to string) error {
	if v.readOnly {
		return ErrReadOnly
	}
	for _, p := range []string{from, to} {
		if _, err := CleanPath(p); err != nil {
			return err
		}
	}

	// One change at a time: were two moves to find to free together, the second
	// rename would replace what the first put there.
	v.mu.Lock()
	defer v.mu.Unlock()

	src, err := v.lstatPath(from)
	if err != nil {
		return err
	}
	if !isEntry(from, src) {
		return ErrInvalidPath
	}
	if src.IsDir() {
		if strings.HasPrefix(to, from+"/") {
			return ErrInvalidPath // a folder cannot go into itself
		}
	} else if IsNote(from) != IsNote(to) || IsAsset(from) != IsAsset(to) {
		return ErrInvalidPath // under another kind of name the app would not list it
	}

	switch dst, err := v.lstatPath(to); {
	case err == nil:
		// Where the filesystem ignores case, the entry itself is found at a name
		// that differs from its own only in case, and renaming it there is just a
		// rename. Any other name that leads to the same file is a hard link:
		// rename does nothing for those and reports success, so the move would be
		// reported without having happened.
		if from == to || !strings.EqualFold(from, to) || !os.SameFile(src, dst) {
			return ErrExists
		}
	case !errors.Is(err, ErrNotFound):
		return err
	default:
		if dir := path.Dir(to); dir != "." {
			if err := v.makeFolders(dir); err != nil {
				return err
			}
		}
	}
	return v.rename(from, to)
}

// Trash moves the note, image or folder at p into the vault's hidden .trash
// folder, as .trash/<yyyymmdd-hhmmss>-<name>. The time is the current UTC time
// and the name is the last element of p, so entries from different folders
// share the trash. When that name is taken, by an entry of the same name
// trashed in the same second, a number goes before the extension:
// .trash/<time>-a-2.md. Names are cut to fit the 255 bytes a name may have. A
// folder goes with everything in it, and nothing already in the trash is ever
// replaced.
//
// The errors are those of Move for p: ErrInvalidPath for a hidden or symlinked
// path and for anything that is no note, image or folder, ErrNotFound for a path
// with nothing at it. A .trash that is not a folder is an error of the vault,
// not of p.
func (v *Vault) Trash(p string) error {
	if v.readOnly {
		return ErrReadOnly
	}
	if _, err := CleanPath(p); err != nil {
		return err
	}

	// One change at a time: were two trashes of one name to find it free
	// together, the second rename would replace the first entry.
	v.mu.Lock()
	defer v.mu.Unlock()

	info, err := v.lstatPath(p)
	if err != nil {
		return err
	}
	if !isEntry(p, info) {
		return ErrInvalidPath
	}
	if err := v.makeTrash(); err != nil {
		return err
	}
	to, err := v.freeTrashPath(p, info.IsDir())
	if err != nil {
		return err
	}
	return v.rename(p, to)
}

// OpenAsset opens the image at p for reading and returns it with its FileInfo.
// The caller closes the file. p must name a regular file with an image extension
// (IsAsset), and neither it nor a folder on the way to it may be hidden or a
// symlink. A name that is not an image's, and anything that is not a regular
// file (a folder named like an image, a named pipe), are ErrInvalidPath; a path
// with nothing at it is ErrNotFound. It works in a read-only vault.
func (v *Vault) OpenAsset(p string) (*os.File, fs.FileInfo, error) {
	if _, err := CleanPath(p); err != nil {
		return nil, nil, err
	}
	if !IsAsset(p) {
		return nil, nil, ErrInvalidPath
	}
	info, err := v.lstatPath(p)
	if err != nil {
		return nil, nil, err
	}
	// A named pipe must not be opened at all: that blocks until something writes to it.
	if !info.Mode().IsRegular() {
		return nil, nil, ErrInvalidPath
	}
	f, err := v.root.Open(p)
	if err != nil {
		return nil, nil, orNotFound(err)
	}
	info, err = f.Stat()
	if err == nil && !info.Mode().IsRegular() {
		err = ErrInvalidPath // replaced by something else since the Lstat
	}
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	return f, info, nil
}

// makeFolders creates the folder dir and the folders above it that are missing.
// A file where a folder has to go is ErrExists. dir must have passed CleanPath.
func (v *Vault) makeFolders(dir string) error {
	if err := v.root.MkdirAll(dir, 0o755); err != nil {
		if errors.Is(err, fs.ErrExist) || errors.Is(err, syscall.ENOTDIR) {
			return ErrExists
		}
		return err
	}
	return nil
}

// isEntry reports whether the file at p, which info describes, is one the app
// lists, and so one it may move or delete: a folder, a regular note or a regular
// image. A file it does not list is not its to touch.
func isEntry(p string, info fs.FileInfo) bool {
	if info.IsDir() {
		return true
	}
	return info.Mode().IsRegular() && (IsNote(p) || IsAsset(p))
}

// rename moves from to to, which the caller has found free. A non-empty folder
// that has appeared at to since is refused by the system, and that is ErrExists.
// A file that has appeared would be replaced without a word: the vault's lock,
// which callers hold from looking to renaming, rules that out for this process,
// but not for another program that creates it in that instant.
func (v *Vault) rename(from, to string) error {
	err := v.root.Rename(from, to)
	if errors.Is(err, fs.ErrExist) {
		return ErrExists
	}
	return orNotFound(err)
}

// makeTrash makes sure the vault has a trash folder. A symlink is no folder
// here: Lstat does not follow it, and nothing may be moved through it.
func (v *Vault) makeTrash() error {
	info, err := v.root.Lstat(trashFolder)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if err := v.root.Mkdir(trashFolder, 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
			return err
		}
		return nil
	case err != nil:
		return err
	case !info.IsDir():
		return fmt.Errorf("vault %q: %s is not a folder", v.name, trashFolder)
	}
	return nil
}

// freeTrashPath returns the path in the trash folder that Trash gives the entry
// at p, which is a folder if isDir: the current UTC time, a dash and the entry's
// name, with a number before the extension when the first choice is taken. The
// name is cut, without splitting a character, so that the result fits the
// longest name a filesystem allows.
func (v *Vault) freeTrashPath(p string, isDir bool) (string, error) {
	stamp := now().UTC().Format(trashTimeLayout)
	name := path.Base(p)
	ext := ""
	if !isDir { // a folder's name has no extension, whatever dots it has
		ext = path.Ext(name) // ".md" or an image extension: 5 bytes at most
	}
	stem := strings.TrimSuffix(name, ext)
	for n := 1; ; n++ {
		suffix := ""
		if n > 1 {
			suffix = "-" + strconv.Itoa(n)
		}
		room := maxComponentBytes - len(stamp) - 1 - len(suffix) - len(ext)
		candidate := trashFolder + "/" + stamp + "-" + clip(stem, room) + suffix + ext
		// Whatever is there, even a symlink, takes the name: only a free one is used.
		switch _, err := v.root.Lstat(candidate); {
		case errors.Is(err, fs.ErrNotExist):
			return candidate, nil
		case err != nil:
			return "", err
		}
	}
}
