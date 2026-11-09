package keyset_test

// The dialect tests pin the SQL that a page writes on PostgreSQL, MySQL and
// SQLite (testdata/dialects.golden), and the answer of every codec on every
// dialect to hostile cursor values (testdata/hostile_cursors.golden). Run
// "go test ./keyset -run 'TestDialect|TestHostile' -update" to rewrite them
// after a deliberate change, and review the diff: a changed PostgreSQL line
// is a change of today's SQL.

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/db/keyset"
	"github.com/nanostack-dev/nanostack-framework/db/mysql"
	jet "github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/search"
	"github.com/nanostack-dev/nanostack-framework/db/sqlite"
)

var updateGolden = flag.Bool("update", false, "rewrite the golden files of the dialect tests")

type dRow struct {
	ID      string
	Name    string
	Note    *string
	Status  string
	StatusP *string
	Ref     uuid.UUID
	Rank    int64
	Score   *float64
	Ratio   float32
	Created time.Time
	Stamp   time.Time
	At      time.Time
	IP      string
	// The computed keys: aggregates of the widget's requests.
	Requests int64
	Biggest  *int64
	Total    float64
	Label    *string
	Last     time.Time
}

type dField string

const (
	dName    dField = "NAME"
	dNote    dField = "NOTE"
	dNoteNF  dField = "NOTE_NULLS_FIRST"
	dNoteNL  dField = "NOTE_NULLS_LAST"
	dStatus  dField = "STATUS"
	dStatusP dField = "STATUS_NULLABLE"
	dRef     dField = "REF"
	dRank    dField = "RANK"
	dScore   dField = "SCORE"
	dRatio   dField = "RATIO"
	dCreated dField = "CREATED"
	dStamp   dField = "STAMP"
	dAt      dField = "AT"
	dIP      dField = "IP"
	// dCreatedSQL is CREATED as SQL writes it on SQLite (CURRENT_TIMESTAMP).
	dCreatedSQL dField = "CREATED_SQL_TEXT"
	// dStampMaria is STAMP on MariaDB 11.5 and later.
	dStampMaria dField = "STAMP_MARIADB"
	// The computed keys.
	dRequests dField = "REQUEST_COUNT"
	dBiggest  dField = "BIGGEST"
	dTotal    dField = "TOTAL"
	dLabel    dField = "LABEL"
	dLast     dField = "LAST_RECEIVED"
)

var dLabels = []string{"pending", "active", "archived"}

// dTable is the same table in one dialect package.
type dTable struct {
	dialect string
	source  keyset.Source
	keys    map[dField]keyset.Key[dRow]
	id      keyset.Key[dRow]
}

var dDialects = []string{"postgres", "mysql", "sqlite"}

