//go:build integration

package transactor_test

import (
	"context"
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	jet "github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/pkg/db/transactor"
	"github.com/nanostack-dev/nanostack-framework/pkg/search"
)

// A session zone whose offset has seconds (Paris before 1911 was UTC+00:09:21)
// must not shift the boundary instant: RFC 3339 cannot carry the seconds.
func TestIntegrationKeysetTimestamptzWithSecondsOffsetZone(t *testing.T) {
	db := openDB(t)
	db.SetMaxOpenConns(1)
	setupKeysetSchema(t, db)
	if _, err := db.Exec(`SET TIME ZONE 'Europe/Paris'`); err != nil {
		t.Fatalf("set zone: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`RESET TIME ZONE`) })
	gen := &ksGen{rng: rand.New(rand.NewPCG(9, 10)), base: time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)}
	for i := range 30 {
		gen.base = time.Date(1900, 1, 1, 0, 0, i, 0, time.UTC)
		gen.insert(t, db, "t1")
	}
	for _, direction := range []search.SortDirection{search.SortAscending, search.SortDescending} {
		order := transactor.Keys(
			[]search.Sort[ksField]{{Field: ksCreated, Direction: direction}},
			ksKeys(),
			ksTiebreak(),
		)
		if got, want := ksWalk(t, db, order, 4, nil), ksOrdered(t, db, order); !slices.Equal(got, want) {
			t.Fatalf("%s: walk in Europe/Paris returned %d rows, ordered query %d", direction, len(got), len(want))
		}
	}
}

// Forged or extreme values must reach Postgres as comparable literals, never
// fail to bind: a smallint compared with a value past its range, and a
// timestamp in year 12000 at a page boundary.
func TestIntegrationKeysetExtremeValues(t *testing.T) {
	type field string
	db := openDB(t)
	for _, stmt := range []string{
		`DROP TABLE IF EXISTS it_keyset_extreme`,
		`CREATE TABLE it_keyset_extreme (id text PRIMARY KEY, small smallint NOT NULL, at timestamptz NOT NULL, ratio real NOT NULL)`,
		`INSERT INTO it_keyset_extreme VALUES
			('a', 1, '2026-01-01Z', 0.1), ('b', 2, '12000-01-01Z', 0.1), ('c', 3, '12000-01-01Z', 0.1),
			('d', 4, '294276-12-31 23:59:59.999999Z', 0.2), ('e', 5, '294276-12-31 23:59:59.999999Z', 0.3)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	t.Cleanup(func() { _, _ = db.Exec(`DROP TABLE IF EXISTS it_keyset_extreme`) })

	type row struct {
		ID    string    `alias:"it_keyset_extreme.id"`
		Small int64     `alias:"it_keyset_extreme.small"`
		At    time.Time `alias:"it_keyset_extreme.at"`
		Ratio float32   `alias:"it_keyset_extreme.ratio"`
	}
	id, small, at, ratio := jet.StringColumn(
		"id",
	), jet.IntegerColumn(
		"small",
	), jet.TimestampzColumn(
		"at",
	), jet.FloatColumn(
		"ratio",
	)
	table := jet.NewTable("public", "it_keyset_extreme", "", id, small, at, ratio)
	keys := map[field]transactor.Key[row]{
		"SMALL": transactor.IntKey(small, func(r row) int64 { return r.Small }),
		"AT":    transactor.TimeKey(at, func(r row) time.Time { return r.At }),
		"RATIO": transactor.RealKey(ratio, func(r row) float32 { return r.Ratio }),
	}
	tiebreak := transactor.StringKey(id, func(r row) string { return r.ID })
	run := func(order transactor.Order[row], page search.Pagination) (search.Result[row], error) {
		return transactor.Page(db, func(r row) row { return r }, id, small, at, ratio).
			From(table).Keyset(order).Run(context.Background(), page).Value()
	}

	bySmall := transactor.Keys(
		[]search.Sort[field]{{Field: "SMALL", Direction: search.SortAscending}},
		keys,
		tiebreak,
	)
	forged, _ := search.EncodeCursor(bySmall.Signature(), []any{int64(1) << 40, "a"})
	if result, err := run(bySmall, search.Pagination{Limit: 2, Cursor: forged}); err != nil || result.Count != 0 {
		t.Fatalf("forged out-of-range smallint cursor = %d rows, %v; want an empty page", result.Count, err)
	}

	// Year 12000 and the last microsecond Postgres stores both sit on page
	// boundaries at limit 1, as does a real value that is inexact as double.
	for _, sorted := range []field{"AT", "RATIO"} {
		order := transactor.Keys(
			[]search.Sort[field]{{Field: sorted, Direction: search.SortAscending}},
			keys,
			tiebreak,
		)
		var got []string
		page := search.Pagination{Limit: 1}
		for range 10 {
			result, err := run(order, page)
			if err != nil {
				t.Fatalf("%s walk: %v", sorted, err)
			}
			for _, r := range result.Items {
				got = append(got, r.ID)
			}
			if !result.HasMore() {
				break
			}
			page.Cursor = result.NextCursor
		}
		if !slices.Equal(got, []string{"a", "b", "c", "d", "e"}) {
			t.Fatalf("%s walk = %v, want every row once", sorted, got)
		}
	}
}
