package keyset_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/db/keyset"
	jet "github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/search"
)

type pageRow struct {
	ID   string `alias:"widgets.id"`
	Name string `alias:"widgets.name"`
}

var pageColumns = []string{"widgets.id", "widgets.name"}

func pageRows(ids ...string) answer {
	rows := make([][]driver.Value, len(ids))
	for i, id := range ids {
		rows[i] = []driver.Value{id, "name " + id}
	}
	return answer{columns: pageColumns, rows: rows}
}

func pageOrder(t *testing.T) keyset.Order[pageRow] {
	t.Helper()
	return mustOrder(t,
		[]search.Sort[sortField]{{Field: sortByName, Direction: search.SortDescending}},
		//nolint:exhaustive // a test lists only the sorts it orders by
		map[sortField]keyset.Key[pageRow]{
			sortByName: keyset.StringKey(colName, func(r pageRow) string { return r.Name }),
		},
		keyset.StringKey(colID, func(r pageRow) string { return r.ID }),
	)
}

func ids(rows []string) string { return strings.Join(rows, ",") }

func itemIDs(items []pageRow) string {
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = item.ID
	}
	return ids(out)
}

func identity(r pageRow) pageRow { return r }

func TestRunSkipsThePageWhenNothingMatches(t *testing.T) {
	fake := &fakeDB{answers: []answer{countAnswer(0)}}
	result, err := keyset.Page(fake.open(), identity, colID, colName).
		From(testTable).Keyset(pageOrder(t)).
		Run(t.Context(), search.Pagination{Limit: 10})
	require.NoError(t, err)
	require.NotNil(t, result.Items, "an empty page has an empty slice, not nil")
	require.Empty(t, result.Items)
	require.Zero(t, result.Total)
	require.False(t, result.HasMore())
	require.Len(t, fake.recorded(), 1, "the page query runs only when the count finds rows")
	require.Contains(t, fake.recorded()[0], "SELECT COUNT(*)")
}

func TestRunReadsAnOffsetPage(t *testing.T) {
	fake := &fakeDB{answers: []answer{countAnswer(7), pageRows("c", "d")}}
	result, err := keyset.Page(fake.open(), identity, colID, colName).
		From(testTable).OrderBy(colName.ASC()).
		Run(t.Context(), search.Pagination{Limit: 2, Offset: 2})
	require.NoError(t, err)
	require.Equal(t, "c,d", itemIDs(result.Items))
	require.Equal(t, int64(7), result.Total)
	require.Equal(t, 2, result.Count)
	require.False(t, result.HasMore(), "an offset page without a keyset has no cursor")
}

func TestRunTrimsAKeysetPageAndContinuesAfterItsCursor(t *testing.T) {
	order := pageOrder(t)
	fake := &fakeDB{answers: []answer{
		countAnswer(5), pageRows("a", "b", "c"),
		countAnswer(5), pageRows("c", "d"),
	}}
	db := fake.open()
	run := func(page search.Pagination) search.Result[pageRow] {
		result, err := keyset.Page(db, identity, colID, colName).
			From(testTable).Where(colTenant.EQ(jet.String("t1"))).Keyset(order).
			Run(t.Context(), page)
		require.NoError(t, err)
		return result
	}

	first := run(search.Pagination{Limit: 2})
	require.Equal(t, "a,b", itemIDs(first.Items), "the extra row tells that a page follows and is cut")
	require.Equal(t, int64(5), first.Total)
	want, err := order.Cursor(pageRow{ID: "b", Name: "name b"})
	require.NoError(t, err)
	require.Equal(t, want, first.NextCursor, "the cursor points at the last kept row")

	second := run(search.Pagination{Limit: 2, Cursor: first.NextCursor})
	require.Equal(t, "c,d", itemIDs(second.Items))
	require.False(t, second.HasMore(), "no extra row: the last page")

	queries := fake.recorded()
	require.Len(t, queries, 4)
	require.NotContains(t, queries[1], "ROW(", "the first page has no seek")
	require.Contains(t, queries[3], "ROW(widgets.name, widgets.id) < ROW(")
	require.Contains(t, queries[3], "LIMIT $", "the page fetches limit+1 rows")
	require.NotContains(t, queries[2], "ROW(", "Total counts every match, not the rows after the cursor")
}