func newDTable(dialect string) dTable {
	var (
		id      = jet.StringColumn("id")
		name    = jet.StringColumn("name")
		note    = jet.StringColumn("note")
		status  = jet.StringColumn("status")
		statusP = jet.StringColumn("status_p")
		ref     = jet.StringColumn("ref")
		rank    = jet.IntegerColumn("rank")
		score   = jet.FloatColumn("score")
		ratio   = jet.FloatColumn("ratio")
		created = jet.TimestampColumn("created")
		stamp   = jet.TimestampColumn("stamp")
		at      = jet.TimestampzColumn("at")
		ip      = jet.StringColumn("ip")

		reqWidget   = jet.StringColumn("widget_id")
		reqSize     = jet.IntegerColumn("size")
		reqAmount   = jet.FloatColumn("amount")
		reqLabel    = jet.StringColumn("label")
		reqReceived = jet.TimestampzColumn("received")
	)
	// ofWidget is a correlated subquery over the requests of the widget, in the
	// dialect's own builder.
	var ofWidget func(jet.Projection) jet.Expression
	table := dTable{dialect: dialect}
	switch dialect {
	case "postgres":
		table.source = jet.NewTable("public", "widgets", "",
			id, name, note, status, statusP, ref, rank, score, ratio, created, stamp, at, ip)
		requests := jet.NewTable("public", "requests", "", reqWidget, reqSize, reqAmount, reqLabel, reqReceived)
		ofWidget = func(p jet.Projection) jet.Expression { return requests.SELECT(p).WHERE(reqWidget.EQ(id)) }
	case "mysql":
		table.source = mysql.NewTable("db", "widgets", "",
			id, name, note, status, statusP, ref, rank, score, ratio, created, stamp, at, ip)
		requests := mysql.NewTable("db", "requests", "", reqWidget, reqSize, reqAmount, reqLabel, reqReceived)
		ofWidget = func(p jet.Projection) jet.Expression { return requests.SELECT(p).WHERE(reqWidget.EQ(id)) }
	case "sqlite":
		table.source = sqlite.NewTable("", "widgets", "",
			id, name, note, status, statusP, ref, rank, score, ratio, created, stamp, at, ip)
		requests := sqlite.NewTable("", "requests", "", reqWidget, reqSize, reqAmount, reqLabel, reqReceived)
		ofWidget = func(p jet.Projection) jet.Expression { return requests.SELECT(p).WHERE(reqWidget.EQ(id)) }
	}
	table.id = keyset.StringKey(id, func(r dRow) string { return r.ID })
	table.keys = map[dField]keyset.Key[dRow]{
		dName:    keyset.StringKey(name, func(r dRow) string { return r.Name }),
		dNote:    keyset.NullableStringKey(note, func(r dRow) *string { return r.Note }),
		dNoteNF:  keyset.NullableStringKey(note, func(r dRow) *string { return r.Note }).NullsFirst(),
		dNoteNL:  keyset.NullableStringKey(note, func(r dRow) *string { return r.Note }).NullsLast(),
		dStatus:  keyset.EnumKey(status, "widget_status", dLabels, func(r dRow) string { return r.Status }),
		dStatusP: keyset.NullableEnumKey(statusP, "widget_status", dLabels, func(r dRow) *string { return r.StatusP }),
		dRef:     keyset.UUIDKey(ref, func(r dRow) uuid.UUID { return r.Ref }),
		dRank:    keyset.IntKey(rank, func(r dRow) int64 { return r.Rank }),
		dScore:   keyset.NullableFloatKey(score, func(r dRow) *float64 { return r.Score }),
		dRatio:   keyset.RealKey(ratio, func(r dRow) float32 { return r.Ratio }),
		dCreated: keyset.LocalTimeKey(created, func(r dRow) time.Time { return r.Created }),
		dStamp:   keyset.MySQLTimestampKey(stamp, func(r dRow) time.Time { return r.Stamp }),
		dCreatedSQL: keyset.LocalTimeKey(created, func(r dRow) time.Time { return r.Created }).
			SQLiteLayout("2006-01-02 15:04:05"),
		dStampMaria: keyset.MariaDBTimestampKey(stamp, func(r dRow) time.Time { return r.Stamp }),
		dAt:         keyset.TimeKey(at, func(r dRow) time.Time { return r.At }),
		dIP:         keyset.InetKey(ip, func(r dRow) string { return r.IP }),
		dRequests: keyset.IntExprKey("widget_row.request_count", jet.IntExp(ofWidget(jet.COUNT(jet.STAR))),
			func(r dRow) int64 { return r.Requests }),
		dBiggest: keyset.NullableIntExprKey("widget_row.biggest", jet.IntExp(ofWidget(jet.MAXi(reqSize))),
			func(r dRow) *int64 { return r.Biggest }),
		dTotal: keyset.FloatExprKey("widget_row.total", jet.FloatExp(ofWidget(jet.SUMf(reqAmount))),
			func(r dRow) float64 { return r.Total }),
		dLabel: keyset.NullableStringExprKey("widget_row.label", jet.StringExp(ofWidget(jet.MIN(reqLabel))),
			func(r dRow) *string { return r.Label }),
		dLast: keyset.TimeExprKey("widget_row.last_received", jet.TimestampzExp(ofWidget(jet.MAX(reqReceived))),
			func(r dRow) time.Time { return r.Last }),
	}
	return table
}

func (d dTable) order(t *testing.T, sorts ...search.Sort[dField]) keyset.Order[dRow] {
	t.Helper()
	return mustOrder(t, sorts, d.keys, d.id)
}

func (d dTable) page() *keyset.PageBuilder[dRow, dRow] {
	return keyset.Page(nil, func(r dRow) dRow { return r }, jet.StringColumn("id")).From(d.source)
}

func asc(field dField) search.Sort[dField] {
	return search.Sort[dField]{Field: field, Direction: search.SortAscending}
}

func desc(field dField) search.Sort[dField] {
	return search.Sort[dField]{Field: field, Direction: search.SortDescending}
}

