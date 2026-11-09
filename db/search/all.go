package search

import "context"

// Pager is a search that can continue after a cursor. After returns a copy
// that starts after cursor; it never changes the search it is called on.
type Pager[Q any] interface {
	After(cursor string) Q
}

// All reads every page of q through page and returns every item, in order. It
// serves internal batch reads; an API pages instead. It never changes the
// caller's q. It stops at the first error of page, and before each page when
// ctx is done. A search with no match gives an empty slice, not nil.
func All[Q Pager[Q], R any](ctx context.Context, q Q, page func(context.Context, Q) (Result[R], error)) ([]R, error) {
	items := []R{}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		result, err := page(ctx, q)
		if err != nil {
			return nil, err
		}
		items = append(items, result.Items...)
		if !result.HasMore() {
			return items, nil
		}
		q = q.After(result.NextCursor)
	}
}
