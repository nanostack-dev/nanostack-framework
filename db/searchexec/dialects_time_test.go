package searchexec_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/db/keyset"
	"github.com/nanostack-dev/nanostack-framework/db/mysql"
	"github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/search"
	"github.com/nanostack-dev/nanostack-framework/db/searchexec"
	"github.com/nanostack-dev/nanostack-framework/db/sqlite"
)

// dLocalSearch runs filter on the events table of each dialect with a timestamp column
// without time zone, the type of every generated MySQL and SQLite time column. It returns
// the count statement of each dialect.
func dLocalSearch(
	t *testing.T, filter func(q dSearch, at searchexec.TimeRange[dSearch]) dSearch,
) map[string]statementSQL {
	t.Helper()
	pgAt, myAt, liteAt := postgres.TimestampColumn("at"), mysql.TimestampColumn("at"), sqlite.TimestampColumn("at")
	tables := map[string]struct {
		source keyset.Source
		at     func(dSearch) searchexec.TimeRange[dSearch]
	}{
		"postgres": {postgres.NewTable("public", "events", "", dEventID, dEventOrg, pgAt),
			func(q dSearch) searchexec.TimeRange[dSearch] { return searchexec.NewLocalTimeRange(q.Query, pgAt) }},
		"mysql": {mysql.NewTable("db", "events", "", dEventID, dEventOrg, myAt),
			func(q dSearch) searchexec.TimeRange[dSearch] { return searchexec.NewLocalTimeRange(q.Query, myAt) }},
		"sqlite": {sqlite.NewTable("", "events", "", dEventID, dEventOrg, liteAt),
			func(q dSearch) searchexec.TimeRange[dSearch] { return searchexec.NewLocalTimeRange(q.Query, liteAt) }},
	}
	out := map[string]statementSQL{}
	for dialect, table := range tables {
		spec := dSpec(table.source)
		q := searchexec.New[dSearch](spec, "org-a")
		rec := &recorder{}
		_, err := searchexec.Run(
			t.Context(),
			rec,
			spec,
			filter(q, table.at(q)).Query,
			func(r dEvent) dEvent { return r },
		)
		require.ErrorIs(t, err, errStop, dialect)
		out[dialect] = statementSQL{flat(rec.queries[0]), rec.args[0]}
	}
	return out
}

// A local bound is rounded as a timestamptz bound. PostgreSQL gets the wall clock of the
// bound in UTC, MySQL gets the bound for its driver to move to loc, and SQLite gets the
// driver's text in UTC.
func TestLocalTimeRangePerDialect(t *testing.T) {
	bound := time.Date(2026, 11, 9, 12, 0, 0, 1_400, time.FixedZone("UTC-5", -5*3600))
	up := bound.Truncate(time.Microsecond).Add(time.Microsecond)
	got := dLocalSearch(t, func(_ dSearch, at searchexec.TimeRange[dSearch]) dSearch { return at.Since(bound) })
	where := "( (events.org = %s) AND (events.at >= %s) );"
	assert.Equal(t, statementSQL{
		"SELECT COUNT(*) FROM public.events WHERE " + fmt.Sprintf(where, "$1", "$2"), []any{"org-a", up.UTC()},
	}, got["postgres"])
	assert.Equal(t, statementSQL{
		"SELECT COUNT(*) FROM db.events WHERE " + fmt.Sprintf(where, "?", "?"), []any{"org-a", up},
	}, got["mysql"])
	assert.Equal(t, statementSQL{
		"SELECT COUNT(*) FROM events WHERE " + fmt.Sprintf(where, "?", "?"),
		[]any{"org-a", "2026-11-09 17:00:00.0000014+00:00"},
	}, got["sqlite"])
}

