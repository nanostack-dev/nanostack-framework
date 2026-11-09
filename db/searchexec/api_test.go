package searchexec_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/db/search"
	"github.com/nanostack-dev/nanostack-framework/db/searchexec"
)

// apiSort and apiDirection are the enum types of an API body (x-go-type). They are not the
// search's own types, so the tests prove that the helpers convert them.
type (
	apiSort      string
	apiDirection string
)

// sameFilter asserts that got runs the same count statement, with the same arguments, as
// want.
func sameFilter(t *testing.T, want, got itemSearch) {
	t.Helper()
	wantSQL, wantArgs := countSQL(t, t.Context(), want)
	gotSQL, gotArgs := countSQL(t, t.Context(), got)
	assert.Equal(t, wantSQL, gotSQL)
	assert.Equal(t, wantArgs, gotArgs)
}

func TestSomeEmptyListFiltersNothing(t *testing.T) {
	q := newItemSearch(fullScope())

	sameFilter(t, q, searchexec.Some(q, q.Name().In, nil))
	sameFilter(t, q, searchexec.Some(q, q.Name().In, []string{}))
	sameFilter(t, q, searchexec.Some(q, q.Text().ContainsAny, []string{}))
	sameFilter(t, q, searchexec.Some(q, q.Permission().In, nil))

	sameFilter(t, q.Name().In("a", "b"), searchexec.Some(q, q.Name().In, []string{"a", "b"}))
	sameFilter(t, q.Text().ContainsAny("a"), searchexec.Some(q, q.Text().ContainsAny, []string{"a"}))
}

func TestSomeKeepsTheLimitError(t *testing.T) {
	q := newItemSearch(fullScope())
	values := make([]string, search.MaxInValues+1)

	_, err := searchexec.Resolve(t.Context(), itemSpec, searchexec.Some(q, q.Name().In, values).Query)

	require.ErrorIs(t, err, search.ErrTooManyValues)
}

func TestOptNilPointerFiltersNothing(t *testing.T) {
	q := newItemSearch(fullScope())

	sameFilter(t, q, searchexec.Opt(q, q.Owner().Present, (*bool)(nil)))
	sameFilter(t, q, searchexec.Opt(q, q.At().Since, (*time.Time)(nil)))
	sameFilter(t, q, searchexec.Opt(q, q.Text().Contains, (*string)(nil)))
	sameFilter(t, q, searchexec.Opt(q, q.Rank().Eq, (*int64)(nil)))

	present, at, term, rank := false, time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC), "a", int64(3)
	sameFilter(t, q.Owner().Present(false), searchexec.Opt(q, q.Owner().Present, &present))
	sameFilter(t, q.At().Since(at), searchexec.Opt(q, q.At().Since, &at))
	sameFilter(t, q.Text().Contains("a"), searchexec.Opt(q, q.Text().Contains, &term))
	sameFilter(t, q.Rank().Eq(3), searchexec.Opt(q, q.Rank().Eq, &rank))
}

func TestOrZero(t *testing.T) {
	type body struct {
		Names []string
		Term  *string
	}
	term := "a"

	assert.Equal(t, body{}, searchexec.OrZero[body](nil))
	assert.Equal(
		t,
		body{Names: []string{"x"}, Term: &term},
		searchexec.OrZero(&body{Names: []string{"x"}, Term: &term}),
	)
}

// sorts resolves q and returns its sorts, with the spec's default applied.
func sorts(t *testing.T, q itemSearch) []search.Sort[itemSort] {
	t.Helper()
	plan, err := searchexec.Resolve(t.Context(), itemSpec, q.Query)
	require.NoError(t, err)
	return plan.Sorts()
}

func TestAPISort(t *testing.T) {
	q := newItemSearch(fullScope())
	name, rank := apiSort(itemSortName), apiSort(itemSortRank)
	asc, desc := apiDirection(search.SortAscending), apiDirection(search.SortDescending)
	cases := map[string]struct {
		field          *apiSort
		direction      *apiDirection
		fieldDirection search.SortDirection
		want           []search.Sort[itemSort]
	}{
		"neither keeps the default sort": {
			nil, nil, search.SortAscending, []search.Sort[itemSort]{searchexec.Desc(itemSortRank)},
		},
		"sort_by alone sorts in the field direction, DESC": {
			&name, nil, search.SortDescending, []search.Sort[itemSort]{searchexec.Desc(itemSortName)},
		},
		"sort_by alone sorts in the field direction, ASC": {
			&name, nil, search.SortAscending, []search.Sort[itemSort]{searchexec.Asc(itemSortName)},
		},
		"sort_direction alone applies to the default sort": {
			nil, &asc, search.SortDescending, []search.Sort[itemSort]{searchexec.Asc(itemSortRank)},
		},
		"both":           {&name, &asc, search.SortDescending, []search.Sort[itemSort]{searchexec.Asc(itemSortName)}},
		"both, DESC":     {&rank, &desc, search.SortAscending, []search.Sort[itemSort]{searchexec.Desc(itemSortRank)}},
		"both, the same": {&rank, &asc, search.SortDescending, []search.Sort[itemSort]{searchexec.Asc(itemSortRank)}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := searchexec.APISort(q, q.SortBy, tc.field, tc.direction, itemSortRank, tc.fieldDirection)

			assert.Equal(t, tc.want, sorts(t, got))
		})
	}
}

