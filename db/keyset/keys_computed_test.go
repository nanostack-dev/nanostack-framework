package keyset_test

import (
	"database/sql/driver"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/db/keyset"
	"github.com/nanostack-dev/nanostack-framework/db/mysql"
	jet "github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/search"
	"github.com/nanostack-dev/nanostack-framework/db/sqlite"
)

// computedRow is a widget with the values of its requests, read through the
// select aliases of the computed keys.
type computedRow struct {
	ID       string     `alias:"widgets.id"`
	Name     string     `alias:"widgets.name"`
	Requests int64      `alias:"widget_row.request_count"`
	Last     *time.Time `alias:"widget_row.last_received"`
	First    time.Time  `alias:"widget_row.first_received"`
	Total    float64    `alias:"widget_row.total"`
	Mean     *float64   `alias:"widget_row.mean"`
	Label    string     `alias:"widget_row.label"`
	MinLabel *string    `alias:"widget_row.min_label"`
	Biggest  *int64     `alias:"widget_row.biggest"`
}

// computedField is the sort field of the computed-key tests.
type computedField string

const (
	sortByRequests computedField = "REQUEST_COUNT"
	sortByLast     computedField = "LAST_RECEIVED"
	sortByFirst    computedField = "FIRST_RECEIVED"
	sortByTotal    computedField = "TOTAL"
	sortByMean     computedField = "MEAN"
	sortByLabel    computedField = "LABEL"
	sortByMinLabel computedField = "MIN_LABEL"
	sortByBiggest  computedField = "BIGGEST"
)

var (
	reqWidgetID = jet.StringColumn("widget_id")
	reqReceived = jet.TimestampzColumn("received_at")
	reqAmount   = jet.FloatColumn("amount")
	reqLabel    = jet.StringColumn("label")
	reqSize     = jet.IntegerColumn("size")
	requests    = jet.NewTable("public", "requests", "", reqWidgetID, reqReceived, reqAmount, reqLabel, reqSize)
)

// ofWidget scopes a correlated subquery over the requests to the outer widget.
func ofWidget(projection jet.Projection) jet.SelectStatement {
	return requests.SELECT(projection).WHERE(reqWidgetID.EQ(colID))
}

func computedKeys() map[computedField]keyset.Key[computedRow] {
	return map[computedField]keyset.Key[computedRow]{
		sortByRequests: keyset.IntExprKey("widget_row.request_count",
			jet.IntExp(ofWidget(jet.COUNT(jet.STAR))), func(r computedRow) int64 { return r.Requests }),
		sortByLast: keyset.NullableTimeExprKey("widget_row.last_received",
			jet.TimestampzExp(ofWidget(jet.MAX(reqReceived))), func(r computedRow) *time.Time { return r.Last }),
		sortByFirst: keyset.TimeExprKey("widget_row.first_received",
			jet.TimestampzExp(ofWidget(jet.MIN(reqReceived))), func(r computedRow) time.Time { return r.First }),
		sortByTotal: keyset.FloatExprKey("widget_row.total",
			jet.FloatExp(ofWidget(jet.SUMf(reqAmount))), func(r computedRow) float64 { return r.Total }),
		sortByMean: keyset.NullableFloatExprKey("widget_row.mean",
			jet.FloatExp(ofWidget(jet.AVG(reqAmount))), func(r computedRow) *float64 { return r.Mean }),
		sortByLabel: keyset.StringExprKey("widget_row.label",
			jet.StringExp(ofWidget(jet.MAX(reqLabel))), func(r computedRow) string { return r.Label }),
		sortByMinLabel: keyset.NullableStringExprKey("widget_row.min_label",
			jet.StringExp(ofWidget(jet.MIN(reqLabel))), func(r computedRow) *string { return r.MinLabel }),
		sortByBiggest: keyset.NullableIntExprKey("widget_row.biggest",
			jet.IntExp(ofWidget(jet.MAXi(reqSize))), func(r computedRow) *int64 { return r.Biggest }),
	}
}

func computedTiebreak() keyset.Key[computedRow] {
	return keyset.StringKey(colID, func(r computedRow) string { return r.ID })
}