var dSample = dRow{
	ID:      "w_7",
	Name:    "apple",
	Note:    new("beta"),
	Status:  "active",
	StatusP: new("active"),
	Ref:     uuid.MustParse("018f6b8e-0000-7000-8000-000000000001"),
	Rank:    42,
	Score:   new(2.25),
	Ratio:   0.1,
	Created: time.Date(2026, time.March, 4, 10, 11, 12, 345_678_000, time.UTC),
	Stamp:   time.Date(2026, time.March, 4, 10, 11, 12, 0, time.UTC),
	At:      time.Date(2026, time.March, 4, 10, 11, 12, 0, time.UTC),
	IP:      "10.0.0.1",

	Requests: 3,
	Biggest:  new(int64(2)),
	Total:    1.25,
	Label:    new("b"),
	Last:     time.Date(2026, time.March, 4, 10, 11, 12, 0, time.UTC),
}

// dCase is one page whose SQL the golden pins on every dialect that runs it.
type dCase struct {
	name  string
	sorts []search.Sort[dField]
	row   dRow
}

func dCases() []dCase {
	nullNote := dSample
	nullNote.Note = nil
	first, last := dSample, dSample
	first.Status, last.Status = "pending", "archived"
	unknown := dSample
	unknown.Status = ""
	nullStatus := dSample
	nullStatus.StatusP = nil
	noRequests := dSample
	noRequests.Requests, noRequests.Biggest, noRequests.Total, noRequests.Label = 0, nil, 0, nil
	return []dCase{
		{name: "created ascending (NOT NULL keys)", sorts: []search.Sort[dField]{asc(dCreated)}, row: dSample},
		{name: "created descending (NOT NULL keys)", sorts: []search.Sort[dField]{desc(dCreated)}, row: dSample},
		{name: "rank, then name descending", sorts: []search.Sort[dField]{asc(dRank), desc(dName)}, row: dSample},
		{name: "nullable note ascending", sorts: []search.Sort[dField]{asc(dNote)}, row: dSample},
		{name: "nullable note ascending after a NULL", sorts: []search.Sort[dField]{asc(dNote)}, row: nullNote},
		{name: "nullable note descending", sorts: []search.Sort[dField]{desc(dNote)}, row: dSample},
		{name: "nullable note descending after a NULL", sorts: []search.Sort[dField]{desc(dNote)}, row: nullNote},
		{name: "nullable note ascending, NULLs first", sorts: []search.Sort[dField]{asc(dNoteNF)}, row: dSample},
		{name: "nullable note descending, NULLs last", sorts: []search.Sort[dField]{desc(dNoteNL)}, row: dSample},
		{name: "enum ascending after a middle label", sorts: []search.Sort[dField]{asc(dStatus)}, row: dSample},
		{name: "enum ascending after the last label", sorts: []search.Sort[dField]{asc(dStatus)}, row: last},
		{name: "enum descending after the first label", sorts: []search.Sort[dField]{desc(dStatus)}, row: first},
		{name: "nullable enum ascending", sorts: []search.Sort[dField]{asc(dStatusP)}, row: dSample},
		{name: "nullable enum ascending after a NULL", sorts: []search.Sort[dField]{asc(dStatusP)}, row: nullStatus},
		{name: "uuid ascending", sorts: []search.Sort[dField]{asc(dRef)}, row: dSample},
		{name: "nullable float ascending", sorts: []search.Sort[dField]{asc(dScore)}, row: dSample},
		{name: "real ascending", sorts: []search.Sort[dField]{asc(dRatio)}, row: dSample},
		{name: "MySQL timestamp ascending", sorts: []search.Sort[dField]{asc(dStamp)}, row: dSample},
		{name: "timestamptz ascending", sorts: []search.Sort[dField]{asc(dAt)}, row: dSample},
		{name: "inet ascending", sorts: []search.Sort[dField]{asc(dIP)}, row: dSample},
		// MySQL's value for an invalid ENUM label: it sorts before every label.
		{name: "enum ascending after MySQL's empty value", sorts: []search.Sort[dField]{asc(dStatus)}, row: unknown},
		{name: "computed count descending", sorts: []search.Sort[dField]{desc(dRequests)}, row: dSample},
		{name: "computed nullable max ascending", sorts: []search.Sort[dField]{asc(dBiggest)}, row: dSample},
		{
			name: "computed nullable max ascending after a NULL", sorts: []search.Sort[dField]{asc(dBiggest)},
			row: noRequests,
		},
		{
			name: "computed nullable max descending, then rank", sorts: []search.Sort[dField]{desc(dBiggest), asc(dRank)},
			row: dSample,
		},
		{name: "computed float, then name", sorts: []search.Sort[dField]{asc(dTotal), asc(dName)}, row: dSample},
		{name: "computed nullable text descending", sorts: []search.Sort[dField]{desc(dLabel)}, row: dSample},
		{name: "computed timestamptz ascending", sorts: []search.Sort[dField]{asc(dLast)}, row: dSample},
	}
}