func TestAPISortTakesTheSearchDirectionType(t *testing.T) {
	q := newItemSearch(fullScope())
	name, direction := apiSort(itemSortName), search.SortAscending

	got := searchexec.APISort(q, q.SortBy, &name, &direction, itemSortRank, search.SortDescending)

	assert.Equal(t, []search.Sort[itemSort]{searchexec.Asc(itemSortName)}, sorts(t, got))
}

func TestAPISortLeavesAnUnknownFieldToRun(t *testing.T) {
	q := newItemSearch(fullScope())
	unknown := apiSort("ip_address")
	rec := &recorder{}

	got := searchexec.APISort(q, q.SortBy, &unknown, (*apiDirection)(nil), itemSortRank, search.SortDescending)
	_, err := searchexec.Run(t.Context(), rec, itemSpec, got.Query, identity)

	require.ErrorIs(t, err, search.ErrUnknownSort)
	assert.Empty(t, rec.queries, "no statement runs")
}

func TestAPIDirectionAppliesToTheDefaultSort(t *testing.T) {
	q := newItemSearch(fullScope())
	asc, desc := apiDirection(search.SortAscending), apiDirection(search.SortDescending)

	assert.Equal(t, []search.Sort[itemSort]{searchexec.Desc(itemSortRank)},
		sorts(t, searchexec.APIDirection(q, q.SortBy, itemSortRank, (*apiDirection)(nil))),
		"no direction keeps the default sort")
	assert.Equal(t, []search.Sort[itemSort]{searchexec.Asc(itemSortRank)},
		sorts(t, searchexec.APIDirection(q, q.SortBy, itemSortRank, &asc)))
	assert.Equal(t, []search.Sort[itemSort]{searchexec.Desc(itemSortRank)},
		sorts(t, searchexec.APIDirection(q, q.SortBy, itemSortRank, &desc)))
}

// An ASC default proves that a request with no sort_direction keeps the direction of the
// default sort, and does not pick DESC.
func TestAPISortKeepsAnAscendingDefault(t *testing.T) {
	def := itemDef()
	def.Default = searchexec.Asc(itemSortRank)
	spec := searchexec.DefineSpec(def)
	q := searchexec.New[itemSearch](spec, fullScope())
	resolve := func(q itemSearch) []search.Sort[itemSort] {
		plan, err := searchexec.Resolve(t.Context(), spec, q.Query)
		require.NoError(t, err)
		return plan.Sorts()
	}
	want := []search.Sort[itemSort]{searchexec.Asc(itemSortRank)}

	assert.Equal(t, want, resolve(searchexec.APISort(
		q, q.SortBy, (*apiSort)(nil), (*apiDirection)(nil), itemSortRank, search.SortDescending)))
	assert.Equal(t, want, resolve(searchexec.APIDirection(q, q.SortBy, itemSortRank, (*apiDirection)(nil))))
}

// A direction that is not ASC or DESC is the client's error: the helpers keep it as sent,
// and Run refuses it before any statement runs. It never falls back to ASC.
func TestAPISortRefusesAnInvalidDirection(t *testing.T) {
	q := newItemSearch(fullScope())
	name := apiSort(itemSortName)
	for _, d := range []apiDirection{"desc", "", "Desc", "DROP TABLE"} {
		t.Run(string(d), func(t *testing.T) {
			for label, got := range map[string]itemSearch{
				"APISort":      searchexec.APISort(q, q.SortBy, &name, &d, itemSortRank, search.SortDescending),
				"APIDirection": searchexec.APIDirection(q, q.SortBy, itemSortRank, &d),
			} {
				rec := &recorder{}
				_, err := searchexec.Run(t.Context(), rec, itemSpec, got.Query, identity)

				require.ErrorIs(t, err, search.ErrInvalidSortDirection, label)
				assert.True(t, search.IsClientError(err), label)
				assert.Empty(t, rec.queries, "%s: no statement runs", label)
			}
		})
	}
}

// apiPage resolves the query that APIPage returns and gives its page.
func apiPage(t *testing.T, q itemSearch, p *search.Pagination) (search.Pagination, error) {
	t.Helper()
	plan, err := searchexec.Resolve(t.Context(), itemSpec, searchexec.APIPage(q.Query, p).Query)
	return plan.Page(), err
}

