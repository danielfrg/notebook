package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/danielfrg/notebook/internal/vault"
	"github.com/danielgtaylor/huma/v2"
)

// VaultInfo is a vault as the API lists it.
type VaultInfo struct {
	Name string `json:"name"`
}

// Listing is everything that a vault shows.
type Listing struct {
	Notes       []vault.Entry `json:"notes"`
	Assets      []vault.Entry `json:"assets" doc:"Images, which are shown and not edited"`
	Folders     []string      `json:"folders" doc:"Every folder, the empty ones too"`
	Fingerprint string        `json:"fingerprint" doc:"Stable while nothing is added, removed, renamed or modified"`
}

type vaultsOutput struct {
	Body struct {
		ReadOnly bool        `json:"readOnly" doc:"True when this instance refuses every change"`
		Vaults   []VaultInfo `json:"vaults" doc:"In the order they are configured"`
	}
}

type filesInput struct {
	VaultParams
	IfNoneMatch string `header:"If-None-Match" doc:"The ETag of a listing that the client has; if it is still the current one, the answer is 304 and has no body"`
}

type filesOutput struct {
	Status int
	ETag   string `header:"ETag" doc:"The fingerprint of the listing, in quotes"`
	Body   Listing
}

func registerVaults(api huma.API, reg *vault.Registry, options Options) {
	huma.Register(api, huma.Operation{
		OperationID: "list-vaults",
		Method:      http.MethodGet,
		Path:        "/api/vaults",
		Summary:     "List the vaults",
	}, func(context.Context, *struct{}) (*vaultsOutput, error) {
		out := &vaultsOutput{}
		out.Body.ReadOnly = options.ReadOnly
		out.Body.Vaults = []VaultInfo{}
		for _, v := range reg.All() {
			out.Body.Vaults = append(out.Body.Vaults, VaultInfo{Name: v.Name()})
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-files",
		Method:      http.MethodGet,
		Path:        "/api/vaults/{vault}/files",
		Summary:     "List the notes, images and folders of a vault",
		Description: "Hidden names and symlinks are never listed. A note that is over 2 MiB or not UTF-8 is listed and cannot be opened.",
		Errors:      []int{http.StatusNotFound},
		Responses:   map[string]*huma.Response{"304": {Description: "The listing is the one in If-None-Match"}},
	}, func(ctx context.Context, in *filesInput) (*filesOutput, error) {
		v, err := find(reg, in.Vault)
		if err != nil {
			return nil, err
		}
		list, err := v.List(ctx)
		if err != nil {
			return nil, problem(err)
		}
		etag := `"` + list.Fingerprint + `"`
		if etagMatches(in.IfNoneMatch, etag) {
			return &filesOutput{Status: http.StatusNotModified, ETag: etag}, nil
		}
		return &filesOutput{Status: http.StatusOK, ETag: etag, Body: Listing{
			Notes:       list.Notes,
			Assets:      list.Assets,
			Folders:     list.Folders,
			Fingerprint: list.Fingerprint,
		}}, nil
	})
}

// find returns the vault called name, or the error that answers a request for
// one that does not exist.
func find(reg *vault.Registry, name string) (*vault.Vault, error) {
	v, ok := reg.Get(name)
	if !ok {
		return nil, huma.Error404NotFound("vault not found")
	}
	return v, nil
}

// etagMatches reports whether an If-None-Match header holds the entity tag etag,
// which is quoted, or "*". The tag counts as it is, weak or not, because a listing
// that has the same fingerprint is the same listing.
func etagMatches(header, etag string) bool {
	for tag := range strings.SplitSeq(header, ",") {
		if tag = strings.TrimSpace(tag); tag == "*" || strings.TrimPrefix(tag, "W/") == etag {
			return true
		}
	}
	return false
}
