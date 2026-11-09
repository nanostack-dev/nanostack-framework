// This file is Nanostack-only (not in go-jet): the db/keyset contract pins for
// values that only one dialect stores. See db/UPSTREAM.md.

package keysetcontract

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/db/keyset"
	"github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/search"
)

// pinRow is one row of the pins table. Each pin owns a tenant, so the pins
// cannot change the 200 shared rows, whose tokens must be the same on every
// dialect.
type pinRow struct {
	ID     string     `alias:"kc_pins.id"`
	At     *time.Time `alias:"kc_pins.at"`
	Stamp  *time.Time `alias:"kc_pins.stamp"`
	Status *string    `alias:"kc_pins.status"`
	Ratio  *float64   `alias:"kc_pins.ratio"`
}

type pins struct {
	c       *contract
	source  keyset.Source
	columns []postgres.Projection
	tenant  postgres.ColumnString
	id      keyset.Key[pinRow]
	at      postgres.ColumnTimestamp
	stamp   postgres.ColumnTimestamp
	status  postgres.ColumnString
	ratio   postgres.ColumnFloat
}

func (c *contract) pinsDDL() []string {
	switch c.cfg.Flavor {
	case SQLite:
		return []string{
			`DROP TABLE IF EXISTS kc_pins`,
			`CREATE TABLE kc_pins (
				id TEXT NOT NULL PRIMARY KEY,
				tenant TEXT NOT NULL,
				at DATETIME DEFAULT CURRENT_TIMESTAMP,
				stamp DATETIME,
				status TEXT COLLATE NOCASE,
				ratio REAL
			)`,
		}
	case MySQL, MariaDB:
		binary := "CHARACTER SET utf8mb4 COLLATE " + c.binaryCollation()
		return []string{
			`DROP TABLE IF EXISTS kc_pins`,
			`CREATE TABLE kc_pins (
				id VARCHAR(64) ` + binary + ` NOT NULL PRIMARY KEY,
				tenant VARCHAR(16) NOT NULL,
				at DATETIME(6) NULL,
				stamp TIMESTAMP(6) NULL DEFAULT NULL,
				status VARCHAR(16) NULL,
				ratio DOUBLE NULL
			)`,
		}
	case Postgres:
	}
	return nil
}

// checkPins walks the values that only MySQL or only SQLite stores:
//
//   - MySQL: the zero date, in a DATETIME and a TIMESTAMP, written without
//     strict sql_mode and walked through the strict connection. The MariaDB
//     11.5 TIMESTAMP bound runs on the server in CI.
//   - SQLite: times finer than a microsecond, times in the text form that SQL
//     writes (CURRENT_TIMESTAMP), an enum column with the NOCASE collation,
//     and a REAL column, which RealKey refuses.
func (c *contract) checkPins(t *testing.T) {
	ddl := c.pinsDDL()
	if ddl == nil {
		t.Skip("PostgreSQL stores none of these values")
	}
	for _, stmt := range ddl {
		_, err := c.cfg.DB.ExecContext(t.Context(), stmt)
		require.NoError(t, err, "setup %q", stmt)
	}
	t.Cleanup(func() {
		_, _ = c.cfg.DB.ExecContext(context.Background(), `DROP TABLE IF EXISTS kc_pins`)
	})
	var (
		id     = postgres.StringColumn("id")
		tenant = postgres.StringColumn("tenant")
		at     = postgres.TimestampColumn("at")
		stamp  = postgres.TimestampColumn("stamp")
		status = postgres.StringColumn("status")
		ratio  = postgres.FloatColumn("ratio")
	)
	all := []postgres.Column{id, tenant, at, stamp, status, ratio}
	p := &pins{
		c:      c,
		source: c.cfg.NewTable(c.cfg.Schema, "kc_pins", all...),
		tenant: tenant,
		id:     keyset.StringKey(id, func(r pinRow) string { return r.ID }),
		at:     at,
		stamp:  stamp,
		status: status,
		ratio:  ratio,
	}
	// The rows read every column but the tenant, which pinRow does not hold.
	p.columns = []postgres.Projection{id, at, stamp, status, ratio}
	switch c.cfg.Flavor {
	case MySQL, MariaDB:
		t.Run("the MySQL zero date", p.checkZeroDate)
		t.Run("the MariaDB 11.5 TIMESTAMP bound", p.checkMariaDBBound)
	case SQLite:
		t.Run("SQLite times finer than a microsecond", p.checkFineTimes)
		t.Run("SQLite times in the text form of CURRENT_TIMESTAMP", p.checkSQLTimes)
		t.Run("a SQLite enum with the NOCASE collation", p.checkNoCaseEnum)
		t.Run("RealKey on SQLite", p.checkRealKey)
	case Postgres:
	}
}