func TestAPIPage(t *testing.T) {
	q := newItemSearch(fullScope())
	cases := map[string]struct {
		q    itemSearch
		page *search.Pagination
		want search.Pagination
	}{
		"no pagination keeps the default":   {q, nil, search.Pagination{Limit: 20}},
		"a missing limit keeps the default": {q, &search.Pagination{}, search.Pagination{Limit: 20}},
		"a cursor and no limit": {
			q, &search.Pagination{Cursor: "token"}, search.Pagination{Limit: 20, Cursor: "token"},
		},
		"an offset and no limit": {q, &search.Pagination{Offset: 3}, search.Pagination{Limit: 20, Offset: 3}},
		"every field": {
			q, &search.Pagination{Limit: 5, Offset: 3}, search.Pagination{Limit: 5, Offset: 3},
		},
		"the largest limit": {
			q, &search.Pagination{Limit: search.MaxLimit}, search.Pagination{Limit: search.MaxLimit},
		},
		"the API page replaces the cursor of the query": {
			q.After("old"), &search.Pagination{Offset: 3}, search.Pagination{Limit: 20, Offset: 3},
		},
		"no pagination keeps the page of the query": {
			q.Limit(7).After("old"), nil, search.Pagination{Limit: 7, Cursor: "old"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := apiPage(t, tc.q, tc.page)

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestAPIPageLeavesTheChecksToResolve(t *testing.T) {
	q := newItemSearch(fullScope())
	cases := map[string]struct {
		page search.Pagination
		want error
	}{
		// Only zero is a missing limit. Another value outside 1 to MaxLimit is the client's.
		"a negative limit":       {search.Pagination{Limit: -1}, search.ErrInvalidLimit},
		"a limit above the max":  {search.Pagination{Limit: search.MaxLimit + 1}, search.ErrInvalidLimit},
		"a negative offset":      {search.Pagination{Offset: -1}, search.ErrInvalidOffset},
		"a cursor and an offset": {search.Pagination{Offset: 3, Cursor: "token"}, search.ErrCursorWithOffset},
		"a cursor that is broken": {
			search.Pagination{Cursor: strings.Repeat("a", search.MaxCursorLength+1)},
			search.ErrInvalidCursor,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := apiPage(t, q, &tc.page)

			require.ErrorIs(t, err, tc.want)
			assert.True(t, search.IsClientError(err))
		})
	}
}

func TestAPIPageDoesNotChangeItsArguments(t *testing.T) {
	q := newItemSearch(fullScope())
	page := search.Pagination{Offset: 3}

	_ = searchexec.APIPage(q.Query, &page)

	assert.Equal(t, search.Pagination{Offset: 3}, page)
	got, err := apiPage(t, q, nil)
	require.NoError(t, err)
	assert.Equal(t, search.Pagination{Limit: 20}, got, "the query keeps its page")
}

// TestAPIMapper writes a mapper as nanostack gen repo writes <Name>SearchFromAPI, and checks that
// an empty body runs the search as New does.
func TestAPIMapper(t *testing.T) {
	type filter struct {
		Names    []string
		HasOwner *bool
	}
	type body struct {
		Filter         *filter
		FullTextSearch *string
		SortBy         *apiSort
		SortDirection  *apiDirection
		Pagination     *search.Pagination
	}
	fromAPI := func(scope itemScope, in *body) itemSearch {
		q, b := newItemSearch(scope), searchexec.OrZero(in)
		if f := b.Filter; f != nil {
			q = searchexec.Some(q, q.Name().In, f.Names)
			q = searchexec.Opt(q, q.Owner().Present, f.HasOwner)
		}
		q = searchexec.Opt(q, q.Text().Contains, b.FullTextSearch)
		q = searchexec.APISort(q, q.SortBy, b.SortBy, b.SortDirection, itemSortRank, search.SortDescending)
		return searchexec.APIPage(q.Query, b.Pagination)
	}
	q := newItemSearch(fullScope())

	sameFilter(t, q, fromAPI(fullScope(), nil))
	sameFilter(t, q, fromAPI(fullScope(), &body{Filter: &filter{}, Pagination: &search.Pagination{}}))

	hasOwner, term, asc := true, "x", apiDirection(search.SortAscending)
	got := fromAPI(fullScope(), &body{
		Filter:         &filter{Names: []string{"a"}, HasOwner: &hasOwner},
		FullTextSearch: &term,
		SortDirection:  &asc,
		Pagination:     &search.Pagination{Limit: 5},
	})
	sameFilter(t, q.Name().In("a").Owner().Present(true).Text().Contains("x"), got)
	plan, err := searchexec.Resolve(t.Context(), itemSpec, got.Query)
	require.NoError(t, err)
	assert.Equal(t, []search.Sort[itemSort]{searchexec.Asc(itemSortRank)}, plan.Sorts())
	assert.Equal(t, search.Pagination{Limit: 5}, plan.Page())
}
