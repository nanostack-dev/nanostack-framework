package keyset_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/db/keyset"
	jet "github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/search"
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

func keyColumns() map[sortField]keyset.Key[keyRow] {
	//nolint:exhaustive // a test lists only the sorts it orders by
	return map[sortField]keyset.Key[keyRow]{
		sortByName:    keyset.StringKey(colName, func(r keyRow) string { return r.Name }),
		sortByCreated: keyset.TimeKey(colCreated, func(r keyRow) time.Time { return r.Created }),
		sortByNote:    keyset.NullableStringKey(colNote, func(r keyRow) *string { return r.Note }),
	}
}

func idKey() keyset.Key[keyRow] {
	return keyset.StringKey(colID, func(r keyRow) string { return r.ID })
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
			name: "a repeated field keeps its first position",
			sorts: []search.Sort[sortField]{
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
			if got := mustOrder(t, tc.sorts, keyColumns(), idKey()).Signature(); got != tc.want {
				t.Fatalf("Signature() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestKeysTiebreakNotRepeated(t *testing.T) {
	columns := keyColumns()
	columns["ID"] = idKey()
	order := mustOrder(t,
		[]search.Sort[sortField]{{Field: "ID", Direction: search.SortDescending}},
		columns,
		idKey(),
	)
	if got := order.Signature(); got != "widgets.id:DESC" {
		t.Fatalf("Signature() = %q", got)
	}
}

func mustOrder[S ~string, T any](
	t *testing.T, sorts []search.Sort[S], keys map[S]keyset.Key[T], tiebreak keyset.Key[T], more ...keyset.Key[T],
) keyset.Order[T] {
	t.Helper()
	order, err := keyset.OrderOf(sorts, keys, tiebreak, more...)
	require.NoError(t, err)
	return order
}

func cursorFor(t *testing.T, order keyset.Order[keyRow], row keyRow) string {
	t.Helper()
	token, err := order.Cursor(row)
	if err != nil {
		t.Fatalf("Cursor() error = %v", err)
	}
	return token
}

func TestSeekUsesRowComparisonForUniformNotNullKeys(t *testing.T) {
	created := time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)
	order := mustOrder(t,
		[]search.Sort[sortField]{{Field: sortByCreated, Direction: search.SortDescending}},
		keyColumns(),
		idKey(),
	)
	seek, err := order.Seek(cursorFor(t, order, keyRow{ID: "w_9", Created: created}))
	if err != nil {
		t.Fatalf("Seek() error = %v", err)
	}
	sql := normalize(jet.SELECT(colID).FROM(testTable).WHERE(seek).DebugSql())
	if !strings.Contains(
		sql,
		"ROW(widgets.created_at, widgets.id) < ROW('2026-10-15 10:00:00Z'::timestamp with time zone, 'w_9'::text)",
	) {
		t.Fatalf("seek SQL = %q, want a row comparison", sql)
	}
}

func TestSeekExpandsMixedDirectionsAndNulls(t *testing.T) {
	sorts := []search.Sort[sortField]{
		{Field: sortByNote, Direction: search.SortAscending},
		{Field: sortByName, Direction: search.SortDescending},
	}
	order := mustOrder(t, sorts, keyColumns(), idKey())

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
	byName := mustOrder(t,
		[]search.Sort[sortField]{{Field: sortByName, Direction: search.SortAscending}},
		keyColumns(),
		idKey(),
	)
	byCreated := mustOrder(t,
		[]search.Sort[sortField]{{Field: sortByCreated, Direction: search.SortAscending}},
		keyColumns(),
		idKey(),
	)
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
		timestamp, _ := search.EncodeCursor(byCreated.Signature(), []any{forged, "w_1"})
		if _, err := byCreated.Seek(timestamp); !errors.Is(err, search.ErrInvalidCursor) {
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

// The signature check is the only guard when two orderings decode the same
// values: the same column in the other direction, or another text column.
func TestSeekRejectsCursorsOfAnotherSignature(t *testing.T) {
	byName := mustOrder(t,
		[]search.Sort[sortField]{{Field: sortByName, Direction: search.SortAscending}},
		keyColumns(), idKey())
	token := cursorFor(t, byName, keyRow{ID: "w_1", Name: "a"})

	byNameDesc := mustOrder(t,
		[]search.Sort[sortField]{{Field: sortByName, Direction: search.SortDescending}},
		keyColumns(), idKey())
	if _, err := byNameDesc.Seek(token); !errors.Is(err, search.ErrInvalidCursor) {
		t.Fatalf("Seek() with the cursor of the other direction = %v, want ErrInvalidCursor", err)
	}
	byTenant := mustOrder(t,
		[]search.Sort[sortField]{{Field: sortByName, Direction: search.SortAscending}},
		//nolint:exhaustive // a test lists only the sorts it orders by
		map[sortField]keyset.Key[keyRow]{
			sortByName: keyset.StringKey(colTenant, func(r keyRow) string { return r.Name }),
		},
		idKey())
	if _, err := byTenant.Seek(token); !errors.Is(err, search.ErrInvalidCursor) {
		t.Fatalf("Seek() with the cursor of another text column = %v, want ErrInvalidCursor", err)
	}
}

// A forged cursor with too few or too many values is ErrInvalidCursor, not a
// panic on the missing value.
func TestSeekRejectsAWrongNumberOfValues(t *testing.T) {
	byName := mustOrder(t,
		[]search.Sort[sortField]{{Field: sortByName, Direction: search.SortAscending}},
		keyColumns(), idKey())
	for _, values := range [][]any{{"a"}, {"a", "w_1", "x"}} {
		forged, err := search.EncodeCursor(byName.Signature(), values)
		require.NoError(t, err)
		if _, err = byName.Seek(forged); !errors.Is(err, search.ErrInvalidCursor) {
			t.Fatalf("Seek() with %d values = %v, want ErrInvalidCursor", len(values), err)
		}
	}
}

// With a nullable tiebreak, a cursor that is all NULL in ascending order has
// no row after it. Seek selects nothing: a nil predicate would restart the
// walk at the first row and never end.
func TestSeekAfterAnAllNullCursorSelectsNothing(t *testing.T) {
	order := mustOrder[sortField](t, nil, keyColumns(),
		keyset.NullableStringKey(colNote, func(r keyRow) *string { return r.Note }))
	forged, err := search.EncodeCursor(order.Signature(), []any{nil})
	require.NoError(t, err)
	seek, err := order.Seek(forged)
	require.NoError(t, err)
	if seek == nil {
		t.Fatal("Seek() = nil, want FALSE")
	}
	sql := normalize(jet.SELECT(colID).FROM(testTable).WHERE(seek).DebugSql())
	if !strings.Contains(sql, "WHERE FALSE") {
		t.Fatalf("seek SQL = %q, want WHERE FALSE", sql)
	}
}

func TestTrim(t *testing.T) {
	order := mustOrder[sortField](t, nil, keyColumns(), idKey())
	rows := []keyRow{{ID: "a"}, {ID: "b"}, {ID: "c"}}

	page, next, err := order.Trim(rows, 2)
	if err != nil || len(page) != 2 || next == "" {
		t.Fatalf("Trim(3 rows, 2) = %d rows, %q, %v", len(page), next, err)
	}
	if want := cursorFor(t, order, rows[1]); next != want {
		t.Fatalf("next cursor points at %q, want the last kept row", next)
	}
	if last, end, _ := order.Trim(rows[:2], 2); len(last) != 2 || end != "" {
		t.Fatalf("Trim(2 rows, 2) = %d rows, %q, want the last page", len(last), end)
	}
	// A transactor caller does not call CheckPage, so a zero limit reaches
	// Trim. It keeps the rows and gives no cursor.
	kept, none, err := order.Trim(rows[:1], 0)
	if err != nil || none != "" || len(kept) != 1 {
		t.Fatalf("Trim(1 row, 0) = %d rows, %q, %v; want the rows unchanged and no cursor", len(kept), none, err)
	}
}

func TestKeysCompositeTiebreak(t *testing.T) {
	order := mustOrder(t, []search.Sort[sortField]{{Field: sortByName, Direction: search.SortDescending}},
		keyColumns(), keyset.StringKey(colTenant, func(keyRow) string { return "" }), idKey())
	if got := order.Signature(); got != "widgets.name:DESC,widgets.tenant_id:DESC,widgets.id:DESC" {
		t.Fatalf("Signature() = %q", got)
	}
}

// A column read from a subquery is a different reference from the base
// column, so it must not swallow the tiebreak.
func TestKeysDistinguishSubqueryColumns(t *testing.T) {
	sub := testTable.SELECT(colID, colName).AsTable("latest")
	subID := colID.From(sub)
	order := mustOrder(t,
		[]search.Sort[sortField]{{Field: "SUB", Direction: search.SortAscending}},
		//nolint:exhaustive // a test lists only the sorts it orders by
		map[sortField]keyset.Key[keyRow]{
			"SUB": keyset.StringKey(subID, func(r keyRow) string { return r.Name }),
		},
		idKey(),
	)
	if got := order.Signature(); !strings.Contains(got, "latest.") || !strings.Contains(got, ",widgets.id:ASC") {
		t.Fatalf("Signature() = %q, want the subquery column and the tiebreak", got)
	}
}

func TestNullPlacementInOrderAndSignature(t *testing.T) {
	columns := keyColumns()
	columns[sortByNote] = columns[sortByNote].NullsLast()
	order := mustOrder(t,
		[]search.Sort[sortField]{{Field: sortByNote, Direction: search.SortDescending}},
		columns,
		idKey(),
	)
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
	seekSQL := normalize(jet.SELECT(colID).FROM(testTable).WHERE(seek).DebugSql())
	if !strings.Contains(seekSQL, "(widgets.note < 'n'::text) OR (widgets.note IS NULL)") {
		t.Fatalf("seek SQL = %q, want NULLs after every value when descending NULLS LAST", seekSQL)
	}
}

func TestOrderOfRefusesAnUnknownSortField(t *testing.T) {
	for name, sorts := range map[string][]search.Sort[sortField]{
		"alone": {{Field: sortUnmapped, Direction: search.SortAscending}},
		"after a valid field": {
			{Field: sortByName, Direction: search.SortAscending},
			{Field: sortUnmapped, Direction: search.SortDescending},
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := keyset.OrderOf(sorts, keyColumns(), idKey())
			require.ErrorIs(t, err, search.ErrUnknownSort)
			require.ErrorContains(t, err, string(sortUnmapped))
		})
	}
	_, err := keyset.OrderOf[sortField](nil, keyColumns(), idKey())
	require.NoError(t, err, "no sort orders by the tiebreak")
}
