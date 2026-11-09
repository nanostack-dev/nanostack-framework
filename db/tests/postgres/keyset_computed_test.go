// This file is Nanostack-only (not in go-jet): the computed keys of db/keyset
// against PostgreSQL. See db/UPSTREAM.md.

package postgres

import (
	"context"
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/db/keyset"
	jet "github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/qrm"
	"github.com/nanostack-dev/nanostack-framework/db/search"
)

// A computed key orders on an aggregate of each widget's requests. Few
// distinct counts, times, amounts and labels make ties dominate, and widgets
// with no request make every aggregate but COUNT NULL.

type kcField string

const (
	ksRequests    kcField = "REQUEST_COUNT"
	ksLast        kcField = "LAST_RECEIVED"
	ksLastNF      kcField = "LAST_RECEIVED_NULLS_FIRST"
	ksLastNL      kcField = "LAST_RECEIVED_NULLS_LAST"
	ksLastOrMade  kcField = "LAST_OR_CREATED"
	ksTotal       kcField = "TOTAL"
	ksTotalOrZero kcField = "TOTAL_OR_ZERO"
	ksMinLabel    kcField = "MIN_LABEL"
	ksLabelOrNone kcField = "LABEL_OR_NONE"
	ksBiggest     kcField = "BIGGEST"
	kcName        kcField = "NAME"
)

type ksComputedRow struct {
	ID          string     `alias:"it_keyset.id"`
	Name        string     `alias:"it_keyset.name"`
	Requests    int64      `alias:"it_keyset_row.request_count"`
	Last        *time.Time `alias:"it_keyset_row.last_received"`
	LastOrMade  time.Time  `alias:"it_keyset_row.last_or_created"`
	Total       *float64   `alias:"it_keyset_row.total"`
	TotalOrZero float64    `alias:"it_keyset_row.total_or_zero"`
	MinLabel    *string    `alias:"it_keyset_row.min_label"`
	LabelOrNone string     `alias:"it_keyset_row.label_or_none"`
	Biggest     *int64     `alias:"it_keyset_row.biggest"`
}

var (
	ksReqWidget   = jet.StringColumn("widget_id")
	ksReqReceived = jet.TimestampzColumn("received")
	ksReqAmount   = jet.FloatColumn("amount")
	ksReqLabel    = jet.StringColumn("label")
	ksReqSize     = jet.IntegerColumn("size")
	ksReqTable    = jet.NewTable("public", "it_keyset_request", "",
		ksReqWidget, ksReqReceived, ksReqAmount, ksReqLabel, ksReqSize)
)

// ofWidget is a correlated subquery over the requests of the outer widget.
func ofWidget(projection jet.Projection) jet.SelectStatement {
	return ksReqTable.SELECT(projection).WHERE(ksReqWidget.EQ(ksID))
}

func ksComputedKeys() map[kcField]keyset.Key[ksComputedRow] {
	last := func() keyset.Key[ksComputedRow] {
		return keyset.NullableTimeExprKey("it_keyset_row.last_received",
			jet.TimestampzExp(ofWidget(jet.MAX(ksReqReceived))),
			func(r ksComputedRow) *time.Time { return r.Last })
	}
	return map[kcField]keyset.Key[ksComputedRow]{
		ksRequests: keyset.IntExprKey("it_keyset_row.request_count",
			jet.IntExp(ofWidget(jet.COUNT(jet.STAR))),
			func(r ksComputedRow) int64 { return r.Requests }),
		ksLast:   last(),
		ksLastNF: last().NullsFirst(),
		ksLastNL: last().NullsLast(),
		ksLastOrMade: keyset.TimeExprKey("it_keyset_row.last_or_created",
			jet.TimestampzExp(jet.COALESCE(ofWidget(jet.MAX(ksReqReceived)), ksCreatedC)),
			func(r ksComputedRow) time.Time { return r.LastOrMade }),
		ksTotal: keyset.NullableFloatExprKey("it_keyset_row.total",
			jet.FloatExp(ofWidget(jet.SUMf(ksReqAmount))),
			func(r ksComputedRow) *float64 { return r.Total }),
		ksTotalOrZero: keyset.FloatExprKey("it_keyset_row.total_or_zero",
			jet.FloatExp(jet.COALESCE(ofWidget(jet.SUMf(ksReqAmount)), jet.Float(0))),
			func(r ksComputedRow) float64 { return r.TotalOrZero }),
		ksMinLabel: keyset.NullableStringExprKey("it_keyset_row.min_label",
			jet.StringExp(ofWidget(jet.MIN(ksReqLabel))),
			func(r ksComputedRow) *string { return r.MinLabel }),
		ksLabelOrNone: keyset.StringExprKey("it_keyset_row.label_or_none",
			jet.StringExp(jet.COALESCE(ofWidget(jet.MAX(ksReqLabel)), jet.String(""))),
			func(r ksComputedRow) string { return r.LabelOrNone }),
		ksBiggest: keyset.NullableIntExprKey("it_keyset_row.biggest",
			jet.IntExp(ofWidget(jet.MAXi(ksReqSize))),
			func(r ksComputedRow) *int64 { return r.Biggest }),
		kcName: keyset.StringKey(ksNameC, func(r ksComputedRow) string { return r.Name }),
	}
}