func computedOrder(t *testing.T, sorts ...search.Sort[computedField]) keyset.Order[computedRow] {
	t.Helper()
	return mustOrder(t, sorts, computedKeys(), computedTiebreak())
}

func byField(field computedField, direction search.SortDirection) search.Sort[computedField] {
	return search.Sort[computedField]{Field: field, Direction: direction}
}

func TestComputedKeySignatureUsesItsName(t *testing.T) {
	order := computedOrder(t,
		byField(sortByRequests, search.SortDescending),
		byField(sortByLast, search.SortAscending),
		byField(sortByRequests, search.SortAscending),
	)
	require.Equal(t,
		"widget_row.request_count:DESC,widget_row.last_received:ASC,widgets.id:ASC",
		order.Signature(),
		"the name is the signature, and a key requested twice keeps its first position",
	)
}

func TestComputedKeyOrdersAndSeeksOnTheExpression(t *testing.T) {
	order := computedOrder(t, byField(sortByRequests, search.SortDescending))
	count := "( SELECT COUNT(*) FROM public.requests WHERE requests.widget_id = widgets.id )::bigint"

	orderBy := normalize(jet.SELECT(colID).FROM(testTable).ORDER_BY(order.OrderBy()...).DebugSql())
	require.Contains(t, orderBy, "ORDER BY "+count+" DESC, widgets.id DESC")

	cursor, err := order.Cursor(computedRow{ID: "w_1", Requests: 3})
	require.NoError(t, err)
	seek, err := order.Seek(cursor)
	require.NoError(t, err)
	require.Contains(t,
		normalize(jet.SELECT(colID).FROM(testTable).WHERE(seek).DebugSql()),
		"ROW("+count+", widgets.id) < ROW(3::bigint, 'w_1'::text)",
	)
}

func TestComputedKeyCastsToTheTypeOfItsCursorValue(t *testing.T) {
	//nolint:exhaustive // the forms that cast, and one that does not
	for field, want := range map[computedField]string{
		sortByLast:     ")::timestamp with time zone ASC",
		sortByTotal:    ")::double precision ASC",
		sortByBiggest:  ")::bigint ASC",
		sortByMinLabel: "ORDER BY (( SELECT MIN(requests.label) FROM public.requests WHERE requests.widget_id = widgets.id )) ASC",
	} {
		t.Run(string(field), func(t *testing.T) {
			order := computedOrder(t, byField(field, search.SortAscending))
			sql := normalize(jet.SELECT(colID).FROM(testTable).ORDER_BY(order.OrderBy()...).DebugSql())
			require.Contains(t, sql, want)
		})
	}
}

func TestProjectionsAliasTheComputedKeysOnly(t *testing.T) {
	require.Empty(t, mustOrder[sortField](t, nil, keyColumns(), idKey()).Projections())

	order := computedOrder(t,
		byField(sortByRequests, search.SortAscending),
		byField(sortByLast, search.SortDescending),
	)
	sql := normalize(jet.SELECT(colID, order.Projections()).FROM(testTable).DebugSql())
	require.Contains(t, sql, `)::bigint AS "widget_row.request_count"`)
	require.Contains(t, sql, `)::timestamp with time zone AS "widget_row.last_received"`)
	require.Len(t, order.Projections(), 2)
}

func TestPageSelectsTheComputedKeysForThePageOnly(t *testing.T) {
	order := computedOrder(t, byField(sortByRequests, search.SortDescending))
	builder := keyset.Page(nil, func(r computedRow) computedRow { return r }, colID, colName).
		From(testTable).Keyset(order)

	page := normalize(builder.KeysetStatementForTest(search.Pagination{Limit: 10}, nil).DebugSql())
	require.Contains(t, page, `)::bigint AS "widget_row.request_count"`)
	require.NotContains(t, normalize(builder.CountStatementForTest().DebugSql()), "widget_row.request_count")
}

