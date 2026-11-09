// This file is Nanostack-only (not in go-jet): the cross-dialect contract test
// of db/keyset (ADR 0015, design-search S3.5). See db/UPSTREAM.md.

// Package keysetcontract is the contract of the keyset pager, written once and
// run on PostgreSQL, MySQL, MariaDB and SQLite. One table, the same 200 rows
// and the same searches on every dialect. Each search walks every page by
// cursor.
//
//   - Binary collations: the text columns sort bytewise on every dialect, so
//     the test computes the expected rows and cursor tokens in Go, and every
//     dialect must return exactly them.
//   - Default collations: the same walks on text columns with the dialect's
//     own collation. Each walk must equal the database's own ORDER BY of the
//     full result: complete, no skip, no repeat.
//
// The rows go through each driver as bound parameters, never as SQL literals,
// so the time codecs see the stored format.
package keysetcontract

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/db/keyset"
	"github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/search"
)

// Flavor is the database a run targets.
type Flavor string

// The flavors of the contract.
const (
	Postgres Flavor = "postgres"
	MySQL    Flavor = "mysql"
	MariaDB  Flavor = "mariadb"
	SQLite   Flavor = "sqlite"
)

// DB runs the statements of the contract.
type DB interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// Config is what a dialect's suite gives the contract.
type Config struct {
	Flavor Flavor
	// DB holds the table. Its session time zone must be UTC.
	DB DB
	// Unstrict is a MySQL or MariaDB connection whose session sql_mode is
	// empty, so it can store an invalid ENUM label (as '', index 0). Other
	// flavors leave it nil.
	Unstrict DB
	// Schema is the schema of the table, as the dialect's NewTable takes it.
	Schema string
	// NewTable is the dialect package's NewTable.
	NewTable func(schema, name string, columns ...postgres.Column) keyset.Source
	// Text is the dialect package's string literal.
	Text func(string) postgres.StringExpression
}

// Row is one row of the contract table.
type Row struct {
	ID      string     `alias:"kc_rows.id"`
	Tenant  string     `alias:"kc_rows.tenant"`
	Name    string     `alias:"kc_rows.name"`
	NameCI  string     `alias:"kc_rows.name_ci"`
	Note    *string    `alias:"kc_rows.note"`
	NoteCI  *string    `alias:"kc_rows.note_ci"`
	Status  string     `alias:"kc_rows.status"`
	StatusP *string    `alias:"kc_rows.status_p"`
	Ref     uuid.UUID  `alias:"kc_rows.ref"`
	Points  int64      `alias:"kc_rows.points"`
	PointsP *int64     `alias:"kc_rows.points_p"`
	Score   *float64   `alias:"kc_rows.score"`
	Ratio   float32    `alias:"kc_rows.ratio"`
	Created time.Time  `alias:"kc_rows.created"`
	Seen    *time.Time `alias:"kc_rows.seen"`
	Stamp   *time.Time `alias:"kc_rows.stamp"`
}

// Labels are the enum labels in declaration order, which differs from their
// text order on purpose.
var Labels = []string{"pending", "active", "archived"}

// contract is one run: the table, its keys and the rows written.
type contract struct {
	cfg     Config
	source  keyset.Source
	columns []postgres.Projection
	tenant  postgres.ColumnString
	keys    map[string]keyDef
	id      keyDef
	rows    []Row
}

// Run runs the contract on one database.
func Run(t *testing.T, cfg Config) {
	t.Helper()
	c := newContract(cfg)
	c.createTable(t)
	c.rows = generateRows()
	c.insert(t, cfg.DB, c.rows)

	t.Run("binary collations give the rows and tokens computed in Go", func(t *testing.T) {
		for i, s := range c.binarySearches() {
			t.Run(fmt.Sprintf("%02d %s", i, s), func(t *testing.T) { c.checkBinary(t, s) })
		}
	})
	t.Run("default collations walk the database's own order", func(t *testing.T) {
		for i, s := range c.collationSearches() {
			t.Run(fmt.Sprintf("%02d %s", i, s), func(t *testing.T) { c.checkWalk(t, s) })
		}
	})
	t.Run("boundary cursors are never a database error", func(t *testing.T) { c.checkBoundaries(t) })
	t.Run("an enum value that is not a label, and NULL", func(t *testing.T) { c.checkEnumPins(t) })
	t.Run("a NOT NULL (created, id) walk reads the index in order", func(t *testing.T) { c.checkExplain(t) })
	t.Run("values that only one dialect stores", func(t *testing.T) { c.checkPins(t) })
}

