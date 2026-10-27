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
// entry can be opened. The slices are never nil.
type Listing struct {
	Notes   []Entry
	Assets  []Entry
	Folders []string
	// Fingerprint is stable while the listing is unchanged and changes when a
	// note, image or folder is added, removed, renamed or modified (size or mtime).
	Fingerprint string
}

// List walks the vault and lists only what the API can serve: regular files
// that are notes (*.md) or images, and folders, even empty ones. Hidden names,
// other names CleanPath rejects (a backslash, control characters, invalid
// UTF-8, a name over 255 bytes, a path over 512) and symlinks are never listed
// or followed, and nothing is listed beneath a skipped folder.
func (v *Vault) List(ctx context.Context) (Listing, error) {
	// A walk of a deleted vault directory succeeds with nothing in it.
	if err := v.Ping(); err != nil {
		return Listing{}, err
	}
	list := Listing{Notes: []Entry{}, Assets: []Entry{}, Folders: []string{}}
	err := fs.WalkDir(v.root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		if err != nil {
			// Sync tools remove entries while we walk, and a folder this process
			// may not read (lost+found) should not make the rest of the vault
			// unlistable. The vault directory itself must be readable.
			if p != "." && (errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission)) {
				return nil
			}
			return err
		}
		if p == "." {
			return nil
		}
		// Hidden names, and names the API could not serve, are not listed, and
		// neither is anything beneath a folder that has one. A file must return
		// nil: SkipDir for a file would skip its siblings.
		if _, cerr := CleanPath(p); cerr != nil {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			list.Folders = append(list.Folders, p)
			return nil
		}
		isNote := IsNote(p)
		if !isNote && !IsAsset(p) {
			return nil
		}
		// Root.Lstat rather than d.Info(): Info is a plain lstat of the path and
		// would leave the root.
		info, err := v.root.Lstat(p)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil // a symlink (Lstat reports the link itself), pipe, socket or device
		}
		entry := Entry{Path: p, Size: info.Size(), Modified: info.ModTime()}
		if isNote {
			list.Notes = append(list.Notes, entry)
		} else {
			list.Assets = append(list.Assets, entry)
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
