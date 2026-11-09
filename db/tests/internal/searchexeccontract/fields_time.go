// This file is Nanostack-only (not in go-jet): the checks of the db/searchexec TimeRange
// field (R7, R10) on every dialect. See db/UPSTREAM.md.

package searchexeccontract

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/db/keyset"
	. "github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/search"
	"github.com/nanostack-dev/nanostack-framework/db/searchexec"
)

// timeColumns makes the time columns of the items table with the type that the generated
// model of each dialect has, and binds a TimeRange to each one.
func (f *sfFixture) timeColumns(s suite) {
	if s.postgresLike() {
		at, atLocal := TimestampzColumn("at"), TimestampColumn("at_local")
		f.at, f.atLocal = at, atLocal
		f.timeRanges = map[string]func(sfSearch) searchexec.TimeRange[sfSearch]{
			"timestamptz": func(q sfSearch) searchexec.TimeRange[sfSearch] { return searchexec.NewTimeRange(q.Query, at) },
			"timestamp": func(q sfSearch) searchexec.TimeRange[sfSearch] {
				return searchexec.NewLocalTimeRange(q.Query, atLocal)
			},
		}
		return
	}
	at := TimestampColumn("at")
	f.at, f.atLocal = at, TimestampColumn("at_local")
	f.timeRanges = map[string]func(sfSearch) searchexec.TimeRange[sfSearch]{
		"local": func(q sfSearch) searchexec.TimeRange[sfSearch] { return searchexec.NewLocalTimeRange(q.Query, at) },
	}
}

// fieldsTimeRange checks the four bounds on each time column. Rows 2 and 4 are exactly on
// sfT0.
func (s suite) fieldsTimeRange(t *testing.T, f *sfFixture) {
	first := time.Date(1, time.January, 2, 0, 0, 0, 0, time.UTC)
	last := time.Date(9999, time.December, 30, 23, 59, 59, 999_999_000, time.UTC)
	for column, at := range f.timeRanges {
		t.Run(column, func(t *testing.T) {
			q := f.search("org-a")
			r := func() searchexec.TimeRange[sfSearch] { return at(q) }
			cases := map[string]struct {
				q    sfSearch
				want []int64
			}{
				"Since: on the bound":      {r().Since(sfT0), []int64{2, 3, 4, 5}},
				"After: not on the bound":  {r().After(sfT0), []int64{3, 5}},
				"Until: on the bound":      {r().Until(sfT0), []int64{1, 2, 4}},
				"Before: not on the bound": {r().Before(sfT0), []int64{1}},
				"Since and Before":         {at(r().Since(sfT0)).Before(sfT0.Add(2 * time.Second)), []int64{2, 3, 4}},
				// A bound with nanoseconds: the same answer on every dialect and driver.
				"Since t0+600ns":  {r().Since(sfT0.Add(600 * time.Nanosecond)), []int64{3, 5}},
				"Since t0+400ns":  {r().Since(sfT0.Add(400 * time.Nanosecond)), []int64{3, 5}},
				"Before t0+400ns": {r().Before(sfT0.Add(400 * time.Nanosecond)), []int64{1, 2, 4}},
				"Until t0-400ns":  {r().Until(sfT0.Add(-400 * time.Nanosecond)), []int64{1}},
				"After t0-600ns":  {r().After(sfT0.Add(-600 * time.Nanosecond)), []int64{2, 3, 4, 5}},
				// The first and the last bound that every dialect holds run, also in a zone
				// that moves them by a day.
				"Since the first bound":  {r().Since(first), sfAllA},
				"After the first bound":  {r().After(first.In(time.FixedZone("UTC-12", -12*3600))), sfAllA},
				"Until the last bound":   {r().Until(last.In(time.FixedZone("UTC+14", 14*3600))), sfAllA},
				"Before the last bound":  {r().Before(last), sfAllA},
				"Until the first bound":  {r().Until(first), []int64{}},
				"Since the last bound":   {r().Since(last), []int64{}},
				"Before the first bound": {r().Before(first), []int64{}},
			}
			for name, tc := range cases {
				t.Run(name, func(t *testing.T) {
					assert.Equal(t, tc.want, s.ids(t, f, tc.q))
				})
			}
			// A bound outside the years that every dialect holds is a client error, before
			// any statement runs: the MySQL driver refuses a year outside 1 to 9999, and
			// SQLite text sorts in time order for four-digit years only.
			late := time.Date(9999, time.December, 31, 23, 0, 0, 0, time.FixedZone("UTC-5", -5*3600))
			early := time.Date(0, time.June, 1, 0, 0, 0, 0, time.UTC)
			refused := map[string]sfSearch{
				"Since year 10000":  r().Since(late),
				"After year 10000":  r().After(late),
				"Until year 10000":  r().Until(late),
				"Before year 10000": r().Before(late),
				"Since year 0":      r().Since(early),
				"Before year 0":     r().Before(early),
			}
			for name, q := range refused {
				t.Run(name, func(t *testing.T) {
					_, err := searchexec.Run(
						t.Context(),
						s.cfg.DB,
						f.spec,
						q.Query,
						func(r sfRow) int64 { return r.ID },
					)
					require.ErrorIs(t, err, search.ErrInvalidTime)
					assert.True(t, search.IsClientError(err))
				})
			}
		})
	}
	if s.cfg.Flavor == SQLite {
		t.Run("SQLite layout", func(t *testing.T) { s.fieldsTimeRangeSQLiteLayout(t) })
	}
}