func newContract(cfg Config) *contract {
	var (
		id      = postgres.StringColumn("id")
		tenant  = postgres.StringColumn("tenant")
		name    = postgres.StringColumn("name")
		nameCI  = postgres.StringColumn("name_ci")
		note    = postgres.StringColumn("note")
		noteCI  = postgres.StringColumn("note_ci")
		status  = postgres.StringColumn("status")
		statusP = postgres.StringColumn("status_p")
		ref     = postgres.StringColumn("ref")
		points  = postgres.IntegerColumn("points")
		pointsP = postgres.IntegerColumn("points_p")
		score   = postgres.FloatColumn("score")
		ratio   = postgres.FloatColumn("ratio")
		created = postgres.TimestampColumn("created")
		seen    = postgres.TimestampColumn("seen")
		stamp   = postgres.TimestampColumn("stamp")
	)
	all := []postgres.Column{
		id, tenant, name, nameCI, note, noteCI, status, statusP, ref, points, pointsP, score, ratio, created, seen,
		stamp,
	}
	c := &contract{cfg: cfg, source: cfg.NewTable(cfg.Schema, "kc_rows", all...), tenant: tenant}
	for _, column := range all {
		c.columns = append(c.columns, column)
	}
	c.id = keyDef{key: keyset.StringKey(id, func(r Row) string { return r.ID }), cmp: func(a, b Row) int {
		return strings.Compare(a.ID, b.ID)
	}}
	c.keys = contractKeys(
		cfg.Flavor,
		name,
		nameCI,
		note,
		noteCI,
		status,
		statusP,
		ref,
		points,
		pointsP,
		score,
		ratio,
		created,
		seen,
		stamp,
	)
	return c
}

// spec is one search of the contract.
type spec struct {
	sorts  []search.Sort[string]
	tenant string
	limit  int32
}

func (s spec) String() string {
	parts := make([]string, len(s.sorts))
	for i, sort := range s.sorts {
		parts[i] = sort.Field + " " + strings.ToLower(string(sort.Direction))
	}
	out := strings.Join(parts, ", ")
	if out == "" {
		out = "tiebreak only"
	}
	if s.tenant != "" {
		out += " where tenant=" + s.tenant
	}
	return fmt.Sprintf("%s, %d per page", out, s.limit)
}

func by(field string, direction search.SortDirection) search.Sort[string] {
	return search.Sort[string]{Field: field, Direction: direction}
}

var limits = []int32{1, 3, 7, 50}

// fullPage is a limit past the number of rows: one page holds every match.
const fullPage int32 = 1000

// binarySearches are the 40 searches whose rows and tokens Go computes, plus
// the MySQL TIMESTAMP walks on MySQL and MariaDB.
func (c *contract) binarySearches() []spec {
	single := []string{
		"name", "note", "note_nulls_first", "note_nulls_last", "status", "status_p", "ref", "points", "points_p",
		"score", "ratio", "created", "seen",
	}
	var out []spec
	for _, field := range single {
		out = append(out,
			spec{sorts: []search.Sort[string]{by(field, search.SortAscending)}},
			spec{sorts: []search.Sort[string]{by(field, search.SortDescending)}})
	}
	asc, desc := search.SortAscending, search.SortDescending
	out = append(out,
		spec{},
		spec{tenant: "t1"},
		spec{sorts: []search.Sort[string]{by("status", asc), by("created", desc)}},
		spec{sorts: []search.Sort[string]{by("points", asc), by("name", desc)}},
		spec{sorts: []search.Sort[string]{by("note", desc), by("score", asc)}},
		spec{sorts: []search.Sort[string]{by("status_p", desc), by("points_p", asc)}},
		spec{sorts: []search.Sort[string]{by("created", asc)}, tenant: "t1"},
		spec{sorts: []search.Sort[string]{by("seen", desc), by("note", asc)}},
		spec{sorts: []search.Sort[string]{by("score", desc), by("ratio", asc)}},
		spec{sorts: []search.Sort[string]{by("ref", desc), by("status", asc)}},
		spec{sorts: []search.Sort[string]{by("points_p", desc), by("seen", asc), by("name", asc)}},
		spec{sorts: []search.Sort[string]{by("note_nulls_last", asc), by("status_p", asc)}, tenant: "t2"},
		spec{sorts: []search.Sort[string]{by("name", asc), by("ref", asc)}, tenant: "t2"},
		spec{sorts: []search.Sort[string]{by("note_nulls_first", desc), by("created", desc)}},
	)
	if c.cfg.Flavor == MySQL || c.cfg.Flavor == MariaDB {
		out = append(out,
			spec{sorts: []search.Sort[string]{by("stamp", asc)}},
			spec{sorts: []search.Sort[string]{by("stamp", desc), by("points", asc)}})
	}
	for i := range out {
		out[i].limit = limits[i%len(limits)]
	}
	return out
}

