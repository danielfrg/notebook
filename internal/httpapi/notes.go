package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/danielfrg/notebook/internal/vault"
	"github.com/danielgtaylor/huma/v2"
)

// bodyLimit is what Huma is told to accept of a request body. The guard has
// already held back what is over maxRequestBytes, and Huma refuses a body that is
// as long as its limit, so the limit is one more.
const bodyLimit = maxRequestBytes + 1

// VaultParams is the path parameter of every operation on one vault. It is
// exported because Huma does not read the parameters of embedded types that are
// not.
type VaultParams struct {
	Vault string `path:"vault" doc:"Name of the vault"`
}

// EntryParams adds the path of an entry of the vault. An empty path is invalid,
// and so is one where a name is empty or starts with a dot, and one with a
// backslash or a control character.
type EntryParams struct {
	VaultParams
	Path string `query:"path" doc:"Path in the vault, with / between the names, for example Folder/Note.md"`
}

// Note is a note as the API sends it. Content is the text of the file as it is.
type Note struct {
	Path     string    `json:"path"`
	Content  string    `json:"content"`
	Revision string    `json:"revision" doc:"Lowercase hex SHA-256 of the file. A save has to send the one that was loaded"`
	Size     int64     `json:"size" doc:"Bytes"`
	Modified time.Time `json:"modified"`
}

func newNote(n vault.Note) Note {
	return Note{Path: n.Path, Content: n.Content, Revision: n.Revision, Size: n.Size, Modified: n.Modified}
}

// RevisionInfo is what is polled to see whether a note has changed.
type RevisionInfo struct {
	Revision string    `json:"revision"`
	Modified time.Time `json:"modified"`
}

// PathBody is the answer to an operation that makes a path.
type PathBody struct {
	Path string `json:"path"`
}

type (
	noteOutput     struct{ Body Note }
	revisionOutput struct{ Body RevisionInfo }
	pathOutput     struct{ Body PathBody }

	putNoteInput struct {
		EntryParams
		Body struct {
			Content  string `json:"content" doc:"The whole text of the note, as it is to be written"`
			Revision string `json:"revision" doc:"The revision that was loaded"`
		}
	}
	createNoteInput struct {
		VaultParams
		Body struct {
			Path    string `json:"path" doc:"Path of the new note, which ends in .md"`
			Content string `json:"content,omitempty" doc:"Its text; empty if left out"`
		}
	}
	createFolderInput struct {
		VaultParams
		Body struct {
			Path string `json:"path" doc:"Path of the new folder"`
		}
	}
	moveInput struct {
		VaultParams
		Body struct {
			From string `json:"from" doc:"Path of the entry"`
			To   string `json:"to" doc:"The path that it gets"`
		}
	}
)

