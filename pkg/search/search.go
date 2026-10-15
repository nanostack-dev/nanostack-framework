package search

const (
	defaultLimit  = 10
	defaultOffset = 0
)

type SortDirection string

const (
	SortAscending  SortDirection = "ASC"
	SortDescending SortDirection = "DESC"
)

type Sort[FieldType ~string] struct {
	Field     FieldType     `validate:"required"`
	Direction SortDirection `validate:"required,oneof=ASC DESC"`
}

// Result is one page of a search.
type Result[T any] struct {
	Items []T
	// Total counts every match of the filter, not only this page or the rows
	// after the cursor.
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
// page number, and cannot be combined with a cursor; see Validate.
type Pagination struct {
	Limit  int32 `validate:"required,min=1,max=1000"`
	Offset int32 `validate:"min=0"`
	// Cursor is an opaque token from Result.NextCursor. Empty starts at the
	// first row.
	Cursor string `validate:"max=8192"`
}

type Request[FilterType any, SortType ~string] struct {
	Filter         *FilterType
	Sort           []Sort[SortType] `validate:"dive"`
	FullTextSearch *string
	Pagination     Pagination
}

func NewRequest[FilterType any, SortType ~string]() Request[FilterType, SortType] {
	return Request[FilterType, SortType]{
		Sort: []Sort[SortType]{},
		Pagination: Pagination{
			Limit:  defaultLimit,
			Offset: defaultOffset,
		},
	}
}

func (r Request[FilterType, SortType]) One() Request[FilterType, SortType] {
	r.Pagination.Limit = 1
	r.Pagination.Offset = 0
	r.Pagination.Cursor = ""
	return r
}

func (r Request[FilterType, SortType]) WithFilter(filter *FilterType) Request[FilterType, SortType] {
	if filter != nil {
		r.Filter = filter
	} else {
		r.Filter = new(FilterType)
	}
	return r
}

func (r Request[FilterType, SortType]) WithFullTextSearch(value *string) Request[FilterType, SortType] {
	if value != nil {
		r.FullTextSearch = value
	} else {
		r.FullTextSearch = new(string)
	}
	return r
}

func (r Request[FilterType, SortType]) WithSort(
	field *SortType,
	direction *SortDirection,
) Request[FilterType, SortType] {
	sortValue := SortType("created_at")
	if field != nil {
		sortValue = *field
	}
	dir := SortAscending
	if direction != nil {
		dir = *direction
	}
	r.Sort = append(r.Sort, Sort[SortType]{Field: sortValue, Direction: dir})
	return r
}

// WithCursor continues the search after the row a previous page's NextCursor
// points to. It clears the offset, which a cursor replaces.
func (r Request[FilterType, SortType]) WithCursor(cursor string) Request[FilterType, SortType] {
	r.Pagination.Cursor = cursor
	if cursor != "" {
		r.Pagination.Offset = 0
	}
	return r
}

func (r Request[FilterType, SortType]) WithPagination(
	pagination *Pagination,
) Request[FilterType, SortType] {
	if pagination != nil {
		r.Pagination = *pagination
		// A client may send only a cursor; an absent limit is the default,
		// not a validation failure.
		if r.Pagination.Limit == 0 {
			r.Pagination.Limit = defaultLimit
		}
	} else {
		r.Pagination = Pagination{Limit: defaultLimit, Offset: defaultOffset}
	}
	return r
}
