package mcp

import "errors"

const (
	defaultLimit = 20
	maxLimit     = 100
)

var errPaging = errors.New("limit must be 1 to 100 and offset must be nonnegative")

// Paging is what the tools that list take to give a long list in pages.
type Paging struct {
	Limit  *int `json:"limit,omitempty" jsonschema:"Maximum results, 1 to 100 (default 20)"`
	Offset int  `json:"offset,omitempty" jsonschema:"Zero-based result offset"`
}

// bounds says where the page is in a list of total items: the items from start
// up to end, and next, the offset of the page after it, which is nil on the last
// one. A limit that is not 1 to 100 and an offset below 0 are errors, and an
// offset past the end is an empty page.
func (p Paging) bounds(total int) (start, end int, next *int, err error) {
	limit := defaultLimit
	if p.Limit != nil {
		limit = *p.Limit
	}
	if limit < 1 || limit > maxLimit || p.Offset < 0 {
		return 0, 0, nil, errPaging
	}
	start = min(p.Offset, total)
	end = start + min(limit, total-start)
	if end < total {
		after := end
		next = &after
	}
	return start, end, next, nil
}
