package transactor_test

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	jet "github.com/go-jet/jet/v2/postgres"

	"github.com/nanostack-dev/nanostack-framework/pkg/db/transactor"
	"github.com/nanostack-dev/nanostack-framework/pkg/search"
)

type keyRow struct {
	ID      string
	Name    string
	Created time.Time
	Note    *string
}

var (
	colID   = jet.StringColumn("id")
	colNote = jet.StringColumn("note")
	_       = jet.NewTable("public", "widgets", "", colID, colNote)
)

func keyColumns() map[sortField]transactor.Key[keyRow] {
	return map[sortField]transactor.Key[keyRow]{
		sortByName:    transactor.StringKey(colName, func(r keyRow) string { return r.Name }),
		sortByCreated: transactor.TimeKey(colCreated, func(r keyRow) time.Time { return r.Created }),
		sortByNote:    transactor.NullableStringKey(colNote, func(r keyRow) *string { return r.Note }),
	}
}

func idKey() transactor.Key[keyRow] {
	return transactor.StringKey(colID, func(r keyRow) string { return r.ID })
}

const sortByNote sortField = "NOTE"

func TestKeysSignature(t *testing.T) {
	for _, tc := range []struct {
		name  string
		sorts []search.Sort[sortField]
		want  string
	}{
		{name: "no sort orders by the tiebreak ascending", want: "widgets.id:ASC"},
		{
			name:  "the tiebreak follows the last direction",
			sorts: []search.Sort[sortField]{{Field: sortByCreated, Direction: search.SortDescending}},
			want:  "widgets.created_at:DESC,widgets.id:DESC",
		},
		{
			name: "unmapped and repeated fields are dropped",
			sorts: []search.Sort[sortField]{
				{Field: sortUnmapped, Direction: search.SortAscending},
				{Field: sortByName, Direction: search.SortAscending},
				{Field: sortByName, Direction: search.SortDescending},
			},
			want: "widgets.name:ASC,widgets.id:ASC",
		},
		{
			name:  "an unknown direction sorts ascending",
			sorts: []search.Sort[sortField]{{Field: sortByName, Direction: "sideways"}},
			want:  "widgets.name:ASC,widgets.id:ASC",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := transactor.Keys(tc.sorts, keyColumns(), idKey()).Signature(); got != tc.want {
				t.Fatalf("Signature() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestKeysTiebreakNotRepeated(t *testing.T) {
	columns := keyColumns()
	columns["ID"] = idKey()
	order := transactor.Keys([]search.Sort[sortField]{{Field: "ID", Direction: search.SortDescending}}, columns, idKey())
	if got := order.Signature(); got != "widgets.id:DESC" {
		t.Fatalf("Signature() = %q", got)
	}
}

func cursorFor(t *testing.T, order transactor.Order[keyRow], row keyRow) string {
	t.Helper()
	token, err := order.Cursor(row)
	if err != nil {
		t.Fatalf("Cursor() error = %v", err)
	}
	return token
}

func TestSeekUsesRowComparisonForUniformNotNullKeys(t *testing.T) {
	created := time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)
	order := transactor.Keys([]search.Sort[sortField]{{Field: sortByCreated, Direction: search.SortDescending}}, keyColumns(), idKey())
	seek, err := order.Seek(cursorFor(t, order, keyRow{ID: "w_9", Created: created}))
	if err != nil {
		t.Fatalf("Seek() error = %v", err)
	}
	sql := normalize(jet.SELECT(colID).FROM(testTable).WHERE(seek).DebugSql())
	if !strings.Contains(sql, "ROW(widgets.created_at, widgets.id) < ROW('2026-10-15 10:00:00Z'::timestamp with time zone, 'w_9'::text)") {
		t.Fatalf("seek SQL = %q, want a row comparison", sql)
	}
}

func TestSeekExpandsMixedDirectionsAndNulls(t *testing.T) {
	sorts := []search.Sort[sortField]{
		{Field: sortByNote, Direction: search.SortAscending},
		{Field: sortByName, Direction: search.SortDescending},
	}
	order := transactor.Keys(sorts, keyColumns(), idKey())

	note := "n"
	seek, err := order.Seek(cursorFor(t, order, keyRow{ID: "w_1", Name: "b", Note: &note}))
	if err != nil {
		t.Fatalf("Seek() error = %v", err)
	}
	sql := normalize(jet.SELECT(colID).FROM(testTable).WHERE(seek).DebugSql())
	for _, want := range []string{
		"((widgets.note > 'n'::text) OR (widgets.note IS NULL))",
		"((widgets.note = 'n'::text) AND (widgets.name < 'b'::text))",
		"(widgets.name = 'b'::text)) AND (widgets.id < 'w_1'::text)",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("seek SQL = %q, want it to contain %q", sql, want)
		}
	}

	seek, err = order.Seek(cursorFor(t, order, keyRow{ID: "w_1", Name: "b"}))
	if err != nil {
		t.Fatalf("Seek(null) error = %v", err)
	}
	sql = normalize(jet.SELECT(colID).FROM(testTable).WHERE(seek).DebugSql())
	if strings.Contains(sql, "widgets.note >") || !strings.Contains(sql, "widgets.note IS NULL") {
		t.Fatalf("seek SQL after a NULL = %q, want nothing after NULL in ascending order", sql)
	}
}

func TestSeekRejectsForeignCursors(t *testing.T) {
	byName := transactor.Keys([]search.Sort[sortField]{{Field: sortByName, Direction: search.SortAscending}}, keyColumns(), idKey())
	byCreated := transactor.Keys([]search.Sort[sortField]{{Field: sortByCreated, Direction: search.SortAscending}}, keyColumns(), idKey())
	token := cursorFor(t, byName, keyRow{ID: "w_1", Name: "a"})

	if _, err := byCreated.Seek(token); !errors.Is(err, search.ErrInvalidCursor) {
		t.Fatalf("Seek() with another sort's cursor = %v, want ErrInvalidCursor", err)
	}
	if _, err := byName.Seek("garbage"); !errors.Is(err, search.ErrInvalidCursor) {
		t.Fatalf("Seek(garbage) = %v, want ErrInvalidCursor", err)
	}
	forged, _ := search.EncodeCursor(byName.Signature(), []any{"a", nil})
	if _, err := byName.Seek(forged); !errors.Is(err, search.ErrInvalidCursor) {
		t.Fatalf("Seek() with NULL for a NOT NULL key = %v, want ErrInvalidCursor", err)
	}
	for _, forged := range []any{"2026-01-01T00:00:00+16:00", 1.5, int64(-300_000_000_000_000_000)} {
		token, _ := search.EncodeCursor(byCreated.Signature(), []any{forged, "w_1"})
		if _, err := byCreated.Seek(token); !errors.Is(err, search.ErrInvalidCursor) {
			t.Fatalf("Seek() with timestamp %v = %v, want ErrInvalidCursor", forged, err)
		}
	}
	nul, _ := search.EncodeCursor(byName.Signature(), []any{"a\u0000b", "w_1"})
	if _, err := byName.Seek(nul); !errors.Is(err, search.ErrInvalidCursor) {
		t.Fatalf("Seek() with a NUL in text = %v, want ErrInvalidCursor", err)
	}
	if seek, err := byName.Seek(""); seek != nil || err != nil {
		t.Fatalf("Seek(\"\") = %v, %v, want no predicate", seek, err)
	}
}

func TestTrim(t *testing.T) {
	order := transactor.Keys[sortField](nil, keyColumns(), idKey())
	rows := []keyRow{{ID: "a"}, {ID: "b"}, {ID: "c"}}

	page, next, err := order.Trim(rows, 2)
	if err != nil || len(page) != 2 || next == "" {
		t.Fatalf("Trim(3 rows, 2) = %d rows, %q, %v", len(page), next, err)
	}
	if want := cursorFor(t, order, rows[1]); next != want {
		t.Fatalf("next cursor points at %q, want the last kept row", next)
	}
	if page, next, _ := order.Trim(rows[:2], 2); len(page) != 2 || next != "" {
		t.Fatalf("Trim(2 rows, 2) = %d rows, %q, want the last page", len(page), next)
	}
}

func keysetBuilder() *transactor.PageBuilder[keyRow, keyRow] {
	return transactor.Page(nil, func(r keyRow) keyRow { return r }, colID, colName).From(testTable)
}

func TestKeysetStatement(t *testing.T) {
	order := transactor.Keys([]search.Sort[sortField]{{Field: sortByName, Direction: search.SortAscending}}, keyColumns(), idKey())
	where := colTenant.EQ(jet.String("t1"))
	seek, err := order.Seek(cursorFor(t, order, keyRow{ID: "w_1", Name: "a"}))
	if err != nil {
		t.Fatal(err)
	}

	sql := normalize(keysetBuilder().Where(where).Keyset(order).
		KeysetStatementForTest(search.Pagination{Limit: 10}, seek).DebugSql())
	for _, want := range []string{
		"widgets.tenant_id = 't1'",
		"ROW(widgets.name, widgets.id) > ROW('a'::text, 'w_1'::text)",
		"ORDER BY widgets.name ASC, widgets.id ASC",
		"LIMIT 11",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("keyset SQL = %q, want %q", sql, want)
		}
	}
	if strings.Contains(sql, "OFFSET") {
		t.Fatalf("keyset SQL = %q, a cursor page has no offset", sql)
	}

	sql = normalize(keysetBuilder().Keyset(order).
		KeysetStatementForTest(search.Pagination{Limit: 10, Offset: 30}, nil).DebugSql())
	if !strings.Contains(sql, "LIMIT 11") || !strings.Contains(sql, "OFFSET 30") {
		t.Fatalf("offset keyset SQL = %q, want LIMIT 11 OFFSET 30", sql)
	}
}

func TestRunRejectsBadPaginationBeforeQuerying(t *testing.T) {
	order := transactor.Keys[sortField](nil, keyColumns(), idKey())
	for name, tc := range map[string]struct {
		builder *transactor.PageBuilder[keyRow, keyRow]
		page    search.Pagination
		want    error
	}{
		"cursor with offset":    {keysetBuilder().Keyset(order), search.Pagination{Limit: 1, Offset: 1, Cursor: "x"}, search.ErrCursorWithOffset},
		"cursor without keyset": {keysetBuilder(), search.Pagination{Limit: 1, Cursor: "x"}, search.ErrInvalidCursor},
		"undecodable cursor":    {keysetBuilder().Keyset(order), search.Pagination{Limit: 1, Cursor: "x"}, search.ErrInvalidCursor},
		"keyset and order by":   {keysetBuilder().Keyset(order).OrderBy(colName.ASC()), search.Pagination{Limit: 1}, transactor.ErrPageOrderConflict},
	} {
		t.Run(name, func(t *testing.T) {
			// db is nil: reaching a query would panic, so an error proves the
			// request was refused up front.
			if _, err := tc.builder.Run(t.Context(), tc.page).Value(); !errors.Is(err, tc.want) {
				t.Fatalf("Run() error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestKeysCompositeTiebreak(t *testing.T) {
	order := transactor.Keys([]search.Sort[sortField]{{Field: sortByName, Direction: search.SortDescending}},
		keyColumns(), transactor.StringKey(colTenant, func(r keyRow) string { return "" }), idKey())
	if got := order.Signature(); got != "widgets.name:DESC,widgets.tenant_id:DESC,widgets.id:DESC" {
		t.Fatalf("Signature() = %q", got)
	}
}

type typedRow struct {
	ID     string
	Status string
	Ref    uuid.UUID
	Score  float64
}

var (
	colStatus = jet.StringColumn("status")
	colRef    = jet.StringColumn("ref")
	colScore  = jet.FloatColumn("score")
	_         = jet.NewTable("public", "widgets", "", colStatus, colRef, colScore)
)

const (
	sortByStatus sortField = "STATUS"
	sortByRef    sortField = "REF"
	sortByScore  sortField = "SCORE"
)

func typedOrder(field sortField) transactor.Order[typedRow] {
	return transactor.Keys([]search.Sort[sortField]{{Field: field, Direction: search.SortAscending}},
		map[sortField]transactor.Key[typedRow]{
			sortByStatus: transactor.EnumKey(colStatus, "widget_status", []string{"on", "off"}, func(r typedRow) string { return r.Status }),
			sortByRef:    transactor.UUIDKey(colRef, func(r typedRow) uuid.UUID { return r.Ref }),
			sortByScore:  transactor.FloatKey(colScore, func(r typedRow) float64 { return r.Score }),
		},
		transactor.StringKey(colID, func(r typedRow) string { return r.ID }))
}

// Forged values fail as ErrInvalidCursor in Seek, before a cast could fail
// in the database and surface as a 500.
func TestSeekRejectsValuesTheColumnCannotHold(t *testing.T) {
	for name, tc := range map[string]struct {
		field sortField
		value any
	}{
		"unknown enum label": {sortByStatus, "maybe"},
		"malformed uuid":     {sortByRef, "not-a-uuid"},
		"unknown float word": {sortByScore, "Lots"},
	} {
		t.Run(name, func(t *testing.T) {
			order := typedOrder(tc.field)
			forged, err := search.EncodeCursor(order.Signature(), []any{tc.value, "w_1"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := order.Seek(forged); !errors.Is(err, search.ErrInvalidCursor) {
				t.Fatalf("Seek() = %v, want ErrInvalidCursor", err)
			}
		})
	}
}

func TestTypedKeysRoundTrip(t *testing.T) {
	ref := uuid.MustParse("f47ac10b-58cc-4372-a567-0e02b2c3d479")
	for name, tc := range map[string]struct {
		field sortField
		row   typedRow
		want  string
	}{
		"enum casts to its type": {sortByStatus, typedRow{ID: "w", Status: "off"}, "'off'::text::widget_status"},
		"uuid casts to uuid":     {sortByRef, typedRow{ID: "w", Ref: ref}, "'f47ac10b-58cc-4372-a567-0e02b2c3d479'"},
		"NaN survives":           {sortByScore, typedRow{ID: "w", Score: math.NaN()}, "NaN"},
		"infinity survives":      {sortByScore, typedRow{ID: "w", Score: math.Inf(-1)}, "Inf"},
	} {
		t.Run(name, func(t *testing.T) {
			order := typedOrder(tc.field)
			token, err := order.Cursor(tc.row)
			if err != nil {
				t.Fatalf("Cursor() error = %v", err)
			}
			seek, err := order.Seek(token)
			if err != nil {
				t.Fatalf("Seek() error = %v", err)
			}
			if sql := jet.SELECT(colID).FROM(testTable).WHERE(seek).DebugSql(); !strings.Contains(sql, tc.want) {
				t.Fatalf("seek SQL = %q, want %q", sql, tc.want)
			}
		})
	}
}

func TestTimeKeyEncodesUTC(t *testing.T) {
	paris := time.FixedZone("LMT", 561)
	order := transactor.Keys([]search.Sort[sortField]{{Field: sortByCreated, Direction: search.SortAscending}}, keyColumns(), idKey())
	token, err := order.Cursor(keyRow{ID: "w", Created: time.Date(1900, 1, 1, 0, 9, 21, 0, paris)})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := search.DecodeCursor(token)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(decoded.Values[0]); got != "-3155673600000000" {
		t.Fatalf("encoded instant = %s, want 1900-01-01T00:00:00Z in microseconds since 2000", got)
	}
	// Years past 9999 are valid Postgres timestamps; RFC 3339 cannot write them.
	far := time.Date(12000, 1, 1, 0, 0, 0, 0, time.UTC)
	token, err = order.Cursor(keyRow{ID: "w", Created: far})
	if err != nil {
		t.Fatalf("Cursor() for year 12000: %v", err)
	}
	if _, err := order.Seek(token); err != nil {
		t.Fatalf("Seek() for year 12000: %v", err)
	}
}

// An integer literal is cast to bigint, so a value past the column's width
// compares instead of failing to bind.
func TestIntKeyCastsLiteralToBigint(t *testing.T) {
	colRank := jet.IntegerColumn("rank")
	_ = jet.NewTable("public", "widgets", "", colRank)
	order := transactor.Keys([]search.Sort[sortField]{{Field: "RANK", Direction: search.SortAscending}},
		map[sortField]transactor.Key[keyRow]{"RANK": transactor.IntKey(colRank, func(keyRow) int64 { return 0 })}, idKey())
	token, _ := search.EncodeCursor(order.Signature(), []any{int64(1) << 40, "w_1"})
	seek, err := order.Seek(token)
	if err != nil {
		t.Fatal(err)
	}
	if sql := jet.SELECT(colID).FROM(testTable).WHERE(seek).DebugSql(); !strings.Contains(sql, "::bigint") {
		t.Fatalf("seek SQL = %q, want the literal cast to bigint", sql)
	}
}

// A column read from a subquery is a different reference from the base
// column, so it must not swallow the tiebreak.
func TestKeysDistinguishSubqueryColumns(t *testing.T) {
	sub := testTable.SELECT(colID, colName).AsTable("latest")
	subID := colID.From(sub)
	order := transactor.Keys([]search.Sort[sortField]{{Field: "SUB", Direction: search.SortAscending}},
		map[sortField]transactor.Key[keyRow]{"SUB": transactor.StringKey(subID, func(r keyRow) string { return r.Name })}, idKey())
	if got := order.Signature(); !strings.Contains(got, "latest.") || !strings.Contains(got, ",widgets.id:ASC") {
		t.Fatalf("Signature() = %q, want the subquery column and the tiebreak", got)
	}
}

func TestNullPlacementInOrderAndSignature(t *testing.T) {
	columns := keyColumns()
	columns[sortByNote] = columns[sortByNote].NullsLast()
	order := transactor.Keys([]search.Sort[sortField]{{Field: sortByNote, Direction: search.SortDescending}}, columns, idKey())
	if got := order.Signature(); got != "widgets.note:DESC:NULLS_LAST,widgets.id:DESC" {
		t.Fatalf("Signature() = %q", got)
	}
	sql := normalize(jet.SELECT(colID).FROM(testTable).ORDER_BY(order.OrderBy()...).DebugSql())
	if !strings.Contains(sql, "ORDER BY widgets.note DESC NULLS LAST, widgets.id DESC") {
		t.Fatalf("order SQL = %q", sql)
	}
	note := "n"
	seek, err := order.Seek(cursorFor(t, order, keyRow{ID: "w_1", Note: &note}))
	if err != nil {
		t.Fatal(err)
	}
	if sql := normalize(jet.SELECT(colID).FROM(testTable).WHERE(seek).DebugSql()); !strings.Contains(sql, "(widgets.note < 'n'::text) OR (widgets.note IS NULL)") {
		t.Fatalf("seek SQL = %q, want NULLs after every value when descending NULLS LAST", sql)
	}
}
