package transactor_test

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nanostack-dev/nanostack-framework/db/keyset"
	jet "github.com/nanostack-dev/nanostack-framework/db/postgres"
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
	order := transactor.Keys(
		[]search.Sort[sortField]{{Field: "ID", Direction: search.SortDescending}},
		columns,
		idKey(),
	)
	if got := order.Signature(); got != "widgets.id:DESC" {
		t.Fatalf("Signature() = %q", got)
	}
}

func keysetBuilder() *transactor.PageBuilder[keyRow, keyRow] {
	return transactor.Page(nil, func(r keyRow) keyRow { return r }, colID, colName).From(testTable)
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

func TestStrictKeysRefuseAnUnknownSortField(t *testing.T) {
	for name, sorts := range map[string][]search.Sort[sortField]{
		"alone":                {{Field: sortUnmapped, Direction: search.SortAscending}},
		"after a valid field":  {{Field: sortByName, Direction: search.SortAscending}, {Field: sortUnmapped, Direction: search.SortDescending}},
		"the injected default": search.NewRequest[struct{}, sortField]().WithSort(nil, nil).Sort,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := transactor.StrictKeys(sorts, keyColumns(), idKey())
			if !errors.Is(err, search.ErrUnknownSort) {
				t.Fatalf("StrictKeys() error = %v, want ErrUnknownSort", err)
			}
		})
	}
}

func TestStrictKeysMatchKeysForMappedFields(t *testing.T) {
	sorts := []search.Sort[sortField]{
		{Field: sortByCreated, Direction: search.SortDescending},
		{Field: sortByName, Direction: search.SortAscending},
	}
	strict, err := transactor.StrictKeys(sorts, keyColumns(), idKey())
	if err != nil {
		t.Fatal(err)
	}
	if lenient := transactor.Keys(sorts, keyColumns(), idKey()); strict.Signature() != lenient.Signature() {
		t.Fatalf("StrictKeys %q differs from Keys %q", strict.Signature(), lenient.Signature())
	}
	if _, err = transactor.StrictKeys[sortField](nil, keyColumns(), idKey()); err != nil {
		t.Fatalf("no sort: %v", err)
	}
}

// A page never locks rows. Before the move the count ran FOR SHARE and
// PostgreSQL refused it; now Run refuses the lock itself, inside or outside a
// transaction, and runs nothing.
func TestPageRunRefusesARowLock(t *testing.T) {
	order := transactor.Keys[sortField](nil, keyColumns(), idKey())
	for name, lock := range map[string]func(context.Context) context.Context{
		"ForShare":       transactor.ForShare,
		"ForUpdate":      transactor.ForUpdate,
		"ForNoKeyUpdate": transactor.ForNoKeyUpdate,
		"ForKeyShare":    transactor.ForKeyShare,
		"WithRowLock": func(ctx context.Context) context.Context {
			return transactor.WithRowLock(ctx, jet.UPDATE().SKIP_LOCKED())
		},
	} {
		t.Run(name, func(t *testing.T) {
			for _, ctx := range []context.Context{t.Context(), inFakeTx(t.Context())} {
				// db is nil and the fake transaction cannot run a statement:
				// the error must come before any query.
				_, err := keysetBuilder().Keyset(order).Run(lock(ctx), search.Pagination{Limit: 1}).Value()
				if !errors.Is(err, transactor.ErrRowLockSearch) {
					t.Fatalf("Run() under %s = %v, want ErrRowLockSearch", name, err)
				}
			}
		})
	}
}

