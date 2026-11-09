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

// The dialect fixture is one search of events, scoped to an organization, on a table of
// each dialect package. Its count statement shows the SQL of each field on that dialect.

type dEvent struct {
	ID string `alias:"events.id"`
}

type dSearch struct {
	searchexec.Query[dEvent, dSearch, string, string]
}

var (
	dEventID  = postgres.StringColumn("id")
	dEventOrg = postgres.StringColumn("org")
	dEventAt  = postgres.TimestampzColumn("at")
	dTagEvent = postgres.StringColumn("event_id")
	dTagOrg   = postgres.StringColumn("org")
	dTagName  = postgres.StringColumn("name")
)

// dTables are the events table and its tags table in each dialect package.
func dTables() map[string][2]keyset.Source {
	return map[string][2]keyset.Source{
		"postgres": {
			postgres.NewTable("public", "events", "", dEventID, dEventOrg, dEventAt),
			postgres.NewTable("public", "tags", "", dTagEvent, dTagOrg, dTagName),
		},
		"mysql": {
			mysql.NewTable("db", "events", "", dEventID, dEventOrg, dEventAt),
			mysql.NewTable("db", "tags", "", dTagEvent, dTagOrg, dTagName),
		},
		"sqlite": {
			sqlite.NewTable("", "events", "", dEventID, dEventOrg, dEventAt),
			sqlite.NewTable("", "tags", "", dTagEvent, dTagOrg, dTagName),
		},
	}
}

func dSpec(source keyset.Source) *searchexec.Spec[dEvent, string, string] {
	key := keyset.StringKey(dEventID, func(r dEvent) string { return r.ID })
	return searchexec.DefineSpec(searchexec.SpecDef[dEvent, string, string]{
		From:     func(string) keyset.Source { return source },
		Columns:  postgres.ProjectionList{dEventID},
		Scope:    []searchexec.ScopeCol[string]{searchexec.ScopeEq(searchexec.Value[string], dEventOrg)},
		Keys:     map[string]keyset.Key[dEvent]{"id": key},
		Tiebreak: []keyset.Key[dEvent]{key},
		Limit:    10,
	})
}

// dCount returns the count statement of the search that filter builds, on each dialect.
func dCount(t *testing.T, filter func(q dSearch, child keyset.Source) dSearch) map[string]statementSQL {
	t.Helper()
	out := map[string]statementSQL{}
	for dialect, tables := range dTables() {
		spec := dSpec(tables[0])
		rec := &recorder{}
		q := filter(searchexec.New[dSearch](spec, "org-a"), tables[1])
		_, err := searchexec.Run(t.Context(), rec, spec, q.Query, func(r dEvent) dEvent { return r })
		require.ErrorIs(t, err, errStop, dialect)
		out[dialect] = statementSQL{flat(rec.queries[0]), rec.args[0]}
	}
	return out
}

func TestRunOnEveryDialect(t *testing.T) {
	got := dCount(t, func(q dSearch, _ keyset.Source) dSearch { return q })
	assert.Equal(t, statementSQL{"SELECT COUNT(*) FROM public.events WHERE (events.org = $1);", []any{"org-a"}},
		got["postgres"])
	assert.Equal(t, statementSQL{"SELECT COUNT(*) FROM db.events WHERE (events.org = ?);", []any{"org-a"}},
		got["mysql"])
	assert.Equal(t, statementSQL{"SELECT COUNT(*) FROM events WHERE (events.org = ?);", []any{"org-a"}},
		got["sqlite"])
}

