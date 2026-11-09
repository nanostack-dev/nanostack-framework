package search

import dbsearch "github.com/nanostack-dev/nanostack-framework/db/search"

// The types of db/search.
type (
	SortDirection             = dbsearch.SortDirection
	Sort[F ~string]           = dbsearch.Sort[F]
	Result[T any]             = dbsearch.Result[T]
	Pagination                = dbsearch.Pagination
	Request[F any, S ~string] = dbsearch.Request[F, S]
	Cursor                    = dbsearch.Cursor
)

// The sort directions of db/search.
const (
	SortAscending  = dbsearch.SortAscending
	SortDescending = dbsearch.SortDescending
)

// MaxCursorLength is db/search.MaxCursorLength.
const MaxCursorLength = dbsearch.MaxCursorLength

// The client errors of db/search.
var (
	ErrInvalidCursor    = dbsearch.ErrInvalidCursor
	ErrCursorWithOffset = dbsearch.ErrCursorWithOffset
	ErrUnknownSort      = dbsearch.ErrUnknownSort
)

// NewRequest is db/search.NewRequest.
func NewRequest[F any, S ~string]() Request[F, S] {
	return dbsearch.NewRequest[F, S]()
}

// IsClientError is db/search.IsClientError.
func IsClientError(err error) bool {
	return dbsearch.IsClientError(err)
}

// EncodeCursor is db/search.EncodeCursor.
func EncodeCursor(order string, values []any) (string, error) {
	return dbsearch.EncodeCursor(order, values)
}

// DecodeCursor is db/search.DecodeCursor.
func DecodeCursor(token string) (Cursor, error) {
	return dbsearch.DecodeCursor(token)
}
