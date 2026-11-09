package keyset_test

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/db/keyset"
	jet "github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/search"
)

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
	sortByIP     sortField = "IP"
)

func typedOrder(t *testing.T, field sortField) keyset.Order[typedRow] {
	return mustOrder(t, []search.Sort[sortField]{{Field: field, Direction: search.SortAscending}},
		//nolint:exhaustive // a test lists only the sorts it orders by
		map[sortField]keyset.Key[typedRow]{
			sortByStatus: keyset.EnumKey(
				colStatus,
				"widget_status",
				[]string{"on", "off"},
				func(r typedRow) string { return r.Status },
			),
			sortByRef:   keyset.UUIDKey(colRef, func(r typedRow) uuid.UUID { return r.Ref }),
			sortByScore: keyset.FloatKey(colScore, func(r typedRow) float64 { return r.Score }),
			sortByIP:    keyset.InetKey(colStatus, func(r typedRow) string { return r.Status }),
		},
		keyset.StringKey(colID, func(r typedRow) string { return r.ID }))
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
		"malformed inet":     {sortByIP, "10.0.0.300"},
	} {
		t.Run(name, func(t *testing.T) {
			order := typedOrder(t, tc.field)
			forged, err := search.EncodeCursor(order.Signature(), []any{tc.value, "w_1"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = order.Seek(forged); !errors.Is(err, search.ErrInvalidCursor) {
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
			order := typedOrder(t, tc.field)
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
	order := mustOrder(t,
		[]search.Sort[sortField]{{Field: sortByCreated, Direction: search.SortAscending}},
		keyColumns(),
		idKey(),
	)
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
	if _, err = order.Seek(token); err != nil {
		t.Fatalf("Seek() for year 12000: %v", err)
	}
}

// An integer literal is cast to bigint, so a value past the column's width
// compares instead of failing to bind.
func TestIntKeyCastsLiteralToBigint(t *testing.T) {
	colRank := jet.IntegerColumn("rank")
	_ = jet.NewTable("public", "widgets", "", colRank)
	order := mustOrder(t,
		[]search.Sort[sortField]{{Field: "RANK", Direction: search.SortAscending}},
		//nolint:exhaustive // a test lists only the sorts it orders by
		map[sortField]keyset.Key[keyRow]{
			"RANK": keyset.IntKey(colRank, func(keyRow) int64 { return 0 }),
		},
		idKey(),
	)
	token, _ := search.EncodeCursor(order.Signature(), []any{int64(1) << 40, "w_1"})
	seek, err := order.Seek(token)
	if err != nil {
		t.Fatal(err)
	}
	if sql := jet.SELECT(colID).FROM(testTable).WHERE(seek).DebugSql(); !strings.Contains(sql, "::bigint") {
		t.Fatalf("seek SQL = %q, want the literal cast to bigint", sql)
	}
}

// EnumKey writes its type name into the SQL as raw text, so a name that is
// not a plain identifier is a programming error, caught when the key is built.
func TestEnumKeyRefusesATypeNameThatIsNotAnIdentifier(t *testing.T) {
	for _, name := range []string{"", "Status", "status; DROP TABLE x", `"status"`, "9status", "sta-tus"} {
		t.Run(name, func(t *testing.T) {
			require.Panics(t, func() {
				keyset.EnumKey(colStatus, name, []string{"on"}, func(r typedRow) string { return r.Status })
			})
		})
	}
	require.NotPanics(t, func() {
		keyset.EnumKey(colStatus, "public.widget_status", []string{"on"}, func(r typedRow) string { return r.Status })
		keyset.NullableEnumKey(colStatus, "_status_2", []string{"on"}, func(typedRow) *string { return nil })
	})
}

// PostgreSQL inet holds no IPv6 zone, so a cursor with one is refused before
// the cast could fail in the database.
func TestInetKeyRefusesAnAddressWithAZone(t *testing.T) {
	order := typedOrder(t, sortByIP)
	for _, value := range []string{"fe80::1%eth0", "fe80::1%25"} {
		forged, err := search.EncodeCursor(order.Signature(), []any{value, "w_1"})
		require.NoError(t, err)
		_, err = order.Seek(forged)
		require.ErrorIs(t, err, search.ErrInvalidCursor, value)
	}
	for _, value := range []string{"fe80::1", "10.0.0.0/8", "2001:db8::/32"} {
		valid, err := search.EncodeCursor(order.Signature(), []any{value, "w_1"})
		require.NoError(t, err)
		_, err = order.Seek(valid)
		require.NoError(t, err, value)
	}
}
