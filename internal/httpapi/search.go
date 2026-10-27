package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/danielfrg/notebook/internal/vault"
	"github.com/danielgtaylor/huma/v2"
)

// maxHits is the most that the search endpoint returns, and what it returns when
// it is not asked for fewer. vault.Search itself has no such limit, because MCP
// pages through more.
const maxHits = 50

// SearchHit is a note that a search found.
type SearchHit struct {
	Path     string          `json:"path"`
	Title    string          `json:"title" doc:"The name of the file without .md"`
	Score    int             `json:"score" doc:"Higher ranks first: a term in the name counts more than one in the path, and that more than one in the text"`
	Modified time.Time       `json:"modified"`
	Snippets []vault.Snippet `json:"snippets" doc:"The first lines that hold a term, as they are written; none for a note that was found by its path alone"`
}

type searchInput struct {
	VaultParams
	Query string `query:"q" doc:"Words, or a \"phrase\" in quotes. Every one has to match, in the path or in the text, and case and accents do not matter. An empty query is invalid"`
	Limit int    `query:"limit" default:"50" doc:"The most hits to return, and never more than 50: a value above 50, or not above 0, is 50"`
}

type searchOutput struct {
	Body struct {
		Hits []SearchHit `json:"hits" doc:"Best first"`
	}
}

func registerSearch(api huma.API, reg *vault.Registry) {
	huma.Register(api, huma.Operation{
		OperationID: "search-notes",
		Method:      http.MethodGet,
		Path:        "/api/vaults/{vault}/search",
		Summary:     "Search the notes of a vault",
		Errors:      []int{http.StatusBadRequest, http.StatusNotFound},
	}, func(ctx context.Context, in *searchInput) (*searchOutput, error) {
		v, err := find(reg, in.Vault)
		if err != nil {
			return nil, err
		}
		limit := in.Limit
		if limit <= 0 || limit > maxHits {
			limit = maxHits
		}
		hits, err := v.Search(ctx, in.Query, vault.SearchOptions{Limit: limit})
		if err != nil {
			return nil, problem(err)
		}
		out := &searchOutput{}
		out.Body.Hits = make([]SearchHit, 0, len(hits))
		for _, h := range hits {
			out.Body.Hits = append(out.Body.Hits, SearchHit{
				Path:     h.Path,
				Title:    h.Title,
				Score:    h.Score,
				Modified: h.Modified,
				Snippets: h.Snippets,
			})
		}
		return out, nil
	})
}
