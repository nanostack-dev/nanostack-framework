// This file is Nanostack-only (not in go-jet): the table of the db/keyset
// contract test, its rows, and the checks that differ per dialect. See
// db/UPSTREAM.md.

package keysetcontract

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/db/keyset"
	"github.com/nanostack-dev/nanostack-framework/db/search"
)

func rawJSON(raw string) json.RawMessage { return json.RawMessage(raw) }

// binaryCollation is the bytewise, NO PAD collation of a flavor's text
// columns. MySQL's utf8mb4_bin is PAD SPACE ('a' equals 'a '), so it is not
// bytewise.
func (c *contract) binaryCollation() string {
	switch c.cfg.Flavor {
	case MySQL:
		return "utf8mb4_0900_bin"
	case MariaDB:
		return "utf8mb4_nopad_bin"
	case Postgres, SQLite:
	}
	return ""
}

// ddl creates the table. The enum type of PostgreSQL is created once and
// kept: the pgx driver caches the statements of each connection, and a cached
// statement names the type by its OID, which a type created again would
// change.
func (c *contract) ddl() []string {
	switch c.cfg.Flavor {
	case Postgres:
		return []string{
			`DO $$ BEGIN
				CREATE TYPE kc_status AS ENUM ('pending', 'active', 'archived');
			EXCEPTION WHEN duplicate_object THEN NULL;
			END $$`,
			`DROP TABLE IF EXISTS kc_rows`,
			`CREATE TABLE kc_rows (
				id text COLLATE "C" PRIMARY KEY,
				tenant text NOT NULL,
				name text COLLATE "C" NOT NULL,
				name_ci text NOT NULL,
				note text COLLATE "C",
				note_ci text,
				status kc_status NOT NULL,
				status_p kc_status,
				ref uuid NOT NULL,
				points bigint NOT NULL,
				points_p bigint,
				score double precision,
				ratio real NOT NULL,
				created timestamp(6) NOT NULL,
				seen timestamp(6),
				stamp timestamp(6)
			)`,
			`CREATE INDEX kc_rows_created ON kc_rows (created, id)`,
		}
	case MySQL, MariaDB:
		binary := "CHARACTER SET utf8mb4 COLLATE " + c.binaryCollation()
		enum := "ENUM('pending', 'active', 'archived')"
		return []string{
			`DROP TABLE IF EXISTS kc_rows`,
			`CREATE TABLE kc_rows (
				id VARCHAR(40) ` + binary + ` NOT NULL PRIMARY KEY,
				tenant VARCHAR(16) NOT NULL,
				name VARCHAR(400) ` + binary + ` NOT NULL,
				name_ci VARCHAR(400) CHARACTER SET utf8mb4 NOT NULL,
				note VARCHAR(400) ` + binary + ` NULL,
				note_ci VARCHAR(400) CHARACTER SET utf8mb4 NULL,
				status ` + enum + ` NOT NULL,
				status_p ` + enum + ` NULL,
				ref CHAR(36) NOT NULL,
				points BIGINT NOT NULL,
				points_p BIGINT NULL,
				score DOUBLE NULL,
				ratio FLOAT NOT NULL,
				created DATETIME(6) NOT NULL,
				seen DATETIME(6) NULL,
				stamp TIMESTAMP(6) NULL,
				KEY kc_rows_created (created, id)
			)`,
		}
	case SQLite:
		// SQLite's default collation is BINARY, the same as the binary part, so
		// the default-collation part uses NOCASE: the collation a product
		// declares to sort text case-insensitively.
		return []string{
			`DROP TABLE IF EXISTS kc_rows`,
			`CREATE TABLE kc_rows (
				id TEXT NOT NULL PRIMARY KEY,
				tenant TEXT NOT NULL,
				name TEXT NOT NULL,
				name_ci TEXT COLLATE NOCASE NOT NULL,
				note TEXT,
				note_ci TEXT COLLATE NOCASE,
				status TEXT NOT NULL,
				status_p TEXT,
				ref TEXT NOT NULL,
				points INTEGER NOT NULL,
				points_p INTEGER,
				score REAL,
				ratio REAL NOT NULL,
				created DATETIME NOT NULL,
				seen DATETIME,
				stamp DATETIME
			)`,
			`CREATE INDEX kc_rows_created ON kc_rows (created, id)`,
		}
	}
	panic("keysetcontract: unknown flavor " + string(c.cfg.Flavor))
}

func (c *contract) createTable(t *testing.T) {
	t.Helper()
	for _, stmt := range c.ddl() {
		_, err := c.cfg.DB.ExecContext(t.Context(), stmt)
		require.NoError(t, err, "setup %q", stmt)
	}
	t.Cleanup(func() {
		_, _ = c.cfg.DB.ExecContext(context.Background(), `DROP TABLE IF EXISTS kc_rows`)
	})
}