func TestRunScansTheComputedKeyAndPutsItInTheCursor(t *testing.T) {
	order := computedOrder(t, byField(sortByLast, search.SortDescending))
	received := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	columns := []string{"widgets.id", "widgets.name", "widget_row.last_received"}
	fake := &fakeDB{answers: []answer{
		countAnswer(3),
		{columns: columns, rows: [][]driver.Value{
			{"w_1", "one", received},
			{"w_2", "two", nil},
			{"w_3", "three", nil},
		}},
	}}
	result, err := keyset.Page(fake.open(), func(r computedRow) computedRow { return r }, colID, colName).
		From(testTable).Keyset(order).
		Run(t.Context(), search.Pagination{Limit: 2})
	require.NoError(t, err)
	require.Len(t, result.Items, 2)
	require.Equal(t, received, result.Items[0].Last.UTC())
	require.Nil(t, result.Items[1].Last)
	require.Contains(t, fake.recorded()[1], `AS "widget_row.last_received"`)

	// The cursor carries the NULL of the last row, so the next page starts
	// inside the NULLs, which sort last when descending only with NullsLast:
	// by default they sort first, and every non-null value follows them.
	seek, err := order.Seek(result.NextCursor)
	require.NoError(t, err)
	sql := normalize(jet.SELECT(colID).FROM(testTable).WHERE(seek).DebugSql())
	require.Contains(t, sql, "IS NOT NULL")
	require.Contains(t, sql, "IS NULL) AND (widgets.id < 'w_2'::text)")
}

func TestNullableComputedKeySeeksAfterANull(t *testing.T) {
	last := "( SELECT MAX(requests.received_at) FROM public.requests WHERE requests.widget_id = widgets.id )" +
		"::timestamp with time zone"
	for name, tc := range map[string]struct {
		key  keyset.Key[computedRow]
		dir  search.SortDirection
		want string
	}{
		"ascending, NULLs last: only NULLs follow": {
			key: computedKeys()[sortByLast], dir: search.SortAscending,
			want: "WHERE (" + last + " IS NULL) AND (widgets.id > 'w_1'::text);",
		},
		"descending, NULLs first: every value follows": {
			key: computedKeys()[sortByLast], dir: search.SortDescending,
			want: "WHERE (" + last + " IS NOT NULL) OR ((" + last + " IS NULL) AND (widgets.id < 'w_1'::text));",
		},
		"ascending with NullsFirst: every value follows": {
			key: computedKeys()[sortByLast].NullsFirst(), dir: search.SortAscending,
			want: "WHERE (" + last + " IS NOT NULL) OR ((" + last + " IS NULL) AND (widgets.id > 'w_1'::text));",
		},
	} {
		t.Run(name, func(t *testing.T) {
			order := mustOrder(t, []search.Sort[computedField]{byField(sortByLast, tc.dir)},
				//nolint:exhaustive // one key per case
				map[computedField]keyset.Key[computedRow]{sortByLast: tc.key}, computedTiebreak())
			cursor, err := order.Cursor(computedRow{ID: "w_1"})
			require.NoError(t, err)
			seek, err := order.Seek(cursor)
			require.NoError(t, err)
			require.Contains(t, normalize(jet.SELECT(colID).FROM(testTable).WHERE(seek).DebugSql()), tc.want)
		})
	}
}

func TestComputedKeyRefusesCursorValuesItCannotHold(t *testing.T) {
	for name, tc := range map[string]struct {
		field  computedField
		values []any
	}{
		"null for a NOT NULL count":   {sortByRequests, []any{nil, "w_1"}},
		"text for a count":            {sortByRequests, []any{"3", "w_1"}},
		"a fraction for a count":      {sortByRequests, []any{1.5, "w_1"}},
		"null for a NOT NULL time":    {sortByFirst, []any{nil, "w_1"}},
		"a time past PostgreSQL":      {sortByLast, []any{int64(math.MaxInt64), "w_1"}},
		"an unknown float word":       {sortByTotal, []any{"Huge", "w_1"}},
		"a NUL byte in a text":        {sortByLabel, []any{"a\x00b", "w_1"}},
		"a number for a text":         {sortByMinLabel, []any{7, "w_1"}},
		"null for a NOT NULL float":   {sortByTotal, []any{nil, "w_1"}},
		"null for a NOT NULL text":    {sortByLabel, []any{nil, "w_1"}},
		"text for a nullable integer": {sortByBiggest, []any{"x", "w_1"}},
	} {
		t.Run(name, func(t *testing.T) {
			order := computedOrder(t, byField(tc.field, search.SortAscending))
			cursor, err := search.EncodeCursor(order.Signature(), tc.values)
			require.NoError(t, err)
			_, err = order.Seek(cursor)
			require.ErrorIs(t, err, search.ErrInvalidCursor)
			require.ErrorContains(t, err, "computed key widget_row.")
		})
	}
}