// The forwarders build the keys of db/keyset: the same signature, the same
// cursor and the same seek. db/keyset's golden test pins those against the
// pager as it was before the move.
func TestForwardersBuildTheKeysOfDBKeyset(t *testing.T) {
	type fRow struct {
		ID  string
		S   string
		U   uuid.UUID
		T   time.Time
		I   int64
		F   float64
		R   float32
		Ptr *string
	}
	text, tm, local := jet.StringColumn("s"), jet.TimestampzColumn("t"), jet.TimestampColumn("lt")
	integer, float, ref := jet.IntegerColumn("i"), jet.FloatColumn("f"), jet.StringColumn("u")
	_ = jet.NewTable("public", "forwarded", "", colID, text, tm, local, integer, float, ref)
	labels := []string{"a", "b"}
	pairs := map[string][2]transactor.Key[fRow]{
		"String": {transactor.StringKey(text, func(r fRow) string { return r.S }),
			keyset.StringKey(text, func(r fRow) string { return r.S })},
		"NullableString": {transactor.NullableStringKey(text, func(r fRow) *string { return r.Ptr }),
			keyset.NullableStringKey(text, func(r fRow) *string { return r.Ptr })},
		"Enum": {transactor.EnumKey(text, "letter", labels, func(fRow) string { return "b" }),
			keyset.EnumKey(text, "letter", labels, func(fRow) string { return "b" })},
		"NullableEnum": {transactor.NullableEnumKey(text, "letter", labels, func(r fRow) *string { return r.Ptr }),
			keyset.NullableEnumKey(text, "letter", labels, func(r fRow) *string { return r.Ptr })},
		"Inet": {transactor.InetKey(text, func(fRow) string { return "10.0.0.1" }),
			keyset.InetKey(text, func(fRow) string { return "10.0.0.1" })},
		"NullableInet": {transactor.NullableInetKey(text, func(r fRow) *string { return r.Ptr }),
			keyset.NullableInetKey(text, func(r fRow) *string { return r.Ptr })},
		"UUID": {transactor.UUIDKey(ref, func(r fRow) uuid.UUID { return r.U }),
			keyset.UUIDKey(ref, func(r fRow) uuid.UUID { return r.U })},
		"NullableUUID": {transactor.NullableUUIDKey(ref, func(fRow) *uuid.UUID { return nil }),
			keyset.NullableUUIDKey(ref, func(fRow) *uuid.UUID { return nil })},
		"Time": {transactor.TimeKey(tm, func(r fRow) time.Time { return r.T }),
			keyset.TimeKey(tm, func(r fRow) time.Time { return r.T })},
		"NullableTime": {transactor.NullableTimeKey(tm, func(r fRow) *time.Time { return &r.T }),
			keyset.NullableTimeKey(tm, func(r fRow) *time.Time { return &r.T })},
		"LocalTime": {transactor.LocalTimeKey(local, func(r fRow) time.Time { return r.T }),
			keyset.LocalTimeKey(local, func(r fRow) time.Time { return r.T })},
		"NullableLocalTime": {transactor.NullableLocalTimeKey(local, func(fRow) *time.Time { return nil }),
			keyset.NullableLocalTimeKey(local, func(fRow) *time.Time { return nil })},
		"Int": {transactor.IntKey(integer, func(r fRow) int64 { return r.I }),
			keyset.IntKey(integer, func(r fRow) int64 { return r.I })},
		"NullableInt": {transactor.NullableIntKey(integer, func(r fRow) *int64 { return &r.I }),
			keyset.NullableIntKey(integer, func(r fRow) *int64 { return &r.I })},
		"Float": {transactor.FloatKey(float, func(r fRow) float64 { return r.F }),
			keyset.FloatKey(float, func(r fRow) float64 { return r.F })},
		"NullableFloat": {transactor.NullableFloatKey(float, func(r fRow) *float64 { return &r.F }),
			keyset.NullableFloatKey(float, func(r fRow) *float64 { return &r.F })},
		"Real": {transactor.RealKey(float, func(r fRow) float32 { return r.R }),
			keyset.RealKey(float, func(r fRow) float32 { return r.R })},
		"NullableReal": {transactor.NullableRealKey(float, func(r fRow) *float32 { return &r.R }),
			keyset.NullableRealKey(float, func(r fRow) *float32 { return &r.R })},
	}
	row := fRow{
		ID: "w_1", S: "apple", U: uuid.MustParse("f47ac10b-58cc-4372-a567-0e02b2c3d479"),
		T: time.Date(2026, 3, 4, 5, 6, 7, 123456000, time.UTC), I: 1 << 40, F: math.Inf(-1), R: 0.1,
	}
	tiebreak := transactor.StringKey(colID, func(r fRow) string { return r.ID })
	for name, pair := range pairs {
		t.Run(name, func(t *testing.T) {
			var got [2]string
			for i, key := range pair {
				order := transactor.Keys(
					[]search.Sort[string]{{Field: "K", Direction: search.SortDescending}},
					map[string]transactor.Key[fRow]{"K": key.NullsLast()},
					tiebreak,
				)
				token, err := order.Cursor(row)
				if err != nil {
					t.Fatal(err)
				}
				seek, err := order.Seek(token)
				if err != nil {
					t.Fatal(err)
				}
				got[i] = order.Signature() + " " + token + " " + jet.SELECT(jet.Int(1)).WHERE(seek).DebugSql()
			}
			if got[0] != got[1] {
				t.Fatalf("forwarder %q\ndb/keyset %q", got[0], got[1])
			}
		})
	}
}