func (p *pins) exec(t *testing.T, db DB, stmt string, args ...any) {
	t.Helper()
	_, err := db.ExecContext(t.Context(), stmt, args...)
	require.NoError(t, err, "%s", stmt)
}

func (p *pins) order(t *testing.T, key keyset.Key[pinRow], direction search.SortDirection) keyset.Order[pinRow] {
	t.Helper()
	order, err := keyset.OrderOf(
		[]search.Sort[string]{{Field: "key", Direction: direction}},
		map[string]keyset.Key[pinRow]{"key": key},
		p.id,
	)
	require.NoError(t, err)
	return order
}

func (p *pins) page(
	tenant string, order keyset.Order[pinRow], cursor string, limit int32,
) (search.Result[pinRow], error) {
	return keyset.Page(p.c.cfg.DB, func(r pinRow) pinRow { return r }, p.columns...).
		From(p.source).Where(p.tenant.EQ(p.c.cfg.Text(tenant))).Keyset(order).
		Run(context.Background(), search.Pagination{Limit: limit, Cursor: cursor})
}

// walk reads every page by cursor. It stops at the first error.
func (p *pins) walk(tenant string, order keyset.Order[pinRow], limit int32) ([]string, error) {
	var ids []string
	cursor := ""
	for range 50 {
		result, err := p.page(tenant, order, cursor, limit)
		if err != nil {
			return ids, err
		}
		for _, row := range result.Items {
			ids = append(ids, row.ID)
		}
		if result.NextCursor == "" {
			return ids, nil
		}
		cursor = result.NextCursor
	}
	return ids, errors.New("the walk did not end after 50 pages")
}

// rows reads the rows of tenant as the driver returns them.
func (p *pins) rows(t *testing.T, tenant string) []pinRow {
	t.Helper()
	result, err := keyset.Page(p.c.cfg.DB, func(r pinRow) pinRow { return r }, p.columns...).
		From(p.source).Where(p.tenant.EQ(p.c.cfg.Text(tenant))).
		Run(t.Context(), search.Pagination{Limit: 100})
	require.NoError(t, err)
	return result.Items
}

// expect sorts the rows of tenant in Go: compare in direction, then the id in
// the same direction. compare must order NULLs as the key does.
func (p *pins) expect(
	t *testing.T, tenant string, direction search.SortDirection, compare func(a, b pinRow) int,
) []string {
	t.Helper()
	rows := p.rows(t, tenant)
	slices.SortFunc(rows, func(a, b pinRow) int {
		r := compare(a, b)
		if r == 0 {
			r = strings.Compare(a.ID, b.ID)
		}
		if direction == search.SortDescending {
			return -r
		}
		return r
	})
	ids := make([]string, len(rows))
	for i, row := range rows {
		ids[i] = row.ID
	}
	return ids
}

// checkWalks walks key over tenant in both directions at each limit, and
// compares the ids with the order computed in Go.
func (p *pins) checkWalks(
	t *testing.T, tenant string, key keyset.Key[pinRow], compare func(a, b pinRow) int, limits ...int32,
) {
	t.Helper()
	for _, direction := range []search.SortDirection{search.SortAscending, search.SortDescending} {
		want := p.expect(t, tenant, direction, compare)
		require.NotEmpty(t, want)
		for _, limit := range limits {
			got, err := p.walk(tenant, p.order(t, key, direction), limit)
			require.NoError(t, err, "%s, %d per page", direction, limit)
			require.Equal(t, want, got, "%s, %d per page: no skip, no repeat", direction, limit)
		}
	}
}

// timeOf orders a NOT NULL time.
func timeOf(get func(pinRow) *time.Time) func(a, b pinRow) int {
	return func(a, b pinRow) int { return get(a).Compare(*get(b)) }
}

func atOf(r pinRow) *time.Time    { return r.At }
func stampOf(r pinRow) *time.Time { return r.Stamp }

