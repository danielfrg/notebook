package vault

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"
	"time"
)

// Entry is a note or an image in a vault.
type Entry struct {
	Path     string    `json:"path"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
}

// Listing is everything a vault shows. Paths are slash-separated, relative to
// the vault and sorted by byte order, and all of them pass CleanPath, so every
// path is valid for the API. That does not make every note readable: one over
// 2 MiB or not UTF-8 is listed and refused when it is opened. The slices are
// never nil.
type Listing struct {
	Notes   []Entry
	Assets  []Entry
	Folders []string
	// Fingerprint is stable while the listing is unchanged and changes when a
	// note, image or folder is added, removed, renamed or modified (size or mtime).
	Fingerprint string
}

// List walks the vault and lists the regular files that are notes (*.md) or
// images, and the folders, even empty ones. Hidden names, other names CleanPath
// rejects (a backslash, control characters, invalid UTF-8, a name over 255
// bytes, a path over 512) and symlinks are never listed or followed, and
// nothing is listed beneath a skipped folder, so every listed path is valid for
// the API (it passes CleanPath).
func (v *Vault) List(ctx context.Context) (Listing, error) {
	list := Listing{Notes: []Entry{}, Assets: []Entry{}, Folders: []string{}}
	err := v.walk(ctx,
		func(p string) { list.Folders = append(list.Folders, p) },
		func(_ *os.Root, files []walkedFile) error {
			for _, f := range files {
				entry := Entry{Path: f.path, Size: f.info.Size(), Modified: f.info.ModTime()}
				if IsNote(f.path) {
					list.Notes = append(list.Notes, entry)
				} else {
					list.Assets = append(list.Assets, entry)
				}
			}
			return nil
		})
	if err != nil {
		return Listing{}, err
	}
	byPath := func(a, b Entry) int { return cmp.Compare(a.Path, b.Path) }
	slices.SortFunc(list.Notes, byPath)
	slices.SortFunc(list.Assets, byPath)
	slices.Sort(list.Folders)
	list.Fingerprint = list.fingerprint()
	return list, nil
}

// walkedFile is a note or an image that walk found.
type walkedFile struct {
	path string      // slash-separated, relative to the vault, passes CleanPath
	name string      // the last element of path: what the root of its folder calls it
	info fs.FileInfo // from Lstat, so the file is a regular file and not a link
}

// walk finds what List lists, by walking the vault depth first with the names
// in each folder in byte order, and reports it to its callbacks. folder, which
// may be nil, is called with the path of each folder. files is called once for
// each folder that holds notes or images, after the folders in it have been
// walked, with those files in name order and the os.Root of their folder, which
// is open until files returns and can be used to read them.
//
// Every folder is opened as an os.Root of its own, and the files in it are looked
// at through that root. A Root re-opens every folder on the way to the file on
// each call, so a stat of a note three folders deep through the vault's root
// costs about 150 microseconds on a Mac (the 5,000 notes of a large vault take
// 750 ms), against about 10 for a name in the root of its own folder. A folder
// is entered with the root of its parent, so that nothing leaves the vault.
// fs.DirEntry.Info would leave it: it is a plain lstat of the path.
//
// What goes away while the walk is under way (sync tools rename and delete all
// the time) is skipped, and so is a folder that is replaced by something that is
// not a folder; a folder this process may not open is skipped but still listed.
// The vault directory itself must be readable. An error that is not one of
// those fails the walk, and is about a path in the vault: os.Root names the
// absolute path of the vault in some of its errors, and that is taken out.
func (v *Vault) walk(ctx context.Context, folder func(p string), files func(dir *os.Root, files []walkedFile) error) error {
	// A walk of a deleted vault directory succeeds with nothing in it.
	if err := v.Ping(); err != nil {
		return err
	}
	// Each entry the walk visits is a check of the context, the vault directory
	// being the first.
	if err := ctx.Err(); err != nil {
		return err
	}
	w := walker{ctx: ctx, folder: folder, files: files}
	return w.dir(v.root, "")
}

type walker struct {
	ctx    context.Context
	folder func(p string)
	files  func(dir *os.Root, files []walkedFile) error
}

func (w *walker) listFolder(p string) {
	if w.folder != nil {
		w.folder(p)
	}
}

// dir walks the folder that root is open on, which is at dirPath, or "" for the
// vault directory.
func (w *walker) dir(root *os.Root, dirPath string) error {
	entries, err := fs.ReadDir(root.FS(), ".")
	if dirPath != "" {
		// A folder this process may not read (a root-owned lost+found at the top
		// of a mounted disk) must not make the rest of the vault unlistable.
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return nil
		case errors.Is(err, fs.ErrPermission):
			w.listFolder(dirPath)
			return nil
		}
	}
	if err != nil {
		return withPath(err, cmp.Or(dirPath, "."))
	}
	if dirPath != "" {
		w.listFolder(dirPath)
	}

	var found []walkedFile
	for _, e := range entries {
		if err := w.ctx.Err(); err != nil {
			return err
		}
		name := e.Name()
		p := name
		if dirPath != "" {
			p = dirPath + "/" + name
		}
		// Hidden names, and names the API could not serve, are not listed, and
		// neither is anything beneath a folder that has one.
		if _, err := CleanPath(p); err != nil {
			continue
		}
		if e.IsDir() {
			if err := w.enter(root, name, p); err != nil {
				return err
			}
			continue
		}
		if !IsNote(p) && !IsAsset(p) {
			continue
		}
		info, err := root.Lstat(name)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			continue // removed since the folder was read
		case errors.Is(err, fs.ErrPermission):
			continue // a folder that can be read but not searched (mode 0o444)
		case err != nil:
			return withPath(err, p)
		}
		if !info.Mode().IsRegular() {
			continue // a symlink (Lstat reports the link itself), pipe, socket or device
		}
		found = append(found, walkedFile{path: p, name: name, info: info})
	}
	if len(found) == 0 {
		return nil
	}
	return w.files(root, found)
}

// enter walks the folder called name in parent, which is at p.
func (w *walker) enter(parent *os.Root, name, p string) error {
	child, err := parent.OpenRoot(name)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil // removed since parent was read
	case errors.Is(err, fs.ErrPermission):
		w.listFolder(p) // it is there, but this process may not open it
		return nil
	case err != nil:
		// A folder can be replaced, after parent was read, by a file or by a link
		// that leaves the vault. It has gone away as a folder, like anything else
		// that goes away.
		if current, lerr := parent.Lstat(name); errors.Is(lerr, fs.ErrNotExist) || (lerr == nil && !current.IsDir()) {
			return nil
		}
		return withPath(err, p)
	}
	defer child.Close()

	opened, err := child.Stat(".")
	switch {
	case errors.Is(err, fs.ErrPermission):
		w.listFolder(p) // it can be opened but not searched (mode 0o444): nothing in it can be looked at
		return nil
	case err != nil:
		return withPath(err, p)
	}
	// OpenRoot follows a symlink that stays inside the vault, so a folder swapped
	// for a link since parent was read would otherwise be walked, and the files
	// of the folder it points to listed under its name. It has to be the folder
	// that the name leads to now.
	current, err := parent.Lstat(name)
	if err != nil || !current.IsDir() || !os.SameFile(current, opened) {
		return nil
	}
	return w.dir(child, p)
}

// withPath returns err as an error about p, a path in the vault. Some of the
// errors of os.Root, and of the files it opens, name the absolute path of the
// vault directory (a folder that turns out not to be one, a failed read), and
// that is not for the callers of the vault to see: its errors are about paths in
// the vault.
func withPath(err error, p string) error {
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		return &fs.PathError{Op: pathErr.Op, Path: p, Err: pathErr.Err}
	}
	return fmt.Errorf("%s: %w", p, err)
}

// fingerprint is the SHA-256 of the sorted lines "kind\x00path\x00size\x00mtime\n".
// Folders have size 0 and no mtime: a folder's own mtime changes whenever a
// child is created or removed, which sync tools do all the time.
func (l Listing) fingerprint() string {
	lines := make([]string, 0, len(l.Notes)+len(l.Assets)+len(l.Folders))
	add := func(kind string, entries []Entry) {
		for _, e := range entries {
			lines = append(lines, fmt.Sprintf("%s\x00%s\x00%d\x00%d\n", kind, e.Path, e.Size, e.Modified.UnixNano()))
		}
	}
	add("note", l.Notes)
	add("asset", l.Assets)
	for _, folder := range l.Folders {
		lines = append(lines, fmt.Sprintf("folder\x00%s\x00%d\x00\n", folder, 0))
	}
	slices.Sort(lines)
	h := sha256.New()
	for _, line := range lines {
		io.WriteString(h, line)
	}
	return hex.EncodeToString(h.Sum(nil))
}
