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

type Result[T any] struct {
	Items []T
	Total int64
	Count int
}

type Pagination struct {
	Limit  int32 `validate:"required,min=1,max=1000"`
	Offset int32 `validate:"min=0"`
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

func (r Request[FilterType, SortType]) WithPagination(
	pagination *Pagination,
) Request[FilterType, SortType] {
	if pagination != nil {
		r.Pagination = *pagination
	} else {
		r.Pagination = Pagination{Limit: defaultLimit, Offset: defaultOffset}
	}
	return r
}