func registerNotes(api huma.API, reg *vault.Registry) {
	huma.Register(api, huma.Operation{
		OperationID: "get-note",
		Method:      http.MethodGet,
		Path:        "/api/vaults/{vault}/note",
		Summary:     "Read a note",
		Description: "The content is the text of the file exactly as it is: line endings and a BOM are kept. A note that is over 2 MiB is 413, and one that is not UTF-8 is 415.",
		Errors:      []int{http.StatusBadRequest, http.StatusNotFound, http.StatusRequestEntityTooLarge, http.StatusUnsupportedMediaType},
	}, func(_ context.Context, in *EntryParams) (*noteOutput, error) {
		v, err := find(reg, in.Vault)
		if err != nil {
			return nil, err
		}
		n, err := v.Read(in.Path)
		if err != nil {
			return nil, problem(err)
		}
		return &noteOutput{Body: newNote(n)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-revision",
		Method:      http.MethodGet,
		Path:        "/api/vaults/{vault}/revision",
		Summary:     "Read the revision of a note without its content",
		Description: "The revision and modification time that reading the note would report, to poll for a change. It has the errors of reading the note.",
		Errors:      []int{http.StatusBadRequest, http.StatusNotFound, http.StatusRequestEntityTooLarge, http.StatusUnsupportedMediaType},
	}, func(_ context.Context, in *EntryParams) (*revisionOutput, error) {
		v, err := find(reg, in.Vault)
		if err != nil {
			return nil, err
		}
		revision, modified, err := v.CurrentRevision(in.Path)
		if err != nil {
			return nil, problem(err)
		}
		return &revisionOutput{Body: RevisionInfo{Revision: revision, Modified: modified}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:  "put-note",
		Method:       http.MethodPut,
		Path:         "/api/vaults/{vault}/note",
		Summary:      "Save a note",
		Description:  "Replaces the text of the note, which must still have the revision that was loaded. Otherwise nothing is written and the answer is 409 with the revision on disk now in errors[0].value. A note that was deleted since it was loaded is 404 and is not made again.",
		MaxBodyBytes: bodyLimit,
		Errors: []int{
			http.StatusBadRequest, http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusConflict,
			http.StatusRequestEntityTooLarge, http.StatusUnsupportedMediaType,
		},
	}, func(_ context.Context, in *putNoteInput) (*noteOutput, error) {
		v, err := find(reg, in.Vault)
		if err != nil {
			return nil, err
		}
		n, err := v.Write(in.Path, in.Body.Content, in.Body.Revision)
		if err != nil {
			return nil, problem(err)
		}
		return &noteOutput{Body: newNote(n)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "create-note",
		Method:        http.MethodPost,
		Path:          "/api/vaults/{vault}/notes",
		Summary:       "Create a note",
		Description:   "Makes the note and the folders above it. It never replaces anything.",
		DefaultStatus: http.StatusCreated,
		MaxBodyBytes:  bodyLimit,
		Errors: []int{
			http.StatusBadRequest, http.StatusMethodNotAllowed, http.StatusConflict,
			http.StatusRequestEntityTooLarge, http.StatusUnsupportedMediaType,
		},
	}, func(_ context.Context, in *createNoteInput) (*noteOutput, error) {
		v, err := find(reg, in.Vault)
		if err != nil {
			return nil, err
		}
		n, err := v.Create(in.Body.Path, in.Body.Content)
		if err != nil {
			return nil, problem(err)
		}
		return &noteOutput{Body: newNote(n)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "create-folder",
		Method:        http.MethodPost,
		Path:          "/api/vaults/{vault}/folders",
		Summary:       "Create a folder",
		Description:   "Makes the folder and the folders above it that are missing. It never changes what is there.",
		DefaultStatus: http.StatusCreated,
		MaxBodyBytes:  bodyLimit,
		Errors:        []int{http.StatusBadRequest, http.StatusMethodNotAllowed, http.StatusConflict},
	}, func(_ context.Context, in *createFolderInput) (*pathOutput, error) {
		v, err := find(reg, in.Vault)
		if err != nil {
			return nil, err
		}
		if err := v.Mkdir(in.Body.Path); err != nil {
			return nil, problem(err)
		}
		return &pathOutput{Body: PathBody{Path: in.Body.Path}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:  "move-entry",
		Method:       http.MethodPost,
		Path:         "/api/vaults/{vault}/move",
		Summary:      "Move or rename a note, an image or a folder",
		Description:  "Makes the folders above the new path that are missing. It never replaces anything, a note stays a note and an image an image, and links to it are not rewritten.",
		MaxBodyBytes: bodyLimit,
		Errors:       []int{http.StatusBadRequest, http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusConflict},
	}, func(_ context.Context, in *moveInput) (*pathOutput, error) {
		v, err := find(reg, in.Vault)
		if err != nil {
			return nil, err
		}
		if err := v.Move(in.Body.From, in.Body.To); err != nil {
			return nil, problem(err)
		}
		return &pathOutput{Body: PathBody{Path: in.Body.To}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "delete-entry",
		Method:        http.MethodDelete,
		Path:          "/api/vaults/{vault}/entry",
		Summary:       "Delete a note, an image or a folder",
		Description:   "Moves it, with what is in it, to .trash/<UTC yyyymmdd-hhmmss>-<name> in the vault, which is hidden. Nothing is erased.",
		DefaultStatus: http.StatusNoContent,
		Errors:        []int{http.StatusBadRequest, http.StatusNotFound, http.StatusMethodNotAllowed},
	}, func(_ context.Context, in *EntryParams) (*struct{}, error) {
		v, err := find(reg, in.Vault)
		if err != nil {
			return nil, err
		}
		if err := v.Trash(in.Path); err != nil {
			return nil, problem(err)
		}
		return nil, nil
	})
}