// With a layout of whole seconds, a SQLite bound between two seconds moves to one of them
// in the direction that keeps its meaning. A bound on a second stays. The other dialects
// do not change.
func TestTimeRangeSQLiteLayout(t *testing.T) {
	const layout = "2006-01-02 15:04:05"
	between := time.Date(2026, 11, 9, 12, 0, 0, 500_000_000, time.FixedZone("UTC-5", -5*3600))
	on := time.Date(2026, 11, 9, 17, 0, 0, 0, time.UTC)
	cases := map[string]struct {
		filter func(searchexec.TimeRange[dSearch]) dSearch
		want   string
	}{
		"Since between": {
			func(f searchexec.TimeRange[dSearch]) dSearch { return f.Since(between) },
			"2026-11-09 17:00:01",
		},
		"Until between": {
			func(f searchexec.TimeRange[dSearch]) dSearch { return f.Until(between) },
			"2026-11-09 17:00:00",
		},
		"After between": {
			func(f searchexec.TimeRange[dSearch]) dSearch { return f.After(between) },
			"2026-11-09 17:00:00",
		},
		"Before between": {
			func(f searchexec.TimeRange[dSearch]) dSearch { return f.Before(between) },
			"2026-11-09 17:00:01",
		},
		"Since on": {func(f searchexec.TimeRange[dSearch]) dSearch { return f.Since(on) }, "2026-11-09 17:00:00"},
		"Before on": {
			func(f searchexec.TimeRange[dSearch]) dSearch { return f.Before(on) },
			"2026-11-09 17:00:00",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := dLocalSearch(t, func(_ dSearch, at searchexec.TimeRange[dSearch]) dSearch {
				return tc.filter(at.SQLiteLayout(layout))
			})
			assert.Equal(t, []any{"org-a", tc.want}, got["sqlite"].args)
			assert.IsType(t, time.Time{}, got["postgres"].args[1])
			assert.IsType(t, time.Time{}, got["mysql"].args[1])
		})
	}
	t.Run("milliseconds", func(t *testing.T) {
		got := dLocalSearch(t, func(_ dSearch, at searchexec.TimeRange[dSearch]) dSearch {
			return at.SQLiteLayout("2006-01-02T15:04:05.000Z07:00").Since(on.Add(1_500 * time.Microsecond))
		})
		assert.Equal(t, []any{"org-a", "2026-11-09T17:00:00.002Z"}, got["sqlite"].args)
	})
}

func TestTimeRangeSQLiteLayoutPanicsOnALayoutOutOfTimeOrder(t *testing.T) {
	q := searchexec.New[dSearch](dSpec(dTables()["sqlite"][0]), "org-a")
	assert.PanicsWithValue(t, `searchexec: SQLite layout "02/01/2006 15:04" does not sort in time order`, func() {
		searchexec.NewLocalTimeRange(q.Query, sqlite.TimestampColumn("at")).SQLiteLayout("02/01/2006 15:04")
	})
}

// A bound outside the years that every dialect holds is a client error before any
// statement runs, on every dialect. The first and the last bound that every dialect holds
// run.
func TestTimeRangeRefusesABoundOutsideTheYears(t *testing.T) {
	first := time.Date(1, time.January, 2, 0, 0, 0, 0, time.UTC)
	last := time.Date(9999, time.December, 30, 23, 59, 59, 999_999_000, time.UTC)
	methods := map[string]func(searchexec.TimeRange[dSearch], time.Time) dSearch{
		"Since":  searchexec.TimeRange[dSearch].Since,
		"Until":  searchexec.TimeRange[dSearch].Until,
		"After":  searchexec.TimeRange[dSearch].After,
		"Before": searchexec.TimeRange[dSearch].Before,
	}
	for dialect, tables := range dTables() {
		spec := dSpec(tables[0])
		for name, method := range methods {
			for _, bound := range []time.Time{first.Add(-time.Nanosecond), last.Add(time.Nanosecond), {}} {
				q := searchexec.New[dSearch](spec, "org-a")
				rec := &recorder{}
				_, err := searchexec.Run(t.Context(), rec, spec,
					method(searchexec.NewTimeRange(q.Query, dEventAt), bound).Query, func(r dEvent) dEvent { return r })
				require.ErrorIs(t, err, search.ErrInvalidTime, "%s %s %s", dialect, name, bound)
				assert.True(t, search.IsClientError(err))
				assert.Empty(t, rec.queries, "%s %s %s", dialect, name, bound)
			}
			for _, bound := range []time.Time{first, last} {
				q := searchexec.New[dSearch](spec, "org-a")
				_, err := searchexec.Resolve(
					t.Context(),
					spec,
					method(searchexec.NewTimeRange(q.Query, dEventAt), bound).Query,
				)
				require.NoError(t, err, "%s %s %s", dialect, name, bound)
			}
		}
	}
}

// A nested SELECT is written in the dialect of the outer statement, so an Exists child of
// another dialect would name a table of another database. Resolve refuses it before any
// statement runs.
func TestExistsRefusesAChildOfAnotherDialect(t *testing.T) {
	tables := dTables()
	for outer, child := range map[string]string{
		"postgres": "mysql", "mysql": "sqlite", "sqlite": "postgres",
	} {
		t.Run(outer+" with a "+child+" child", func(t *testing.T) {
			spec := dSpec(tables[outer][0])
			q := searchexec.New[dSearch](spec, "org-a")
			f := searchexec.NewExists(
				q.Query,
				tables[child][1],
				dTagEvent.EQ(dEventID),
				dTagName,
				searchexec.StringLit[string],
			)
			for _, filtered := range []dSearch{f.In("red"), f.In()} {
				rec := &recorder{}
				_, err := searchexec.Run(t.Context(), rec, spec, filtered.Query, func(r dEvent) dEvent { return r })
				require.ErrorIs(t, err, keyset.ErrDialect)
				assert.ErrorContains(t, err, "an Exists child is a table of")
				assert.Empty(t, rec.queries)
			}
		})
	}
}