// fieldsTimeRangeSQLiteLayout checks a SQLite column that SQL fills, with the text form of
// CURRENT_TIMESTAMP. Row 2 is exactly on the bound. Without the layout, its text
// "2026-11-09 17:00:00" is a prefix of the bound "2026-11-09 17:00:00+00:00", and compares
// as smaller.
func (s suite) fieldsTimeRangeSQLiteLayout(t *testing.T) {
	name := s.name("nanostack_sf_sql_times")
	s.create(t, map[string]string{name: `id BIGINT PRIMARY KEY, org TEXT NOT NULL, at DATETIME NOT NULL`}, name)
	s.exec(t, "INSERT INTO "+s.qualified(name)+" (id, org, at) VALUES "+
		"(1, 'org-a', '2026-11-09 16:59:59'), (2, 'org-a', '2026-11-09 17:00:00'), (3, 'org-a', '2026-11-09 17:00:01'), "+
		"(4, 'org-c', CURRENT_TIMESTAMP)")
	id, org, at := IntegerColumn("id"), StringColumn("org"), TimestampColumn("at")
	table := s.table(name, id, org, at)
	key := keyset.IntKey(id, func(r sfRow) int64 { return r.ID })
	spec := searchexec.DefineSpec(searchexec.SpecDef[sfRow, sfSort, string]{
		From:     func(string) keyset.Source { return table },
		Columns:  ProjectionList{id.AS("sf_row.id")},
		Scope:    []searchexec.ScopeCol[string]{searchexec.ScopeEq(searchexec.Value[string], org)},
		Keys:     map[sfSort]keyset.Key[sfRow]{sfID: key},
		Tiebreak: []keyset.Key[sfRow]{key},
		Default:  searchexec.Asc(sfID),
		Limit:    search.MaxLimit,
	})
	q := searchexec.New[sfSearch](spec, "org-a")
	now := searchexec.New[sfSearch](spec, "org-c")
	r := func() searchexec.TimeRange[sfSearch] {
		return searchexec.NewLocalTimeRange(q.Query, at).SQLiteLayout("2006-01-02 15:04:05")
	}
	bound := time.Date(2026, 11, 9, 17, 0, 0, 0, time.UTC)
	half := 500 * time.Millisecond
	cases := map[string]struct {
		q    sfSearch
		want []int64
	}{
		"Since: on the bound":        {r().Since(sfT0), []int64{2, 3}},
		"Until: on the bound":        {r().Until(bound), []int64{1, 2}},
		"After: not on the bound":    {r().After(bound), []int64{3}},
		"Before: not on the bound":   {r().Before(bound), []int64{1}},
		"Since half a second before": {r().Since(bound.Add(-half)), []int64{2, 3}},
		"Before half a second after": {r().Before(bound.Add(half)), []int64{1, 2}},
		"Until half a second after":  {r().Until(bound.Add(half)), []int64{1, 2}},
		"After half a second before": {r().After(bound.Add(-half)), []int64{2, 3}},
		"Since half a second after":  {r().Since(bound.Add(half)), []int64{3}},
		"Until half a second before": {r().Until(bound.Add(-half)), []int64{1}},
		// The row that CURRENT_TIMESTAMP wrote, in the org-c scope.
		"CURRENT_TIMESTAMP, since an hour ago": {
			searchexec.NewLocalTimeRange(now.Query, at).
				SQLiteLayout("2006-01-02 15:04:05").
				Since(time.Now().Add(-time.Hour)),
			[]int64{4},
		},
		"CURRENT_TIMESTAMP, before an hour ago": {
			searchexec.NewLocalTimeRange(now.Query, at).
				SQLiteLayout("2006-01-02 15:04:05").
				Before(time.Now().Add(-time.Hour)),
			[]int64{},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			result, err := searchexec.Run(t.Context(), s.cfg.DB, spec, tc.q.Query, func(r sfRow) int64 { return r.ID })
			require.NoError(t, err)
			got := result.Items
			if got == nil {
				got = []int64{}
			}
			assert.Equal(t, tc.want, got)
		})
	}
}