func TestDialectSQL(t *testing.T) {
	var golden strings.Builder
	for _, tc := range dCases() {
		fmt.Fprintf(&golden, "## %s\n", tc.name)
		for _, dialect := range dDialects {
			table := newDTable(dialect)
			order := table.order(t, tc.sorts...)
			cursor, err := order.Cursor(tc.row)
			require.NoError(t, err)
			_, rows, err := table.page().Keyset(order).StatementsForTest(search.Pagination{Limit: 10, Cursor: cursor})
			switch {
			case errors.Is(err, keyset.ErrDialect), errors.Is(err, search.ErrInvalidCursor):
				fmt.Fprintf(&golden, "%-8s %s\n", dialect, errorName(err))
			case err != nil:
				t.Fatalf("%s on %s: %v", tc.name, dialect, err)
			default:
				fmt.Fprintf(&golden, "%-8s %s\n", dialect, normalize(rows.DebugSql()))
			}
		}
		golden.WriteString("\n")
	}
	compareGolden(t, "dialects.golden", golden.String())
}

// The properties that the golden file shows, asserted on their own so a
// careless -update cannot lose them.
func TestDialectSQLProperties(t *testing.T) {
	sqlOf := func(t *testing.T, dialect string, row dRow, sorts ...search.Sort[dField]) string {
		t.Helper()
		table := newDTable(dialect)
		order := table.order(t, sorts...)
		cursor, err := order.Cursor(row)
		require.NoError(t, err)
		_, rows, err := table.page().Keyset(order).StatementsForTest(search.Pagination{Limit: 10, Cursor: cursor})
		require.NoError(t, err)
		return normalize(rows.DebugSql())
	}

	t.Run("MySQL orders NOT NULL keys plainly and seeks with the expanded form", func(t *testing.T) {
		sql := sqlOf(t, "mysql", dSample, asc(dCreated))
		require.Contains(t, sql, "ORDER BY widgets.created ASC, widgets.id ASC LIMIT 11")
		require.NotContains(t, sql, "IS NULL", "a NOT NULL key has no NULL to place")
		require.NotContains(t, sql, "(widgets.created, widgets.id)", "MySQL gets no row value")
		require.Contains(t, sql, "widgets.created > '2026-03-04 10:11:12.345678'")
	})

	t.Run("MySQL casts an integer literal to SIGNED", func(t *testing.T) {
		require.Contains(t, sqlOf(t, "mysql", dSample, asc(dRank)), "widgets.`rank` > CAST(42 AS SIGNED)")
	})

	t.Run("MySQL places NULLs of a nullable key by the contract", func(t *testing.T) {
		require.Contains(t, sqlOf(t, "mysql", dSample, asc(dNote)), "ORDER BY widgets.note IS NULL, widgets.note ASC")
		require.Contains(t, sqlOf(t, "mysql", dSample, desc(dNote)),
			"ORDER BY widgets.note IS NOT NULL, widgets.note DESC")
		require.Contains(t, sqlOf(t, "mysql", dSample, asc(dNoteNF)), "ORDER BY widgets.note ASC,",
			"NULLs first when ascending is MySQL's own default")
	})

	t.Run("MySQL orders an enum natively and seeks with an IN list", func(t *testing.T) {
		sql := sqlOf(t, "mysql", dSample, asc(dStatus))
		require.Contains(t, sql, "ORDER BY widgets.status ASC")
		require.Contains(t, sql, "widgets.status IN ('archived')")
		require.NotContains(t, sql, "CAST(", "MySQL has no enum type to cast to")
	})

	t.Run("SQLite seeks NOT NULL keys with a row value", func(t *testing.T) {
		sql := sqlOf(t, "sqlite", dSample, asc(dCreated))
		require.Contains(t, sql, "(widgets.created, widgets.id) > ('2026-03-04 10:11:12.345678+00:00', 'w_7')")
		require.Contains(t, sql, "ORDER BY widgets.created ASC, widgets.id ASC LIMIT 11")
	})

	t.Run("SQLite names the contract placement of a nullable key", func(t *testing.T) {
		require.Contains(t, sqlOf(t, "sqlite", dSample, asc(dNote)), "ORDER BY widgets.note ASC NULLS LAST")
		require.Contains(t, sqlOf(t, "sqlite", dSample, desc(dNote)), "ORDER BY widgets.note DESC NULLS FIRST")
	})

	t.Run("SQLite orders an enum by a CASE and seeks with the expanded form", func(t *testing.T) {
		sql := sqlOf(t, "sqlite", dSample, asc(dStatus))
		require.Contains(t, sql, "ORDER BY (CASE WHEN CAST(widgets.status AS BLOB) = CAST('pending' AS BLOB) THEN 0")
		require.Contains(t, sql, "CAST(widgets.status AS BLOB) IN (CAST('archived' AS BLOB))")
		require.Contains(t, sql, "CAST(widgets.status AS BLOB) = CAST('active' AS BLOB)")
		require.NotContains(t, sql, "(widgets.status, widgets.id)", "a row value compares the enum as text")
		require.NotContains(t, sql, "widgets.status IN", "the column's collation must not match a label")
	})

	t.Run("MySQL seeks after the zero date without a zero-date literal", func(t *testing.T) {
		zero := dSample
		zero.Created, zero.Stamp = time.Time{}, time.Time{}
		sql := sqlOf(t, "mysql", zero, asc(dCreated))
		require.Contains(t, sql, "(widgets.created >= '1000-01-01 00:00:00') OR "+
			"((widgets.created < '1000-01-01 00:00:00') AND (widgets.id > 'w_7'))")
		require.Contains(t, sqlOf(t, "mysql", zero, asc(dStamp)), "widgets.stamp >= '1970-01-01 00:00:01'")
		require.NotContains(t, sql, "0000-00-00", "the seek does not depend on how sql_mode reads a zero-date literal")
	})

	t.Run("SQLite writes a time finer than a microsecond in full", func(t *testing.T) {
		fine := dSample
		fine.Created = time.Date(2026, time.March, 4, 10, 11, 12, 123_456_789, time.UTC)
		require.Contains(t, sqlOf(t, "sqlite", fine, asc(dCreated)), "'2026-03-04 10:11:12.123456789+00:00'")
	})

	t.Run("SQLite writes a time in the key's layout", func(t *testing.T) {
		whole := dSample
		whole.Created = time.Date(2026, time.March, 4, 10, 11, 12, 0, time.UTC)
		require.Contains(t, sqlOf(t, "sqlite", whole, asc(dCreatedSQL)),
			"(widgets.created, widgets.id) > ('2026-03-04 10:11:12', 'w_7')")
	})

	t.Run("a uuid is lower-case text on MySQL and SQLite", func(t *testing.T) {
		for _, dialect := range []string{"mysql", "sqlite"} {
			require.Contains(t, sqlOf(t, dialect, dSample, asc(dRef)), "'018f6b8e-0000-7000-8000-000000000001'")
		}
	})

	t.Run("PostgreSQL keeps today's SQL", func(t *testing.T) {
		sql := sqlOf(t, "postgres", dSample, asc(dNote))
		require.Contains(t, sql, "ORDER BY widgets.note ASC, widgets.id ASC")
		require.NotContains(t, sql, "NULLS")
	})
}

