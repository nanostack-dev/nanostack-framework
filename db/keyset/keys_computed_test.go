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
	count := "COALESCE(( SELECT COUNT(*) FROM public.requests WHERE requests.widget_id = widgets.id )::bigint, 0)"

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
		sortByTotal:    ")::double precision, 0) ASC",
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
	require.Contains(t, sql, `)::bigint, 0) AS "widget_row.request_count"`)
	require.Contains(t, sql, `)::timestamp with time zone AS "widget_row.last_received"`)
	require.Len(t, order.Projections(), 2)
}

func TestPageSelectsTheComputedKeysForThePageOnly(t *testing.T) {
	order := computedOrder(t, byField(sortByRequests, search.SortDescending))
	builder := keyset.Page(nil, func(r computedRow) computedRow { return r }, colID, colName).
		From(testTable).Keyset(order)

	page := normalize(builder.KeysetStatementForTest(search.Pagination{Limit: 10}, nil).DebugSql())
	require.Contains(t, page, `)::bigint, 0) AS "widget_row.request_count"`)
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
	for _, name := range []string{
		"", "Request_Count", `widget_row."x`, "widget row", "1count",
		"request_count", "widget_row.a.b", ".count", "widget_row.",
	} {
		require.Panics(t, func() { keyset.IntExprKey(name, count, value) }, name)
	}
	require.NotPanics(t, func() { keyset.IntExprKey("widget_row.request_count", count, value) })
}