func TestPageOfMapsTheTrimmedPageAtOnce(t *testing.T) {
	fake := &fakeDB{answers: []answer{countAnswer(3), pageRows("a", "b", "c")}}
	var seen []pageRow
	result, err := keyset.PageOf(fake.open(), func(_ context.Context, rows []pageRow) ([]string, error) {
		seen = rows
		out := make([]string, len(rows))
		for i, r := range rows {
			out[i] = "item " + r.ID
		}
		return out, nil
	}, colID, colName).From(testTable).Keyset(pageOrder(t)).Run(t.Context(), search.Pagination{Limit: 2})
	require.NoError(t, err)
	require.Equal(t, "a,b", itemIDs(seen), "the mapper sees the page, not the extra row")
	require.Equal(t, []string{"item a", "item b"}, result.Items)
	require.Equal(t, 2, result.Count)
	require.True(t, result.HasMore())
}

func TestPageOfNeverReturnsNilItems(t *testing.T) {
	fake := &fakeDB{answers: []answer{countAnswer(1), pageRows()}}
	result, err := keyset.PageOf(fake.open(), func(context.Context, []pageRow) ([]string, error) {
		return nil, nil
	}, colID, colName).From(testTable).Keyset(pageOrder(t)).Run(t.Context(), search.Pagination{Limit: 2})
	require.NoError(t, err)
	require.NotNil(t, result.Items)
	require.Zero(t, result.Count)
}

func TestRunReturnsTheErrorsOfTheQueriesAndTheMapper(t *testing.T) {
	errDB := errors.New("connection reset")
	errMap := errors.New("mapper failed")
	for name, tc := range map[string]struct {
		answers []answer
		mapper  func(context.Context, []pageRow) ([]pageRow, error)
		want    error
	}{
		"the count fails": {answers: []answer{{err: errDB}}, want: errDB},
		"the page fails":  {answers: []answer{countAnswer(1), {err: errDB}}, want: errDB},
		"the mapper fails": {
			answers: []answer{countAnswer(1), pageRows("a")},
			mapper:  func(context.Context, []pageRow) ([]pageRow, error) { return nil, errMap },
			want:    errMap,
		},
	} {
		t.Run(name, func(t *testing.T) {
			fake := &fakeDB{answers: tc.answers}
			mapper := tc.mapper
			if mapper == nil {
				mapper = func(_ context.Context, rows []pageRow) ([]pageRow, error) { return rows, nil }
			}
			result, err := keyset.PageOf(fake.open(), mapper, colID, colName).
				From(testTable).Keyset(pageOrder(t)).Run(t.Context(), search.Pagination{Limit: 2})
			require.ErrorIs(t, err, tc.want)
			require.Nil(t, result.Items)
		})
	}
}

// A page whose last row is its own cursor row would hand the same cursor back
// without end. Run refuses it.
func TestRunRefusesAPageThatEndsOnItsCursorRow(t *testing.T) {
	order := pageOrder(t)
	cursor, err := order.Cursor(pageRow{ID: "b", Name: "name b"})
	require.NoError(t, err)
	fake := &fakeDB{answers: []answer{countAnswer(9), pageRows("a", "b", "c")}}
	_, err = keyset.Page(fake.open(), identity, colID, colName).
		From(testTable).Keyset(order).
		Run(t.Context(), search.Pagination{Limit: 2, Cursor: cursor})
	require.ErrorIs(t, err, keyset.ErrNoProgress)
	require.False(t, search.IsClientError(err), "a seek that matches its own row is not a client error")

	fake = &fakeDB{answers: []answer{countAnswer(9), pageRows("c", "d", "e")}}
	result, err := keyset.Page(fake.open(), identity, colID, colName).
		From(testTable).Keyset(order).
		Run(t.Context(), search.Pagination{Limit: 2, Cursor: cursor})
	require.NoError(t, err, "a page that moves past the cursor runs")
	require.NotEqual(t, cursor, result.NextCursor)
}
