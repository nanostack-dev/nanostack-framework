package transactor

import (
	dbsearch "github.com/nanostack-dev/nanostack-framework/db/search"
	"github.com/nanostack-dev/nanostack-framework/pkg/search"
)

// The transactor API takes and returns the types of pkg/search, and db/keyset
// takes and returns the types of db/search. They have the same fields. These
// functions copy one into the other, field by field, until pkg/search becomes
// aliases of db/search; then they go away.

func toDBSorts[S ~string](sorts []search.Sort[S]) []dbsearch.Sort[S] {
	if sorts == nil {
		return nil
	}
	out := make([]dbsearch.Sort[S], len(sorts))
	for i, sort := range sorts {
		out[i] = dbsearch.Sort[S]{Field: sort.Field, Direction: dbsearch.SortDirection(sort.Direction)}
	}
	return out
}

func toDBPagination(page search.Pagination) dbsearch.Pagination {
	return dbsearch.Pagination{Limit: page.Limit, Offset: page.Offset, Cursor: page.Cursor}
}

func fromDBResult[R any](result dbsearch.Result[R]) search.Result[R] {
	return search.Result[R]{
		Items:      result.Items,
		Total:      result.Total,
		Count:      result.Count,
		NextCursor: result.NextCursor,
	}
}
