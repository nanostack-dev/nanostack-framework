package search

import (
	"encoding/json"

	dbsearch "github.com/nanostack-dev/nanostack-framework/db/search"
)

// MaxCursorLength bounds the opaque cursor a client may send back. It is
// db/search.MaxCursorLength.
const MaxCursorLength = dbsearch.MaxCursorLength

// The client errors of a search. They are the values of db/search, so
// errors.Is, IsClientError, the error envelope and core/log's LevelFor give one
// answer whichever package a search returns from. Each logs at Debug through
// any wrapping: the API refusing a bad request is not the service failing.
var (
	// ErrInvalidCursor reports a cursor that cannot continue this search: it
	// is malformed, too long, or was issued for a different ordering. Callers
	// map it to a client error (HTTP 400).
	ErrInvalidCursor = dbsearch.ErrInvalidCursor

	// ErrCursorWithOffset reports a request that sets both a cursor and a
	// non-zero offset. A cursor already says where the page starts, so the two
	// cannot be combined. Callers map it to a client error (HTTP 400).
	ErrCursorWithOffset = dbsearch.ErrCursorWithOffset

	// ErrUnknownSort reports a sort field the search cannot order by. A
	// silent fallback to the default order would answer a different question
	// than the client asked, so callers map it to a client error (HTTP 400).
	ErrUnknownSort = dbsearch.ErrUnknownSort
)

// IsClientError reports whether err is a search error the caller caused. It
// is db/search.IsClientError: an invalid cursor, a cursor combined with an
// offset, an unknown sort field, and the page and filter limits of db/search.
// Each answers HTTP 400, so it is the API refusing a bad request, not the
// service failing. db/search.ErrUnscoped is not one.
func IsClientError(err error) bool {
	return dbsearch.IsClientError(err)
}

// Cursor is the decoded form of an opaque pagination token: the ordering it
// was issued for and the last row's value for each ordering column. It has
// the fields of db/search.Cursor.
//
// A cursor is position, not authority. It is neither signed nor encrypted:
// tenant scope and filters always come from the request, so an edited cursor
// can at most move the start of a page inside rows the caller may already
// read. Never put anything into a cursor that the caller may not see.
type Cursor struct {
	// Order identifies the ordering, for example "created_at:DESC,id:DESC".
	// A cursor only continues the ordering it was issued for.
	Order string
	// Values holds one JSON value per ordering column, in order. A JSON null
	// is a SQL NULL.
	Values []json.RawMessage
}

// EncodeCursor builds the opaque token for the row whose ordering values are
// given. It is db/search.EncodeCursor: the tokens are byte for byte the same.
func EncodeCursor(order string, values []any) (string, error) {
	return dbsearch.EncodeCursor(order, values)
}

// DecodeCursor parses a token produced by EncodeCursor. It is
// db/search.DecodeCursor: every failure wraps ErrInvalidCursor.
func DecodeCursor(token string) (Cursor, error) {
	cursor, err := dbsearch.DecodeCursor(token)
	return Cursor(cursor), err
}

// Validate reports whether the pagination can run: a cursor must fit
// MaxCursorLength and cannot be combined with a non-zero offset. The limit and
// offset bounds stay with the struct tags. It is db/search.Pagination.Validate.
func (p Pagination) Validate() error {
	return dbsearch.Pagination(p).Validate()
}