// checkZeroDate pins the MySQL zero date. go-sql-driver reads it as
// time.Time{}, which sorts before every date. The key seeks past it through
// the column's first valid date, because strict sql_mode refuses a zero-date
// literal.
func (p *pins) checkZeroDate(t *testing.T) {
	require.NotNil(t, p.c.cfg.Unstrict, "MySQL needs a connection without strict sql_mode")
	const zero = "0000-00-00 00:00:00"
	p.exec(t, p.c.cfg.Unstrict, `INSERT INTO kc_pins (id, tenant, at, stamp) VALUES
		('z1', 'zero', ?, ?), ('z2', 'zero', ?, ?), ('z3', 'zero', '2026-01-01 00:00:00', '2026-01-01 00:00:00'),
		('z4', 'zero', ?, NULL), ('z5', 'zero', '1000-01-01 00:00:00', '1970-01-01 00:00:01')`,
		zero, zero, zero, zero, zero)
	rows := p.rows(t, "zero")
	require.Len(t, rows, 5)
	for _, row := range rows {
		if row.ID == "z1" {
			require.True(t, row.At.IsZero(), "the driver reads the zero date as time.Time{}")
			require.True(t, row.Stamp.IsZero())
		}
	}
	p.checkWalks(t, "zero", keyset.LocalTimeKey(p.at, func(r pinRow) time.Time { return *r.At }), timeOf(atOf), 1, 2)
	p.checkWalks(t, "zero", keyset.NullableMySQLTimestampKey(p.stamp, stampOf), func(a, b pinRow) int {
		// NULLs sort last when ascending, first when descending.
		switch {
		case a.Stamp == nil && b.Stamp == nil:
			return 0
		case a.Stamp == nil:
			return 1
		case b.Stamp == nil:
			return -1
		}
		return a.Stamp.Compare(*b.Stamp)
	}, 1)
}

// checkMariaDBBound seeks from the last TIMESTAMP of MariaDB 11.5. A server
// whose TIMESTAMP ends in 2038 must run the seek, not fail it.
func (p *pins) checkMariaDBBound(t *testing.T) {
	p.exec(t, p.c.cfg.DB, `INSERT INTO kc_pins (id, tenant, stamp) VALUES ('m1', 'maria', '2030-01-01 00:00:00')`)
	key := keyset.NullableMariaDBTimestampKey(p.stamp, stampOf)
	for _, last := range []time.Time{
		time.Date(2038, time.January, 19, 3, 14, 8, 0, time.UTC),
		time.Date(2106, time.February, 7, 6, 28, 15, 999_999_000, time.UTC),
	} {
		order := p.order(t, key, search.SortAscending)
		cursor, err := order.Cursor(pinRow{ID: "", Stamp: &last})
		require.NoError(t, err)
		result, err := p.page("maria", order, cursor, 5)
		require.NoError(t, err, "a cursor at %s", last)
		require.Empty(t, result.Items, "no row is later than %s", last)
	}
}

// checkFineTimes pins times finer than a microsecond, which the SQLite driver
// writes with nine fraction digits. A cursor cut to microseconds would sort
// before its own row and repeat it.
func (p *pins) checkFineTimes(t *testing.T) {
	base := time.Date(2026, time.March, 4, 10, 11, 12, 123_456_000, time.UTC)
	stmt := `INSERT INTO kc_pins (id, tenant, at) VALUES (?, 'fine', ?)`
	for i, nanos := range []int{100, 200, 300, 100, 0, 1_000, 999_999_999 - 123_456_000} {
		p.exec(t, p.c.cfg.DB, stmt, fmt.Sprintf("f%d", i), base.Add(time.Duration(nanos)))
	}
	rows := p.rows(t, "fine")
	require.Len(t, rows, 7)
	require.True(t, slices.ContainsFunc(rows, func(r pinRow) bool { return r.At.Nanosecond()%1_000 != 0 }),
		"the driver keeps the nanoseconds")
	key := keyset.LocalTimeKey(p.at, func(r pinRow) time.Time { return *r.At })
	p.checkWalks(t, "fine", key, timeOf(atOf), 1, 2)
}

var sqlTimeText = regexp.MustCompile(`^\d{4}-\d\d-\d\d \d\d:\d\d:\d\d$`)

