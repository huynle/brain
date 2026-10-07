package brain

import (
	"context"
	"iter"
	"math"
	"net/url"
	"strconv"
)

// Iterate walks legacy offset pages without treating the page-local Total as a
// collection total. Filters are captured at construction. Truncation, offset
// mismatch and the 10,000-page ceiling are errors, never silent completion.
// This is not a snapshot: concurrent server changes can move entries between pages.
func (s EntriesService) Iterate(ctx context.Context, p *EntriesListParams) iter.Seq2[BrainEntry, error] {
	encoded := listQuery(p).Encode() // Capture values now, not caller-owned pointers.
	return func(yield func(BrainEntry, error) bool) {
		q, err := url.ParseQuery(encoded)
		if err != nil {
			yield(BrainEntry{}, &Error{Code: "invalid_pagination"})
			return
		}
		limit, _ := strconv.Atoi(q.Get("limit"))
		if limit == 0 {
			limit = 100
		}
		offset, _ := strconv.Atoi(q.Get("offset"))
		if limit < 1 || offset < 0 {
			yield(BrainEntry{}, &Error{Code: "invalid_pagination"})
			return
		}
		q.Set("limit", strconv.Itoa(limit))
		for page := 0; page < 10000; page++ {
			q.Set("offset", strconv.Itoa(offset))
			resp, err := result[ListEntriesResponse](s.c, ctx, "GET", "/entries", nil, q, RequestOptions{})
			if err != nil {
				yield(BrainEntry{}, err)
				return
			}
			if resp.Truncated != nil && *resp.Truncated {
				yield(BrainEntry{}, &Error{Code: "pagination_incomplete"})
				return
			}
			if resp.Offset != offset || resp.Limit != limit {
				yield(BrainEntry{}, &Error{Code: "invalid_pagination"})
				return
			}
			if resp.Entries == nil || len(*resp.Entries) == 0 {
				return
			}
			if len(*resp.Entries) > limit {
				yield(BrainEntry{}, &Error{Code: "invalid_pagination"})
				return
			}
			for _, entry := range *resp.Entries {
				if err := s.c.ctx.Err(); err != nil {
					yield(BrainEntry{}, err)
					return
				}
				if err := ctx.Err(); err != nil {
					yield(BrainEntry{}, err)
					return
				}
				if !yield(entry, nil) {
					return
				}
			}
			if offset > math.MaxInt-limit {
				yield(BrainEntry{}, &Error{Code: "invalid_pagination"})
				return
			}
			offset += limit
		}
		yield(BrainEntry{}, &Error{Code: "pagination_limit"})
	}
}
