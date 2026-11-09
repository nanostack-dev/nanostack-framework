package search

// The page that NewRequest and WithPagination select when the caller gives no
// limit.
const (
	defaultLimit  = 10
	defaultOffset = 0
)

// Request is the request of a search that has no SQL builder: a remote call,
// data in memory, or a query that nanostack gen repo cannot declare. A search that
// nanostack gen repo generates takes its own builder, not a Request.
//
// F is the filter type of the search and S its sort field type. The validate
// tags stay, so a hand-written search can validate the request struct.
type Request[F any, S ~string] struct {
	Filter         *F
	Sort           []Sort[S] `validate:"dive"`
	FullTextSearch *string
	Pagination     Pagination
}

// NewRequest returns a request with no filter, no sort and the first page of
// the default limit (10).
func NewRequest[F any, S ~string]() Request[F, S] {
	return Request[F, S]{
		Sort: []Sort[S]{},
		Pagination: Pagination{
			Limit:  defaultLimit,
			Offset: defaultOffset,
		},
	}
}

// One returns a copy of r that reads one row from the first row.
func (r Request[F, S]) One() Request[F, S] {
	r.Pagination.Limit = 1
	r.Pagination.Offset = 0
	r.Pagination.Cursor = ""
	return r
}

// WithFilter returns a copy of r with the filter. A nil filter sets the zero
// filter.
func (r Request[F, S]) WithFilter(filter *F) Request[F, S] {
	if filter != nil {
		r.Filter = filter
	} else {
		r.Filter = new(F)
	}
	return r
}

// WithFullTextSearch returns a copy of r with the full-text term. A nil term
// sets the empty term.
func (r Request[F, S]) WithFullTextSearch(value *string) Request[F, S] {
	if value != nil {
		r.FullTextSearch = value
	} else {
		r.FullTextSearch = new(string)
	}
	return r
}

// WithSort returns a copy of r with one more sort key. A nil field is
// "created_at" and a nil direction is SortAscending.
func (r Request[F, S]) WithSort(field *S, direction *SortDirection) Request[F, S] {
	sortValue := S("created_at")
	if field != nil {
		sortValue = *field
	}
	dir := SortAscending
	if direction != nil {
		dir = *direction
	}
	r.Sort = append(r.Sort, Sort[S]{Field: sortValue, Direction: dir})
	return r
}

// WithCursor continues the search after the row a previous page's NextCursor
// points to. It clears the offset, which a cursor replaces.
func (r Request[F, S]) WithCursor(cursor string) Request[F, S] {
	r.Pagination.Cursor = cursor
	if cursor != "" {
		r.Pagination.Offset = 0
	}
	return r
}

// WithPagination returns a copy of r with the page. A nil page is the first
// page of the default limit, and a page with no limit gets the default limit:
// a client may send only a cursor.
func (r Request[F, S]) WithPagination(pagination *Pagination) Request[F, S] {
	if pagination != nil {
		r.Pagination = *pagination
		if r.Pagination.Limit == 0 {
			r.Pagination.Limit = defaultLimit
		}
	} else {
		r.Pagination = Pagination{Limit: defaultLimit, Offset: defaultOffset}
	}
	return r
}