// collationSearches order on the text columns that keep the dialect's own
// collation.
func (c *contract) collationSearches() []spec {
	asc, desc := search.SortAscending, search.SortDescending
	out := []spec{
		{sorts: []search.Sort[string]{by("name_ci", asc)}},
		{sorts: []search.Sort[string]{by("name_ci", desc)}},
		{sorts: []search.Sort[string]{by("note_ci", asc)}},
		{sorts: []search.Sort[string]{by("note_ci", desc)}},
		{sorts: []search.Sort[string]{by("name_ci", asc), by("note_ci", desc)}},
		{sorts: []search.Sort[string]{by("note_ci", asc), by("status", desc)}, tenant: "t1"},
		{sorts: []search.Sort[string]{by("status", asc), by("name_ci", asc)}},
		{sorts: []search.Sort[string]{by("name_ci", desc), by("created", asc)}, tenant: "t2"},
	}
	for i := range out {
		out[i].limit = limits[i%len(limits)]
	}
	return out
}

func (c *contract) order(t *testing.T, s spec) keyset.Order[Row] {
	t.Helper()
	keys := make(map[string]keyset.Key[Row], len(c.keys))
	for field, def := range c.keys {
		keys[field] = def.key
	}
	order, err := keyset.OrderOf(s.sorts, keys, c.id.key)
	require.NoError(t, err)
	return order
}

func (c *contract) where(s spec) postgres.BoolExpression {
	if s.tenant == "" {
		return nil
	}
	return c.tenant.EQ(c.cfg.Text(s.tenant))
}

// expected sorts the matching rows in Go by the contract: each key in its
// direction, NULLs last when ascending and first when descending unless the
// key asks otherwise, then the id in the direction of the last sort.
func (c *contract) expected(s spec) []Row {
	var rows []Row
	for _, row := range c.rows {
		if s.tenant == "" || row.Tenant == s.tenant {
			rows = append(rows, row)
		}
	}
	direction := search.SortAscending
	type step struct {
		def       keyDef
		direction search.SortDirection
	}
	var steps []step
	for _, sort := range s.sorts {
		direction = sort.Direction
		steps = append(steps, step{c.keys[sort.Field], sort.Direction})
	}
	steps = append(steps, step{c.id, direction})
	slices.SortStableFunc(rows, func(a, b Row) int {
		for _, st := range steps {
			if r := compareKey(st.def, st.direction, a, b); r != 0 {
				return r
			}
		}
		return 0
	})
	return rows
}

func compareKey(def keyDef, direction search.SortDirection, a, b Row) int {
	if def.null != nil {
		aNull, bNull := def.null(a), def.null(b)
		nullsAfter := def.nulls == "last" || (def.nulls == "" && direction == search.SortAscending)
		switch {
		case aNull && bNull:
			return 0
		case aNull && nullsAfter, bNull && !nullsAfter:
			return 1
		case aNull, bNull:
			return -1
		}
	}
	r := def.cmp(a, b)
	if direction == search.SortDescending {
		return -r
	}
	return r
}

// page is one page of a walk.
type page struct {
	ids   []string
	next  string
	total int64
}

func (c *contract) run(t *testing.T, db DB, s spec, order keyset.Order[Row], cursor string, limit int32) page {
	t.Helper()
	result, err := keyset.Page(db, func(r Row) Row { return r }, c.columns...).
		From(c.source).Where(c.where(s)).Keyset(order).
		Run(t.Context(), search.Pagination{Limit: limit, Cursor: cursor})
	require.NoError(t, err)
	ids := make([]string, len(result.Items))
	for i, row := range result.Items {
		ids[i] = row.ID
	}
	return page{ids: ids, next: result.NextCursor, total: result.Total}
}