// A cursor whose every value is a trailing ascending NULL selects nothing.
// Only a nullable tiebreak reaches it, and each dialect must parse the FALSE.
func TestDialectSeekAfterAnAllNullCursorSelectsNothing(t *testing.T) {
	for dialect, want := range map[string]string{
		"postgres": "WHERE FALSE::boolean",
		"mysql":    "WHERE FALSE ORDER BY",
		"sqlite":   "WHERE FALSE ORDER BY",
	} {
		t.Run(dialect, func(t *testing.T) {
			table := newDTable(dialect)
			order := mustOrder[dField](t, nil, nil, table.keys[dNote])
			cursor, err := order.Cursor(dRow{})
			require.NoError(t, err)
			_, rows, err := table.page().Keyset(order).StatementsForTest(search.Pagination{Limit: 1, Cursor: cursor})
			require.NoError(t, err)
			require.Contains(t, normalize(rows.DebugSql()), want)
		})
	}
}

func TestDialectRefusesAKeyItsColumnTypeLacks(t *testing.T) {
	for _, tc := range []struct {
		dialect string
		field   dField
	}{
		{"mysql", dAt}, {"sqlite", dAt},
		{"mysql", dIP}, {"sqlite", dIP},
		{"postgres", dStamp}, {"sqlite", dStamp},
		{"postgres", dStampMaria}, {"sqlite", dStampMaria},
		{"sqlite", dRatio},
	} {
		t.Run(tc.dialect+" "+string(tc.field), func(t *testing.T) {
			table := newDTable(tc.dialect)
			order := table.order(t, asc(tc.field))
			// db is nil: an error proves the page was refused before a query.
			_, err := table.page().Keyset(order).Run(t.Context(), search.Pagination{Limit: 1})
			require.ErrorIs(t, err, keyset.ErrDialect)
			require.False(t, search.IsClientError(err), "a key on the wrong dialect is a programming error")
		})
	}
}

