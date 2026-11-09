// This file is Nanostack-only (not in go-jet): db/keyset at the edges of the
// PostgreSQL types. It is the copy of
// pkg/db/transactor/keyset_bounds_integration_test.go that calls db/keyset
// directly. See db/UPSTREAM.md.

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

// A session zone whose offset has seconds (Paris before 1911 was UTC+00:09:21)
// must not shift the boundary instant: RFC 3339 cannot carry the seconds. The
// zone is set for one transaction, which is also the executor of every page.
// The table is created outside it, so the rollback cannot drop it.
func TestIntegrationKeysetTimestamptzWithSecondsOffsetZone(t *testing.T) {
	setupKeysetSchema(t, keysetDB(t))
	tx, err := db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	_, err = tx.ExecContext(t.Context(), `SET LOCAL TIME ZONE 'Europe/Paris'`)
	require.NoError(t, err)

	gen := &ksGen{rng: rand.New(rand.NewPCG(9, 10)), base: time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)}
	for i := range 30 {
		gen.base = time.Date(1900, 1, 1, 0, 0, i, 0, time.UTC)
		gen.insert(t, tx, "t1")
	}
	for _, direction := range []search.SortDirection{search.SortAscending, search.SortDescending} {
		order := ksOrder(t,
			[]search.Sort[ksField]{{Field: ksCreated, Direction: direction}},
			ksKeys(),
			ksTiebreak(),
		)
		if got, want := ksWalk(t, tx, order, 4, nil), ksOrdered(t, tx, order); !slices.Equal(got, want) {
			t.Fatalf("%s: walk in Europe/Paris returned %d rows, ordered query %d", direction, len(got), len(want))
		}
	}
}

type extremeRow struct {
	ID    string    `alias:"it_keyset_extreme.id"`
	Small int64     `alias:"it_keyset_extreme.small"`
	At    time.Time `alias:"it_keyset_extreme.at"`
	Ratio float32   `alias:"it_keyset_extreme.ratio"`
}

var (
	extremeID    = jet.StringColumn("id")
	extremeSmall = jet.IntegerColumn("small")
	extremeAt    = jet.TimestampzColumn("at")
	extremeRatio = jet.FloatColumn("ratio")
	extremeTable = jet.NewTable("public", "it_keyset_extreme", "", extremeID, extremeSmall, extremeAt, extremeRatio)
	extremeKeys  = map[string]keyset.Key[extremeRow]{
		"SMALL": keyset.IntKey(extremeSmall, func(r extremeRow) int64 { return r.Small }),
		"AT":    keyset.TimeKey(extremeAt, func(r extremeRow) time.Time { return r.At }),
		"RATIO": keyset.RealKey(extremeRatio, func(r extremeRow) float32 { return r.Ratio }),
	}
	extremeTiebreak = keyset.StringKey(extremeID, func(r extremeRow) string { return r.ID })
)

func setupExtremeTable(t *testing.T, conn qrm.DB) {
	t.Helper()
	for _, stmt := range []string{
		`DROP TABLE IF EXISTS it_keyset_extreme`,
		`CREATE TABLE it_keyset_extreme (
			id text PRIMARY KEY, small smallint NOT NULL, at timestamptz NOT NULL, ratio real NOT NULL)`,
		`INSERT INTO it_keyset_extreme VALUES
			('a', 1, '2026-01-01Z', 0.1), ('b', 2, '12000-01-01Z', 0.1), ('c', 3, '12000-01-01Z', 0.1),
			('d', 4, '294276-12-31 23:59:59.999999Z', 0.2), ('e', 5, '294276-12-31 23:59:59.999999Z', 0.3)`,
	} {
		_, err := conn.ExecContext(t.Context(), stmt)
		require.NoError(t, err, stmt)
	}
	t.Cleanup(func() { _, _ = conn.ExecContext(context.Background(), `DROP TABLE IF EXISTS it_keyset_extreme`) })
}

func extremeOrder(t *testing.T, field string) keyset.Order[extremeRow] {
	t.Helper()
	order, err := keyset.OrderOf(
		[]search.Sort[string]{{Field: field, Direction: search.SortAscending}}, extremeKeys, extremeTiebreak,
	)
	require.NoError(t, err)
	return order
}

func runExtreme(
	t *testing.T,
	order keyset.Order[extremeRow],
	page search.Pagination,
) (search.Result[extremeRow], error) {
	t.Helper()
	return keyset.Page(db, func(r extremeRow) extremeRow { return r }, extremeID, extremeSmall, extremeAt, extremeRatio).
		From(extremeTable).
		Keyset(order).
		Run(t.Context(), page)
}

// Forged or extreme values must reach PostgreSQL as comparable literals, and
// never fail to bind: a smallint compared with a value past its range, and a
// timestamp in year 12000 at a page boundary.
func TestIntegrationKeysetExtremeValues(t *testing.T) {
	setupExtremeTable(t, keysetDB(t))

	bySmall := extremeOrder(t, "SMALL")
	forged, err := search.EncodeCursor(bySmall.Signature(), []any{int64(1) << 40, "a"})
	require.NoError(t, err)
	result, err := runExtreme(t, bySmall, search.Pagination{Limit: 2, Cursor: forged})
	require.NoError(t, err, "forged out-of-range smallint cursor")
	require.Zero(t, result.Count, "forged out-of-range smallint cursor gives an empty page")

	// Year 12000 and the last microsecond PostgreSQL stores both sit on page
	// boundaries at limit 1, as does a real value that is inexact as double.
	for _, sorted := range []string{"AT", "RATIO"} {
		order := extremeOrder(t, sorted)
		var got []string
		page := search.Pagination{Limit: 1}
		for range 10 {
			result, err = runExtreme(t, order, page)
			require.NoError(t, err, sorted)
			for _, r := range result.Items {
				got = append(got, r.ID)
			}
			if !result.HasMore() {
				break
			}
			page.Cursor = result.NextCursor
		}
		require.Equal(t, []string{"a", "b", "c", "d", "e"}, got, "%s walk returns every row once", sorted)
	}
}