func ksComputedOrder(t *testing.T, sorts ...search.Sort[kcField]) keyset.Order[ksComputedRow] {
	t.Helper()
	order, err := keyset.OrderOf(sorts, ksComputedKeys(),
		keyset.StringKey(ksID, func(r ksComputedRow) string { return r.ID }))
	require.NoError(t, err)
	return order
}

// setupComputedKeysetData seeds the widgets of tenant t1 and their requests.
// A widget gets zero to three requests.
func setupComputedKeysetData(t *testing.T, conn qrm.DB, seed uint64, widgets int) {
	t.Helper()
	setupKeysetSchema(t, conn)
	_, err := conn.ExecContext(t.Context(), `DROP TABLE IF EXISTS it_keyset_request`)
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), `
		CREATE TABLE it_keyset_request (
			id bigserial PRIMARY KEY,
			widget_id text NOT NULL,
			received timestamptz NOT NULL,
			amount double precision NOT NULL,
			label text NOT NULL,
			size bigint NOT NULL
		)`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = conn.ExecContext(context.Background(), `DROP TABLE IF EXISTS it_keyset_request`)
	})

	base := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	gen := &ksGen{rng: rand.New(rand.NewPCG(seed, seed+1)), base: base}
	for range widgets {
		gen.insert(t, conn, "t1")
	}
	for range 20 {
		gen.insert(t, conn, "t2") // other tenant, must never appear
	}
	for _, widget := range widgetIDs(t, conn) {
		for range gen.rng.IntN(4) {
			_, err = conn.ExecContext(t.Context(),
				`INSERT INTO it_keyset_request (widget_id, received, amount, label, size) VALUES ($1, $2, $3, $4, $5)`,
				widget,
				base.Add(time.Duration(gen.rng.IntN(3))*time.Hour),
				[]float64{0.5, 1.25, 2}[gen.rng.IntN(3)],
				[]string{"a", "b", "B"}[gen.rng.IntN(3)],
				int64(1+gen.rng.IntN(2)),
			)
			require.NoError(t, err)
		}
	}
}

func widgetIDs(t *testing.T, conn qrm.DB) []string {
	t.Helper()
	rows, err := conn.QueryContext(t.Context(), `SELECT id FROM it_keyset ORDER BY id`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		require.NoError(t, rows.Scan(&id))
		ids = append(ids, id)
	}
	require.NoError(t, rows.Err())
	return ids
}

var ksComputedColumns = []jet.Projection{ksID, ksNameC}

func ksComputedPage(
	t *testing.T, conn qrm.DB, order keyset.Order[ksComputedRow], page search.Pagination,
) search.Result[ksComputedRow] {
	t.Helper()
	result, err := keyset.Page(conn, func(r ksComputedRow) ksComputedRow { return r }, ksComputedColumns...).
		From(ksTable).
		Where(ksTenantC.EQ(jet.String("t1"))).
		Keyset(order).
		Run(t.Context(), page)
	require.NoError(t, err, "Run(%+v)", page)
	return result
}