func TestComputedKeysRoundTripTheirCursorValues(t *testing.T) {
	received := time.Date(2026, 11, 9, 10, 0, 0, 123_456_000, time.UTC)
	mean, biggest, label := math.Inf(1), int64(-7), "zeta"
	row := computedRow{
		ID: "w_9", Requests: 12, Last: &received, First: received,
		Total: math.NaN(), Mean: &mean, Label: "alpha", MinLabel: &label, Biggest: &biggest,
	}
	for field := range computedKeys() {
		t.Run(string(field), func(t *testing.T) {
			order := computedOrder(t, byField(field, search.SortAscending))
			cursor, err := order.Cursor(row)
			require.NoError(t, err)
			seek, err := order.Seek(cursor)
			require.NoError(t, err)
			require.NotNil(t, seek)
		})
	}
}

func TestComputedKeyNameMustBeAnIdentifier(t *testing.T) {
	count := jet.IntExp(ofWidget(jet.COUNT(jet.STAR)))
	value := func(r computedRow) int64 { return r.Requests }
	for _, name := range []string{"", "Request_Count", `widget_row."x`, "widget row", "1count"} {
		require.Panics(t, func() { keyset.IntExprKey(name, count, value) }, name)
	}
	require.NotPanics(t, func() { keyset.IntExprKey("widget_row.request_count", count, value) })
}

func TestComputedKeyPageOnAnotherDialectIsErrDialect(t *testing.T) {
	order := computedOrder(t, byField(sortByRequests, search.SortDescending))
	for name, source := range map[string]keyset.Source{
		"MySQL":  mysql.NewTable("db", "widgets", "", mysql.StringColumn("id")),
		"SQLite": sqlite.NewTable("", "widgets", "", sqlite.StringColumn("id")),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := keyset.Page(nil, func(r computedRow) computedRow { return r }, colID).
				From(source).Keyset(order).
				Run(t.Context(), search.Pagination{Limit: 1})
			require.ErrorIs(t, err, keyset.ErrDialect)
		})
	}
}

// A subquery inside a function, such as COALESCE((SELECT ...), 0), must keep
// its parentheses in the ORDER BY, the seek and the select list.
func TestComputedKeyKeepsTheParenthesesOfASubqueryInAFunction(t *testing.T) {
	//nolint:exhaustive // one integer form and one text form
	keys := map[computedField]keyset.Key[computedRow]{
		sortByRequests: keyset.IntExprKey("widget_row.request_count",
			jet.IntExp(jet.COALESCE(ofWidget(jet.MAXi(reqSize)), jet.Int(0))),
			func(r computedRow) int64 { return r.Requests }),
		sortByLabel: keyset.StringExprKey("widget_row.label",
			jet.StringExp(jet.COALESCE(ofWidget(jet.MAX(reqLabel)), jet.String(""))),
			func(r computedRow) string { return r.Label }),
	}
	for field := range keys {
		t.Run(string(field), func(t *testing.T) {
			order := mustOrder(
				t,
				[]search.Sort[computedField]{byField(field, search.SortAscending)},
				keys,
				computedTiebreak(),
			)
			cursor, err := order.Cursor(computedRow{ID: "w_1", Label: "b", Requests: 2})
			require.NoError(t, err)
			seek, err := order.Seek(cursor)
			require.NoError(t, err)
			sql := normalize(jet.SELECT(colID, order.Projections()).FROM(testTable).
				WHERE(seek).ORDER_BY(order.OrderBy()...).DebugSql())
			require.NotContains(t, sql, "COALESCE(SELECT")
			require.Equal(t, 3, strings.Count(sql, "COALESCE(( SELECT"), sql)
		})
	}
}