// The integer, float and text forms run on every dialect, each with its own
// cast. The timestamptz form is PostgreSQL only, as TimeKey is.
func TestComputedKeyPagePerDialect(t *testing.T) {
	sources := map[string]keyset.Source{
		"MySQL":  mysql.NewTable("db", "widgets", "", mysql.StringColumn("id")),
		"SQLite": sqlite.NewTable("", "widgets", "", sqlite.StringColumn("id")),
	}
	for _, field := range []computedField{sortByRequests, sortByBiggest, sortByTotal, sortByMean, sortByLabel, sortByMinLabel} {
		order := computedOrder(t, byField(field, search.SortDescending))
		for name, source := range sources {
			t.Run(string(field)+" on "+name, func(t *testing.T) {
				_, rows, err := keyset.Page(nil, func(r computedRow) computedRow { return r }, colID).
					From(source).Keyset(order).
					StatementsForTest(search.Pagination{Limit: 1})
				require.NoError(t, err)
				require.NotContains(t, rows.DebugSql(), "::", "no PostgreSQL cast")
			})
		}
	}
	for _, field := range []computedField{sortByLast, sortByFirst} {
		order := computedOrder(t, byField(field, search.SortDescending))
		for name, source := range sources {
			t.Run(string(field)+" on "+name, func(t *testing.T) {
				_, err := keyset.Page(nil, func(r computedRow) computedRow { return r }, colID).
					From(source).Keyset(order).
					Run(t.Context(), search.Pagination{Limit: 1})
				require.ErrorIs(t, err, keyset.ErrDialect)
			})
		}
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

// A non-nullable form reads NULL as the zero value: the order, the seek and
// the select all see COALESCE(expression, zero), so a NULL row can never
// carry a cursor value that its order does not hold. The Nullable forms keep
// NULL.
func TestNotNullComputedKeyReadsNullAsTheZeroValue(t *testing.T) {
	for field, want := range map[computedField]string{
		sortByRequests: "COALESCE(( SELECT COUNT(*) FROM public.requests WHERE requests.widget_id = widgets.id )" +
			"::bigint, 0)",
		sortByTotal: "COALESCE(( SELECT SUM(requests.amount) FROM public.requests WHERE requests.widget_id = widgets.id )" +
			"::double precision, 0)",
		sortByFirst: "COALESCE(( SELECT MIN(requests.received_at) FROM public.requests WHERE requests.widget_id = " +
			"widgets.id )::timestamp with time zone, '0001-01-01 00:00:00Z'::timestamp with time zone)",
		sortByLabel: "COALESCE((( SELECT MAX(requests.label) FROM public.requests WHERE requests.widget_id = " +
			"widgets.id )), '')",
		sortByLast:     "",
		sortByMean:     "",
		sortByMinLabel: "",
		sortByBiggest:  "",
	} {
		t.Run(string(field), func(t *testing.T) {
			order := computedOrder(t, byField(field, search.SortAscending))
			cursor, err := order.Cursor(computedRow{ID: "w_1"})
			require.NoError(t, err)
			seek, err := order.Seek(cursor)
			require.NoError(t, err)
			sql := normalize(jet.SELECT(colID, order.Projections()).FROM(testTable).
				WHERE(seek).ORDER_BY(order.OrderBy()...).DebugSql())
			if want == "" {
				require.NotContains(t, sql, "COALESCE", "a Nullable form keeps NULL")
				return
			}
			require.Equal(t, 3, strings.Count(sql, want), "the select, the seek and the order: %s", sql)
		})
	}
}

// Two different keys of one name would share one cursor signature, and one
// would replace the other in the order. OrderOf refuses them whatever the
// request sorts on.
func TestComputedKeyNameCannotNameAnotherKey(t *testing.T) {
	count := jet.IntExp(ofWidget(jet.COUNT(jet.STAR)))
	biggest := jet.IntExp(ofWidget(jet.MAXi(reqSize)))
	value := func(r computedRow) int64 { return r.Requests }

	t.Run("a computed key named after the tiebreak column", func(t *testing.T) {
		//nolint:exhaustive // one key
		keys := map[computedField]keyset.Key[computedRow]{sortByRequests: keyset.IntExprKey("widgets.id", count, value)}
		for _, sorts := range [][]search.Sort[computedField]{nil, {byField(sortByRequests, search.SortAscending)}} {
			_, err := keyset.OrderOf(sorts, keys, computedTiebreak())
			require.ErrorContains(t, err, "widgets.id names two different keys")
		}
	})
	t.Run("two computed keys of one name", func(t *testing.T) {
		//nolint:exhaustive // two keys
		keys := map[computedField]keyset.Key[computedRow]{
			sortByRequests: keyset.IntExprKey("widget_row.n", count, value),
			sortByBiggest:  keyset.IntExprKey("widget_row.n", biggest, value),
		}
		_, err := keyset.OrderOf([]search.Sort[computedField]{byField(sortByRequests, search.SortAscending)},
			keys, computedTiebreak())
		require.ErrorContains(t, err, "widget_row.n names two different keys")
	})
	t.Run("one key listed twice", func(t *testing.T) {
		key := keyset.IntExprKey("widget_row.n", count, value)
		//nolint:exhaustive // one key under two sorts
		keys := map[computedField]keyset.Key[computedRow]{sortByRequests: key, sortByBiggest: key}
		order, err := keyset.OrderOf([]search.Sort[computedField]{
			byField(sortByRequests, search.SortAscending), byField(sortByBiggest, search.SortAscending),
		}, keys, computedTiebreak(), computedTiebreak())
		require.NoError(t, err)
		require.Equal(t, "widget_row.n:ASC,widgets.id:ASC", order.Signature())
	})
}

// A column with the alias of a computed key would put a second value into the
// field: the computed one, or a value that the seek does not compare.
func TestPageRefusesAColumnWithTheAliasOfAComputedKey(t *testing.T) {
	//nolint:exhaustive // one key
	keys := map[computedField]keyset.Key[computedRow]{
		sortByLabel: keyset.StringExprKey("widgets.name", jet.LOWER(colName),
			func(r computedRow) string { return r.Name }),
	}
	order := mustOrder(t, []search.Sort[computedField]{byField(sortByLabel, search.SortAscending)},
		keys, computedTiebreak())
	fake := &fakeDB{}
	_, err := keyset.Page(fake.open(), func(r computedRow) computedRow { return r }, colID, colName).
		From(testTable).Keyset(order).
		Run(t.Context(), search.Pagination{Limit: 2})
	require.ErrorContains(t, err, `a column has the alias of the computed key "widgets.name"`)
	require.Empty(t, fake.recorded(), "no statement runs")
}

// The design selects a computed value on every order, through
// Key.Projection. When the order also uses the key, the page selects it once.
func TestPageSelectsAComputedKeyInTheColumnsOnce(t *testing.T) {
	requests := computedKeys()[sortByRequests]
	for _, field := range []computedField{sortByRequests, sortByLast} {
		t.Run(string(field), func(t *testing.T) {
			order := computedOrder(t, byField(field, search.SortDescending))
			builder := keyset.Page(nil, func(r computedRow) computedRow { return r },
				colID, colName, requests.Projection()).From(testTable).Keyset(order)
			page := normalize(builder.KeysetStatementForTest(search.Pagination{Limit: 10}, nil).DebugSql())
			require.Equal(t, 1, strings.Count(page, `AS "widget_row.request_count"`), page)
		})
	}
	require.Nil(t, computedTiebreak().Projection(), "a column key has no projection")
}