// ksComputedOrdered is the reference: one query ordered by the same keys.
func ksComputedOrdered(t *testing.T, conn qrm.DB, order keyset.Order[ksComputedRow]) []ksComputedRow {
	t.Helper()
	var rows []ksComputedRow
	err := ksTable.SELECT(ksID, ksNameC, order.Projections()).
		WHERE(ksTenantC.EQ(jet.String("t1"))).
		ORDER_BY(order.OrderBy()...).
		QueryContext(t.Context(), conn, &rows)
	require.NoError(t, err)
	return rows
}

func ksComputedWalk(t *testing.T, conn qrm.DB, order keyset.Order[ksComputedRow], limit int32) []ksComputedRow {
	t.Helper()
	var rows []ksComputedRow
	page := search.Pagination{Limit: limit}
	var total int64
	for range 10_000 {
		result := ksComputedPage(t, conn, order, page)
		if page.Cursor == "" {
			total = result.Total
		}
		require.Equal(t, total, result.Total, "Total counts every match on every page")
		rows = append(rows, result.Items...)
		if !result.HasMore() {
			return rows
		}
		require.EqualValues(t, limit, result.Count, "a page with a next cursor must be full")
		page.Cursor = result.NextCursor
	}
	t.Fatal("cursor walk did not terminate")
	return nil
}

func rowIDs(rows []ksComputedRow) []string {
	ids := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	return ids
}

func TestIntegrationKeysetComputedWalkMatchesOrderedQuery(t *testing.T) {
	conn := keysetDB(t)
	// Fewer widgets than the other suites: every page evaluates the
	// correlated subqueries of every match.
	const widgets = 60
	setupComputedKeysetData(t, conn, 11, widgets)

	fields := []kcField{
		ksRequests, ksLast, ksLastNF, ksLastNL, ksLastOrMade,
		ksTotal, ksTotalOrZero, ksMinLabel, ksLabelOrNone, ksBiggest,
	}
	directions := []search.SortDirection{search.SortAscending, search.SortDescending}
	var orderings [][]search.Sort[kcField]
	for _, field := range fields {
		for _, direction := range directions {
			orderings = append(
				orderings,
				[]search.Sort[kcField]{{Field: field, Direction: direction}},
				[]search.Sort[kcField]{
					{Field: field, Direction: direction},
					{Field: kcName, Direction: search.SortAscending},
				},
			)
		}
	}
	orderings = append(
		orderings,
		[]search.Sort[kcField]{
			{Field: ksRequests, Direction: search.SortDescending},
			{Field: ksLast, Direction: search.SortAscending},
			{Field: ksMinLabel, Direction: search.SortDescending},
		},
		[]search.Sort[kcField]{
			{Field: kcName, Direction: search.SortDescending},
			{Field: ksTotal, Direction: search.SortAscending},
		},
	)

	for _, sorts := range orderings {
		order := ksComputedOrder(t, sorts...)
		want := rowIDs(ksComputedOrdered(t, conn, order))
		require.Len(t, want, widgets)
		limits := []int32{7, 25}
		if len(sorts) == 1 {
			limits = append(limits, 1) // every row is a page boundary
		}
		for _, limit := range limits {
			got := rowIDs(ksComputedWalk(t, conn, order, limit))
			require.True(t, slices.Equal(got, want),
				"%s limit %d: the cursor walk returned %d rows that differ from the ordered query's %d",
				order.Signature(), limit, len(got), len(want))
		}
	}
}