func TestExistsPerDialect(t *testing.T) {
	got := dCount(t, func(q dSearch, child keyset.Source) dSearch {
		return searchexec.NewExists(q.Query, child, dTagEvent.EQ(dEventID).AND(dTagOrg.EQ(dEventOrg)), dTagName,
			searchexec.StringLit[string]).In("red", "blue")
	})
	exists := "SELECT COUNT(*) FROM %sevents WHERE ( (events.org = %s) AND (EXISTS ( SELECT 1 FROM %stags " +
		"WHERE ( ((tags.event_id = events.id) AND (tags.org = events.org)) AND (tags.name IN (%s, %s)) ) )) );"
	args := []any{"org-a", "red", "blue"}
	assert.Equal(t, statementSQL{fmt.Sprintf(exists, "public.", "$1", "public.", "$2", "$3"), args}, got["postgres"])
	assert.Equal(t, statementSQL{fmt.Sprintf(exists, "db.", "?", "db.", "?", "?"), args}, got["mysql"])
	assert.Equal(t, statementSQL{fmt.Sprintf(exists, "", "?", "", "?", "?"), args}, got["sqlite"])
}

// A bound in a zone that is not UTC, 400 ns past a microsecond. PostgreSQL and MySQL get a
// time on a whole microsecond. SQLite gets the text of the time in UTC, every digit kept,
// in the form in which its driver writes a time.Time.
func TestTimeRangePerDialect(t *testing.T) {
	bound := time.Date(2026, 11, 9, 12, 0, 0, 1_400, time.FixedZone("UTC-5", -5*3600))
	up, down := bound.Truncate(time.Microsecond).Add(time.Microsecond), bound.Truncate(time.Microsecond)
	cases := map[string]struct {
		filter  func(searchexec.TimeRange[dSearch]) dSearch
		op      string
		rounded time.Time
	}{
		"Since":  {func(f searchexec.TimeRange[dSearch]) dSearch { return f.Since(bound) }, ">=", up},
		"Until":  {func(f searchexec.TimeRange[dSearch]) dSearch { return f.Until(bound) }, "<=", down},
		"After":  {func(f searchexec.TimeRange[dSearch]) dSearch { return f.After(bound) }, ">", down},
		"Before": {func(f searchexec.TimeRange[dSearch]) dSearch { return f.Before(bound) }, "<", up},
	}
	const utcText = "2026-11-09 17:00:00.0000014+00:00"
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := dCount(t, func(q dSearch, _ keyset.Source) dSearch {
				return tc.filter(searchexec.NewTimeRange(q.Query, dEventAt))
			})
			where := "( (events.org = %s) AND (events.at " + tc.op + " %s) );"
			assert.Equal(t, statementSQL{
				"SELECT COUNT(*) FROM public.events WHERE " + fmt.Sprintf(where, "$1", "$2"),
				[]any{"org-a", tc.rounded},
			}, got["postgres"])
			assert.Equal(t, statementSQL{
				"SELECT COUNT(*) FROM db.events WHERE " + fmt.Sprintf(where, "?", "?"),
				[]any{"org-a", tc.rounded},
			}, got["mysql"])
			assert.Equal(t, statementSQL{
				"SELECT COUNT(*) FROM events WHERE " + fmt.Sprintf(where, "?", "?"),
				[]any{"org-a", utcText},
			}, got["sqlite"])
		})
	}
}

func TestPlanRunsOnEveryDialect(t *testing.T) {
	for dialect, tables := range dTables() {
		t.Run(dialect, func(t *testing.T) {
			spec := dSpec(tables[0])
			plan, err := searchexec.Resolve(t.Context(), spec, searchexec.New[dSearch](spec, "org-a").Query)
			require.NoError(t, err)
			order, err := plan.Order(nil)
			require.NoError(t, err)
			rec := &recorder{}
			_, err = plan.And(dEventID.EQ(postgres.String("e1"))).
				Run(t.Context(), rec, order, func(r dEvent) dEvent { return r })
			require.ErrorIs(t, err, errStop)
			assert.Contains(t, flat(rec.queries[0]), "(events.org = ", dialect)
			assert.Equal(t, []any{"org-a", "e1"}, rec.args[0])
			assert.Equal(t, search.Pagination{Limit: 10}, plan.Page())
		})
	}
}
