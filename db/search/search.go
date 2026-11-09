package search

import "fmt"

// Limits of a search. A product's API contract may set lower limits; the
// runtime never serves more than these.
const (
	// MaxLimit is the largest page a search serves.
	MaxLimit = 1000
	// MaxCursorLength bounds the opaque cursor a client may send back. A
	// cursor carries one value per ordering column, so a few hundred bytes is
	// typical. The bound leaves room for several text sort values of 1,000
	// characters each, which is why a sortable text column must be bounded
	// (VARCHAR(255) or a validated length): a longer value would issue a
	// cursor that DecodeCursor refuses.
	MaxCursorLength = 8192
	// MaxInValues is the largest number of values of one set filter (In,
	// InFold or ContainsAny). More values are ErrTooManyValues. PostgreSQL
	// binds at most 65535 parameters per statement.
	MaxInValues = 1000
	// MaxTermLength is the largest number of characters of one text term
	// (Contains). A longer term is ErrTermTooLong.
	MaxTermLength = 1000
)

// SortDirection is the direction of one sort key.
type SortDirection string

// The sort directions. They are the wire values of the API.
const (
	SortAscending  SortDirection = "ASC"
	SortDescending SortDirection = "DESC"
)

// Sort is one requested sort key. The validate tags stay for hand-written
// searches that validate their request structs.
type Sort[F ~string] struct {
	Field     F             `validate:"required"`
	Direction SortDirection `validate:"required,oneof=ASC DESC"`
}

// Result is one page of a search.
type Result[T any] struct {
	Items []T
	// Total counts every match of the filters, not only this page or the
	// rows after the cursor.
	Total int64
	// Count is len(Items).
	Count int
	// NextCursor continues the search after the last item. It is empty when
	// no rows follow, so an empty NextCursor is the end of the iteration.
	NextCursor string
}

// HasMore reports whether a following page exists.
func (r Result[T]) HasMore() bool {
	return r.NextCursor != ""
}

// Pagination selects one page. Cursor is the standard: pass the previous
// result's NextCursor to continue. Offset remains for clients that jump to a
// page number, and cannot be combined with a cursor.
type Pagination struct {
	Limit  int32 `json:"limit"  validate:"required,min=1,max=1000"`
	Offset int32 `json:"offset" validate:"min=0"`
	// Cursor is an opaque token from Result.NextCursor. Empty starts at the
	// first row.
	Cursor string `json:"cursor,omitempty" validate:"max=8192"`
}

// Validate reports whether the cursor of the page can run: a cursor must fit
// MaxCursorLength and cannot be combined with a non-zero offset. It does not
// check the limit and the offset; CheckPage does.
func (p Pagination) Validate() error {
	if p.Cursor == "" {
		return nil
	}
	if p.Offset > 0 {
		return ErrCursorWithOffset
	}
	if len(p.Cursor) > MaxCursorLength {
		return fmt.Errorf("%w: longer than %d bytes", ErrInvalidCursor, MaxCursorLength)
	}
	return nil
}

// CheckPage reports whether a search can serve the page. It refuses a limit
// outside 1 to MaxLimit (ErrInvalidLimit) and a negative offset
// (ErrInvalidOffset), then applies Validate. An HTTP request passes the API
// contract first; CheckPage applies the same rule to a Go caller.
func CheckPage(p Pagination) error {
	if p.Limit < 1 || p.Limit > MaxLimit {
		return fmt.Errorf("%w: %d is not between 1 and %d", ErrInvalidLimit, p.Limit, MaxLimit)
	}
	if p.Offset < 0 {
		return fmt.Errorf("%w: %d is negative", ErrInvalidOffset, p.Offset)
	}
	return p.Validate()
}