const insertColumns = "id, tenant, name, name_ci, note, note_ci, status, status_p, ref, points, points_p, score, " +
	"ratio, created, seen, stamp"

// insert writes rows as bound parameters, so each driver writes the stored
// form of every value itself. It sends up to 100 rows per statement.
func (c *contract) insert(t *testing.T, db DB, rows []Row) {
	t.Helper()
	const columns, batch = 16, 100
	for start := 0; start < len(rows); start += batch {
		chunk := rows[start:min(start+batch, len(rows))]
		tuples := make([]string, len(chunk))
		args := make([]any, 0, len(chunk)*columns)
		for i, r := range chunk {
			placeholders := make([]string, columns)
			for j := range placeholders {
				placeholders[j] = "?"
				if c.cfg.Flavor == Postgres {
					placeholders[j] = fmt.Sprintf("$%d", i*columns+j+1)
				}
			}
			tuples[i] = "(" + strings.Join(placeholders, ", ") + ")"
			args = append(args,
				r.ID, r.Tenant, r.Name, r.NameCI, r.Note, r.NoteCI, r.Status, r.StatusP, r.Ref.String(), r.Points,
				r.PointsP, r.Score, float64(r.Ratio), r.Created, r.Seen, r.Stamp)
		}
		stmt := "INSERT INTO kc_rows (" + insertColumns + ") VALUES " + strings.Join(tuples, ", ")
		_, err := db.ExecContext(t.Context(), stmt, args...)
		require.NoError(t, err, "insert rows %d to %d", start, start+len(chunk))
	}
}

// checkEnumPins pins the values an enum column holds besides its labels. MySQL
// stores an invalid label as ” (index 0) when sql_mode is not strict, and
// sorts it before every label. SQLite text holds any value, and the key sorts
// it before every label too. PostgreSQL holds labels only. NULL follows the
// contract everywhere: last when ascending, first when descending.
func (c *contract) checkEnumPins(t *testing.T) {
	base := c.rows[0]
	row := func(id string, status string, statusP *string) Row {
		r := base
		r.ID, r.Tenant, r.Status, r.StatusP = id, "enum", status, statusP
		return r
	}
	rows := []Row{
		row("enum-a", "active", nil),
		row("enum-p", "pending", new("pending")),
		row("enum-z", "archived", new("archived")),
	}
	c.insert(t, c.cfg.DB, rows)
	withUnknown := c.cfg.Flavor != Postgres
	switch c.cfg.Flavor {
	case MySQL, MariaDB:
		require.NotNil(t, c.cfg.Unstrict, "MySQL needs a connection without strict sql_mode")
		c.insert(t, c.cfg.Unstrict, []Row{row("enum-u", "bogus", new("bogus"))})
	case SQLite:
		c.insert(t, c.cfg.DB, []Row{row("enum-u", "bogus", new("bogus"))})
	case Postgres:
	}
	t.Cleanup(func() {
		_, _ = c.cfg.DB.ExecContext(context.Background(), `DELETE FROM kc_rows WHERE tenant = 'enum'`)
	})
	if withUnknown {
		stored := ""
		if c.cfg.Flavor == SQLite {
			stored = "bogus"
		}
		status, statusP := c.readStatus(t, "enum-u")
		require.Equal(t, stored, status, "the stored value of an invalid label")
		require.Equal(t, stored, statusP)
	}
	want := func(ids ...string) []string {
		var out []string
		for _, id := range ids {
			if id != "enum-u" || withUnknown {
				out = append(out, id)
			}
		}
		return out
	}
	asc, desc := search.SortAscending, search.SortDescending
	for _, tc := range []struct {
		sort search.Sort[string]
		want []string
	}{
		{by("status", asc), want("enum-u", "enum-p", "enum-a", "enum-z")},
		{by("status", desc), want("enum-z", "enum-a", "enum-p", "enum-u")},
		{by("status_p", asc), want("enum-u", "enum-p", "enum-z", "enum-a")},
		{by("status_p", desc), want("enum-a", "enum-z", "enum-p", "enum-u")},
	} {
		s := spec{sorts: []search.Sort[string]{tc.sort}, tenant: "enum", limit: 1}
		ids, _ := c.walk(t, s, c.order(t, s))
		require.Equal(t, tc.want, ids, "%s, one row per page", s)
	}
}

// readStatus reads the two enum columns of one row as the driver returns them.
func (c *contract) readStatus(t *testing.T, id string) (string, string) {
	t.Helper()
	query := "SELECT status, status_p FROM kc_rows WHERE id = '" + id + "'"
	rows, err := c.cfg.DB.QueryContext(t.Context(), query)
	require.NoError(t, err)
	defer func() { require.NoError(t, rows.Close()) }()
	var status, statusP string
	require.True(t, rows.Next())
	require.NoError(t, rows.Scan(&status, &statusP))
	require.NoError(t, rows.Err())
	return status, statusP
}