// checkSQLTimes pins a column that SQL fills: CURRENT_TIMESTAMP writes
// "2006-01-02 15:04:05", with no fraction and no offset. SQLiteLayout walks
// it. The driver's form would skip the rows that tie with the cursor row when
// ascending. When descending, the cursor row matches its own seek, and Run
// refuses the page with ErrNoProgress.
func (p *pins) checkSQLTimes(t *testing.T) {
	p.exec(t, p.c.cfg.DB, `INSERT INTO kc_pins (id, tenant, at) VALUES
		('s1', 'sql', '2026-03-04 10:11:12'), ('s2', 'sql', '2026-03-04 10:11:12'),
		('s3', 'sql', '2026-03-04 10:11:13'), ('s4', 'sql', '2026-03-04 10:11:11')`)
	p.exec(t, p.c.cfg.DB, `INSERT INTO kc_pins (id, tenant) VALUES ('s5', 'sql')`)
	rows, err := p.c.cfg.DB.QueryContext(t.Context(), `SELECT CAST(at AS TEXT) FROM kc_pins WHERE id = 's5'`)
	require.NoError(t, err)
	defer func() { require.NoError(t, rows.Close()) }()
	var stored string
	require.True(t, rows.Next())
	require.NoError(t, rows.Scan(&stored))
	require.NoError(t, rows.Err())
	require.Regexp(t, sqlTimeText, stored, "the text form of CURRENT_TIMESTAMP")

	value := func(r pinRow) time.Time { return *r.At }
	sqlKey := keyset.LocalTimeKey(p.at, value).SQLiteLayout("2006-01-02 15:04:05")
	p.checkWalks(t, "sql", sqlKey, timeOf(atOf), 1, 2)

	driverKey := keyset.LocalTimeKey(p.at, value)
	ascending, err := p.walk("sql", p.order(t, driverKey, search.SortAscending), 1)
	require.NoError(t, err)
	require.Equal(t, []string{"s4", "s1", "s3", "s5"}, ascending, "the driver's form skips s2, the tie of s1")
	_, err = p.walk("sql", p.order(t, driverKey, search.SortDescending), 1)
	require.ErrorIs(t, err, keyset.ErrNoProgress, "the driver's form matches its own cursor row")

	order := p.order(t, sqlKey, search.SortAscending)
	fraction := time.Date(2026, time.March, 4, 10, 11, 12, 500_000_000, time.UTC)
	cursor, err := order.Cursor(pinRow{ID: "s1", At: &fraction})
	require.NoError(t, err)
	_, err = p.page("sql", order, cursor, 1)
	require.ErrorIs(t, err, search.ErrInvalidCursor, "the layout cannot write the fraction")
}

// checkNoCaseEnum pins an enum on a NOCASE column. 'ACTIVE' is not the label
// "active": it sorts with the values that are not labels, as Go's exact match
// says.
func (p *pins) checkNoCaseEnum(t *testing.T) {
	p.exec(t, p.c.cfg.DB, `INSERT INTO kc_pins (id, tenant, status) VALUES
		('e1', 'enum', 'pending'), ('e2', 'enum', 'ACTIVE'), ('e3', 'enum', 'active'),
		('e4', 'enum', 'archived'), ('e5', 'enum', 'PENDING'), ('e6', 'enum', 'active')`)
	position := func(r pinRow) int { return slices.Index(Labels, *r.Status) }
	key := keyset.EnumKey(p.status, "kc_status", Labels, func(r pinRow) string { return *r.Status })
	p.checkWalks(t, "enum", key, func(a, b pinRow) int { return cmp.Compare(position(a), position(b)) }, 1, 2)
}

// checkRealKey pins RealKey on SQLite, whose REAL is a double: Page refuses
// it before any statement runs.
func (p *pins) checkRealKey(t *testing.T) {
	p.exec(t, p.c.cfg.DB, `INSERT INTO kc_pins (id, tenant, ratio) VALUES ('r1', 'real', 0.1), ('r2', 'real', 0.1)`)
	key := keyset.RealKey(p.ratio, func(r pinRow) float32 { return float32(*r.Ratio) })
	_, err := p.page("real", p.order(t, key, search.SortAscending), "", 1)
	require.ErrorIs(t, err, keyset.ErrDialect)
	p.checkWalks(t, "real", keyset.FloatKey(p.ratio, func(r pinRow) float64 { return *r.Ratio }),
		func(a, b pinRow) int { return cmp.Compare(*a.Ratio, *b.Ratio) }, 1)
}