func TestDialectReadsAMySQLAndASQLiteJoin(t *testing.T) {
	left := mysql.NewTable("db", "a", "", mysql.StringColumn("id"))
	right := mysql.NewTable("db", "b", "", mysql.StringColumn("id"))
	sLeft := sqlite.NewTable("", "a", "", sqlite.StringColumn("id"))
	sRight := sqlite.NewTable("", "b", "", sqlite.StringColumn("id"))
	for name, source := range map[string]keyset.Source{
		"mysql":  left.INNER_JOIN(right, mysql.Bool(true)),
		"sqlite": sLeft.INNER_JOIN(sRight, sqlite.Bool(true)),
	} {
		t.Run(name, func(t *testing.T) {
			count, _, err := keyset.Page(nil, func(r dRow) dRow { return r }, jet.StringColumn("id")).
				From(source).StatementsForTest(search.Pagination{Limit: 1})
			require.NoError(t, err)
			require.Contains(t, normalize(count.DebugSql()), "INNER JOIN")
		})
	}
}

func errorName(err error) string {
	switch {
	case errors.Is(err, keyset.ErrDialect):
		return "ErrDialect"
	case errors.Is(err, search.ErrInvalidCursor):
		return "ErrInvalidCursor"
	}
	return err.Error()
}

func compareGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *updateGolden {
		require.NoError(t, os.WriteFile(path, []byte(got), 0o600))
		return
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "run with -update to write %s", path)
	require.Equal(t, string(want), got, "the SQL or a codec changed: review it, then run with -update")
}

// A SQLite time finer than a microsecond keeps its nanoseconds in the cursor,
// so the seek starts exactly after the row. A whole microsecond keeps the
// single-number token.
func TestDialectTimeCursorKeepsNanosecondsForSQLite(t *testing.T) {
	table := newDTable("sqlite")
	order := table.order(t, asc(dCreated))
	fine := dSample
	fine.Created = time.Date(2026, time.March, 4, 10, 11, 12, 123_456_789, time.UTC)
	token, err := order.Cursor(fine)
	require.NoError(t, err)
	decoded, err := search.DecodeCursor(token)
	require.NoError(t, err)
	require.JSONEq(t, "[825934272123456,789]", string(decoded.Values[0]))
	token, err = order.Cursor(dSample)
	require.NoError(t, err)
	decoded, err = search.DecodeCursor(token)
	require.NoError(t, err)
	require.JSONEq(t, "825934272345678", string(decoded.Values[0]))
}

func TestSQLiteLayoutPanicsOffALocalTimeKeyAndOnALayoutOutOfOrder(t *testing.T) {
	table := newDTable("sqlite")
	require.Panics(t, func() { table.keys[dName].SQLiteLayout("2006-01-02 15:04:05") })
	require.Panics(t, func() { table.keys[dAt].SQLiteLayout("2006-01-02 15:04:05") })
	require.Panics(t, func() { table.keys[dCreated].SQLiteLayout("02/01/2006 15:04:05") })
	require.NotPanics(t, func() { table.keys[dCreated].SQLiteLayout("2006-01-02T15:04:05.000Z") })
}

// SQLiteLayout keeps every other setting of the key.
func TestSQLiteLayoutKeepsTheNullPlacement(t *testing.T) {
	seen := jet.TimestampColumn("seen")
	key := keyset.NullableLocalTimeKey(seen, func(dRow) *time.Time { return nil }).NullsLast().
		SQLiteLayout("2006-01-02 15:04:05")
	table := newDTable("sqlite")
	order := mustOrder[dField](t, nil, nil, key)
	_, rows, err := table.page().Keyset(order).StatementsForTest(search.Pagination{Limit: 1})
	require.NoError(t, err)
	require.Contains(t, normalize(rows.DebugSql()), "ORDER BY seen ASC NULLS LAST LIMIT 2")
}