// recorder keeps the last statement a page sends, so EXPLAIN can read the
// exact SQL of the pager.
type recorder struct {
	DB
	query string
	args  []any
}

func (r *recorder) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	r.query, r.args = query, args
	return r.DB.QueryContext(ctx, query, args...)
}

// checkExplain asks the database for the plan of a page after a cursor, on a
// walk of NOT NULL keys with an index on (created, id). MySQL must not sort
// the matches (no "Using filesort") and must start a range scan at the cursor
// (the expanded seek; a row value scans the whole index). SQLite must search
// the index from the cursor and must not build a temporary B-tree for ORDER
// BY.
//
// The check first adds 3000 rows. On 200 rows, after the first page, the
// MySQL optimizer reads the whole table and sorts it, because that costs less
// than the index at that size, whatever the SQL. At 3200 rows it reads the
// index range in order, which is the plan a real table gets.
func (c *contract) checkExplain(t *testing.T) {
	var explain string
	switch c.cfg.Flavor {
	case MySQL, MariaDB:
		explain = "EXPLAIN "
	case SQLite:
		explain = "EXPLAIN QUERY PLAN "
	case Postgres:
		t.Skip("the design checks the plan on MySQL and SQLite, which need the explicit NULL placement rule")
	}
	bulk := make([]Row, 3000)
	for i := range bulk {
		bulk[i] = c.rows[i%len(c.rows)]
		bulk[i].ID, bulk[i].Tenant = fmt.Sprintf("bulk-%05d", i), "bulk"
	}
	c.insert(t, c.cfg.DB, bulk)
	t.Cleanup(func() {
		_, _ = c.cfg.DB.ExecContext(context.Background(), `DELETE FROM kc_rows WHERE tenant = 'bulk'`)
	})
	if c.cfg.Flavor == MySQL || c.cfg.Flavor == MariaDB {
		_, err := c.cfg.DB.ExecContext(t.Context(), `ANALYZE TABLE kc_rows`)
		require.NoError(t, err)
	}
	s := spec{sorts: []search.Sort[string]{by("created", search.SortAscending)}, limit: 5}
	order := c.order(t, s)
	first := c.run(t, c.cfg.DB, s, order, "", s.limit)
	require.NotEmpty(t, first.next)
	// A deep cursor: past every row but those at the latest time. Reading the
	// index from its start would cost far more than starting at the cursor.
	last := slices.MaxFunc(c.rows, func(a, b Row) int { return a.Created.Compare(b.Created) })
	last.ID = ""
	deep, err := order.Cursor(last)
	require.NoError(t, err)
	for name, cursor := range map[string]string{"after the first page": first.next, "deep": deep} {
		t.Run(name, func(t *testing.T) {
			rec := &recorder{DB: c.cfg.DB}
			_, runErr := keyset.Page(rec, func(r Row) Row { return r }, c.columns...).
				From(c.source).Keyset(order).Run(t.Context(), search.Pagination{Limit: s.limit, Cursor: cursor})
			require.NoError(t, runErr)
			require.Contains(t, rec.query, "ORDER BY", "the recorded statement is the page")
			plan := c.plan(t, explain+rec.query, rec.args)
			t.Logf("plan of %s:\n%s", rec.query, plan)
			switch c.cfg.Flavor {
			case MySQL, MariaDB:
				require.NotContains(t, plan, "filesort", "MySQL reads the index in order")
				if name == "deep" {
					// A row-value seek cannot drive a range: MySQL then scans the
					// whole index from its start ("type=index").
					require.Contains(t, plan, "type=range", "MySQL starts the scan at the cursor")
				}
			case SQLite:
				require.NotContains(t, plan, "TEMP B-TREE", "SQLite reads the index in order")
				require.Contains(t, plan, "SEARCH kc_rows USING INDEX kc_rows_created",
					"SQLite starts the scan at the cursor")
			case Postgres:
			}
		})
	}
}

// plan runs an EXPLAIN and joins its rows, one line per row.
func (c *contract) plan(t *testing.T, query string, args []any) string {
	t.Helper()
	rows, err := c.cfg.DB.QueryContext(t.Context(), query, args...)
	require.NoError(t, err)
	defer func() { require.NoError(t, rows.Close()) }()
	columns, err := rows.Columns()
	require.NoError(t, err)
	var lines []string
	for rows.Next() {
		values := make([]sql.NullString, len(columns))
		targets := make([]any, len(columns))
		for i := range values {
			targets[i] = &values[i]
		}
		require.NoError(t, rows.Scan(targets...))
		cells := make([]string, len(columns))
		for i, v := range values {
			cells[i] = columns[i] + "=" + v.String
		}
		lines = append(lines, strings.Join(cells, " "))
	}
	require.NoError(t, rows.Err())
	return strings.Join(lines, "\n")
}