// walk reads every page by cursor and returns the ids and the next cursors.
func (c *contract) walk(t *testing.T, s spec, order keyset.Order[Row]) ([]string, []string) {
	t.Helper()
	var ids, tokens []string
	cursor := ""
	for range len(c.rows) + 2 {
		p := c.run(t, c.cfg.DB, s, order, cursor, s.limit)
		ids = append(ids, p.ids...)
		if p.next == "" {
			return ids, tokens
		}
		require.Len(t, p.ids, int(s.limit), "a page before the last is full")
		tokens = append(tokens, p.next)
		cursor = p.next
	}
	t.Fatalf("the walk did not end after %d pages", len(c.rows)+2)
	return nil, nil
}

func (c *contract) checkBinary(t *testing.T, s spec) {
	order := c.order(t, s)
	want := c.expected(s)
	wantIDs := make([]string, len(want))
	for i, row := range want {
		wantIDs[i] = row.ID
	}
	var wantTokens []string
	for end := int(s.limit); end < len(want); end += int(s.limit) {
		token, err := order.Cursor(want[end-1])
		require.NoError(t, err)
		wantTokens = append(wantTokens, token)
	}
	ids, tokens := c.walk(t, s, order)
	require.Equal(t, wantIDs, ids, "the walk returns the rows in the order computed in Go")
	require.Equal(t, wantTokens, tokens, "the cursor tokens are the ones computed in Go")
	require.Equal(t, int64(len(want)), c.run(t, c.cfg.DB, s, order, "", s.limit).total)
}

func (c *contract) checkWalk(t *testing.T, s spec) {
	order := c.order(t, s)
	full := c.run(t, c.cfg.DB, s, order, "", fullPage)
	require.Empty(t, full.next)
	require.Len(t, full.ids, int(full.total))
	ids, _ := c.walk(t, s, order)
	require.Equal(t, full.ids, ids, "the walk equals the database's own ORDER BY: no skip, no repeat")
}

// checkBoundaries seeks from the extreme values that a codec accepts. Each
// must run, or be refused before the query: never a database error.
func (c *contract) checkBoundaries(t *testing.T) {
	values := map[string][]string{
		"name":   {`"a\u0000b"`, `"` + strings.Repeat("z", 300) + `"`},
		"status": {`""`, `"zzz"`, `"archived"`},
		"ref":    {`"FFFFFFFF-FFFF-FFFF-FFFF-FFFFFFFFFFFF"`, `"00000000-0000-0000-0000-000000000000"`},
		"points": {"9223372036854775807", "-9223372036854775808"},
		"score":  {"1.7976931348623157e308", "-1.7976931348623157e308", `"Infinity"`, `"-Infinity"`, `"NaN"`},
		"ratio":  {"3.4028234663852886e38", `"Infinity"`, `"NaN"`},
		"created": {
			micros(time.Date(-4713, time.November, 24, 0, 0, 0, 0, time.UTC)),
			micros(time.Date(0, time.January, 1, 0, 0, 0, 0, time.UTC)),
			micros(time.Date(1000, time.January, 1, 0, 0, 0, 0, time.UTC)),
			micros(time.Date(9999, time.December, 31, 23, 59, 59, 999_999_000, time.UTC)),
			micros(time.Date(294276, time.December, 31, 23, 59, 59, 999_999_000, time.UTC)),
		},
		"stamp": {
			micros(time.Date(1970, time.January, 1, 0, 0, 1, 0, time.UTC)),
			micros(time.Date(2038, time.January, 19, 3, 14, 7, 999_999_000, time.UTC)),
		},
	}
	for _, field := range slices.Sorted(func(yield func(string) bool) {
		for field := range values {
			if !yield(field) {
				return
			}
		}
	}) {
		for _, raw := range values[field] {
			s := spec{sorts: []search.Sort[string]{by(field, search.SortAscending)}, limit: 5}
			order := c.order(t, s)
			cursor, err := search.EncodeCursor(order.Signature(), []any{rawJSON(raw), "zz"})
			require.NoError(t, err)
			_, err = keyset.Page(c.cfg.DB, func(r Row) Row { return r }, c.columns...).
				From(c.source).Keyset(order).Run(t.Context(), search.Pagination{Limit: 5, Cursor: cursor})
			if err != nil && !errors.Is(err, search.ErrInvalidCursor) && !errors.Is(err, keyset.ErrDialect) {
				t.Errorf("%s %s: a database error, not a refusal: %v", field, raw, err)
			}
		}
	}
}

func micros(v time.Time) string {
	epoch := time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)
	return fmt.Sprint((v.Unix()-epoch.Unix())*1_000_000 + int64(v.Nanosecond()/1_000))
}