// requestStats reads each t1 widget's count and last request time with plain
// SQL, independent of the keys.
func requestStats(t *testing.T, conn qrm.DB) (map[string]int64, map[string]*time.Time) {
	t.Helper()
	rows, err := conn.QueryContext(t.Context(), `
		SELECT w.id, COUNT(r.id), MAX(r.received)
		FROM it_keyset w LEFT JOIN it_keyset_request r ON r.widget_id = w.id
		WHERE w.tenant = 't1'
		GROUP BY w.id`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	counts, lasts := map[string]int64{}, map[string]*time.Time{}
	for rows.Next() {
		var id string
		var count int64
		var last *time.Time
		require.NoError(t, rows.Scan(&id, &count, &last))
		counts[id], lasts[id] = count, last
	}
	require.NoError(t, rows.Err())
	return counts, lasts
}

func TestIntegrationKeysetComputedCountWithTies(t *testing.T) {
	conn := keysetDB(t)
	setupComputedKeysetData(t, conn, 21, 157)
	counts, _ := requestStats(t, conn)

	for _, direction := range []search.SortDirection{search.SortAscending, search.SortDescending} {
		order := ksComputedOrder(t, search.Sort[kcField]{Field: ksRequests, Direction: direction})
		got := ksComputedWalk(t, conn, order, 5)
		require.Len(t, got, len(counts), "every widget, once")

		seen := map[string]bool{}
		distinct := map[int64]bool{}
		for i, row := range got {
			require.False(t, seen[row.ID], "row %s returned twice", row.ID)
			seen[row.ID] = true
			require.Equal(t, counts[row.ID], row.Requests, "the scan reads the count through the alias")
			distinct[row.Requests] = true
			if i == 0 {
				continue
			}
			previous := got[i-1]
			if direction == search.SortDescending {
				require.GreaterOrEqual(t, previous.Requests, row.Requests)
			} else {
				require.LessOrEqual(t, previous.Requests, row.Requests)
			}
			if previous.Requests == row.Requests {
				if direction == search.SortDescending {
					require.Greater(t, previous.ID, row.ID, "the id breaks a tie in the sort's direction")
				} else {
					require.Less(t, previous.ID, row.ID, "the id breaks a tie in the sort's direction")
				}
			}
		}
		require.Less(t, len(distinct), len(got)/10, "the data must be mostly ties")
		require.True(t, distinct[0], "some widgets have no request")
	}
}

func TestIntegrationKeysetComputedNullableMaxIsComplete(t *testing.T) {
	conn := keysetDB(t)
	setupComputedKeysetData(t, conn, 31, 60)
	_, lasts := requestStats(t, conn)

	for _, tc := range []struct {
		field      kcField
		direction  search.SortDirection
		nullsFirst bool
	}{
		{ksLast, search.SortAscending, false},
		{ksLast, search.SortDescending, true},
		{ksLastNF, search.SortAscending, true},
		{ksLastNF, search.SortDescending, true},
		{ksLastNL, search.SortAscending, false},
		{ksLastNL, search.SortDescending, false},
	} {
		order := ksComputedOrder(t, search.Sort[kcField]{Field: tc.field, Direction: tc.direction})
		// A limit of 1 makes every row a page boundary, the NULL rows too.
		for _, limit := range []int32{1, 4} {
			got := ksComputedWalk(t, conn, order, limit)
			require.Len(t, got, len(lasts), "%s: every widget, once", order.Signature())

			nulls := 0
			for _, row := range got {
				want := lasts[row.ID]
				if want == nil {
					require.Nil(t, row.Last, "a widget with no request scans NULL")
					nulls++
					continue
				}
				require.NotNil(t, row.Last)
				require.True(t, want.Equal(*row.Last), "the scan reads the time through the alias")
			}
			require.Positive(t, nulls, "some widgets have no request")
			require.Less(t, nulls, len(got), "some widgets have requests")

			nullBlock := got[:nulls]
			if !tc.nullsFirst {
				nullBlock = got[len(got)-nulls:]
			}
			for _, row := range nullBlock {
				require.Nil(t, row.Last, "%s: the NULLs sort together at one end", order.Signature())
			}
			for i := 1; i < len(nullBlock); i++ {
				if tc.direction == search.SortDescending {
					require.Greater(t, nullBlock[i-1].ID, nullBlock[i].ID)
				} else {
					require.Less(t, nullBlock[i-1].ID, nullBlock[i].ID)
				}
			}
		}
	}
}
