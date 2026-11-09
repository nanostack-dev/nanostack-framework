package keyset_test

// The golden test proves that the move kept the cursors and the SQL. The
// fixture testdata/golden.json was written by the same cases run through
// pkg/db/transactor (Keys, the key constructors, Cursor, Seek and OrderBy) at
// nanostack-framework commit dbd16f9, before the pager moved here. Never
// regenerate it from db/keyset: a difference is a change of the cursor
// format, which strands every cursor a client holds.

import (
	"encoding/json"
	"math"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/db/keyset"
	jet "github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/search"
)

type gRow struct {
	ID   string
	S    string
	SP   *string
	E    string
	EP   *string
	IP   string
	IPP  *string
	U    uuid.UUID
	UP   *uuid.UUID
	T    time.Time
	TP   *time.Time
	LT   time.Time
	LTP  *time.Time
	I    int64
	IPtr *int64
	F    float64
	FP   *float64
	R    float32
	RP   *float32
}

var (
	gID  = jet.StringColumn("id")
	gS   = jet.StringColumn("s")
	gE   = jet.StringColumn("e")
	gIP  = jet.StringColumn("ip")
	gU   = jet.StringColumn("u")
	gT   = jet.TimestampzColumn("t")
	gLT  = jet.TimestampColumn("lt")
	gI   = jet.IntegerColumn("i")
	gF   = jet.FloatColumn("f")
	gR   = jet.FloatColumn("r")
	_    = jet.NewTable("public", "golden", "", gID, gS, gE, gIP, gU, gT, gLT, gI, gF, gR)
	gLbl = []string{"pending", "active", "archived"}
)

// gKeys has one key per key constructor, and the two NULL placements.
func gKeys() map[string]keyset.Key[gRow] {
	return map[string]keyset.Key[gRow]{
		"S":    keyset.StringKey(gS, func(r gRow) string { return r.S }),
		"SP":   keyset.NullableStringKey(gS, func(r gRow) *string { return r.SP }),
		"SPNF": keyset.NullableStringKey(gS, func(r gRow) *string { return r.SP }).NullsFirst(),
		"SPNL": keyset.NullableStringKey(gS, func(r gRow) *string { return r.SP }).NullsLast(),
		"E":    keyset.EnumKey(gE, "golden_status", gLbl, func(r gRow) string { return r.E }),
		"EP":   keyset.NullableEnumKey(gE, "golden_status", gLbl, func(r gRow) *string { return r.EP }),
		"IP":   keyset.InetKey(gIP, func(r gRow) string { return r.IP }),
		"IPP":  keyset.NullableInetKey(gIP, func(r gRow) *string { return r.IPP }),
		"U":    keyset.UUIDKey(gU, func(r gRow) uuid.UUID { return r.U }),
		"UP":   keyset.NullableUUIDKey(gU, func(r gRow) *uuid.UUID { return r.UP }),
		"T":    keyset.TimeKey(gT, func(r gRow) time.Time { return r.T }),
		"TP":   keyset.NullableTimeKey(gT, func(r gRow) *time.Time { return r.TP }),
		"LT":   keyset.LocalTimeKey(gLT, func(r gRow) time.Time { return r.LT }),
		"LTP":  keyset.NullableLocalTimeKey(gLT, func(r gRow) *time.Time { return r.LTP }),
		"I":    keyset.IntKey(gI, func(r gRow) int64 { return r.I }),
		"IPtr": keyset.NullableIntKey(gI, func(r gRow) *int64 { return r.IPtr }),
		"F":    keyset.FloatKey(gF, func(r gRow) float64 { return r.F }),
		"FP":   keyset.NullableFloatKey(gF, func(r gRow) *float64 { return r.FP }),
		"R":    keyset.RealKey(gR, func(r gRow) float32 { return r.R }),
		"RP":   keyset.NullableRealKey(gR, func(r gRow) *float32 { return r.RP }),
	}
}

type gOut struct {
	Signature string `json:"signature"`
	Cursor    string `json:"cursor"`
	OrderBy   string `json:"order_by"`
	Seek      string `json:"seek"`
}

func goldenRows() map[string]gRow {
	zone := time.FixedZone("plus1", 3600)
	full := gRow{
		ID: "w_1", S: "apple é", SP: new("Banana"), E: "active", EP: new("archived"),
		IP: "10.0.0.2", IPP: new("2001:db8::/32"),
		U:  uuid.MustParse("f47ac10b-58cc-4372-a567-0e02b2c3d479"),
		UP: new(uuid.MustParse("018f6b8e-0000-7000-8000-000000000001")),
		T:  time.Date(2026, 3, 4, 5, 6, 7, 123456789, zone), TP: new(time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)),
		LT: time.Date(2026, 3, 4, 5, 6, 7, 123456000, zone), LTP: new(time.Date(12000, 1, 1, 0, 0, 0, 1000, time.UTC)),
		I: -42, IPtr: new(int64(1) << 40), F: 2.25, FP: new(math.Inf(-1)), R: 0.1, RP: new(float32(-3.5)),
	}
	empty := gRow{
		ID: "w_2", S: "", E: "pending", IP: "::1", U: uuid.Nil,
		T: time.Unix(0, 0).UTC(), LT: time.Unix(0, 0).UTC(), F: math.NaN(), R: float32(math.Inf(1)),
	}
	return map[string]gRow{"full": full, "empty": empty}
}

func goldenSorts() map[string][]search.Sort[string] {
	fields := []string{
		"S", "SP", "SPNF", "SPNL", "E", "EP", "IP", "IPP", "U", "UP",
		"T", "TP", "LT", "LTP", "I", "IPtr", "F", "FP", "R", "RP",
	}
	sorts := map[string][]search.Sort[string]{}
	for _, f := range fields {
		sorts[f+":ASC"] = []search.Sort[string]{{Field: f, Direction: search.SortAscending}}
		sorts[f+":DESC"] = []search.Sort[string]{{Field: f, Direction: search.SortDescending}}
	}
	sorts["none"] = nil
	sorts["mixed"] = []search.Sort[string]{
		{Field: "SP", Direction: search.SortDescending},
		{Field: "FP", Direction: search.SortAscending},
		{Field: "T", Direction: search.SortDescending},
	}
	sorts["uniform"] = []search.Sort[string]{
		{Field: "T", Direction: search.SortAscending},
		{Field: "I", Direction: search.SortAscending},
		{Field: "U", Direction: search.SortAscending},
	}
	return sorts
}

func TestCursorsAndSQLMatchTheTransactorPager(t *testing.T) {
	raw, err := os.ReadFile("testdata/golden.json")
	require.NoError(t, err)
	var want map[string]gOut
	require.NoError(t, json.Unmarshal(raw, &want))

	tie := keyset.StringKey(gID, func(r gRow) string { return r.ID })
	got := map[string]gOut{}
	for name, sorts := range goldenSorts() {
		order, orderErr := keyset.OrderOf(sorts, gKeys(), tie)
		require.NoError(t, orderErr, name)
		for rowName, row := range goldenRows() {
			cursor, cursorErr := order.Cursor(row)
			require.NoError(t, cursorErr, name)
			seek, seekErr := order.Seek(cursor)
			require.NoError(t, seekErr, name)
			got[name+"/"+rowName] = gOut{
				Signature: order.Signature(),
				Cursor:    cursor,
				OrderBy:   jet.SELECT(jet.Int(1)).ORDER_BY(order.OrderBy()...).DebugSql(),
				Seek:      jet.SELECT(jet.Int(1)).WHERE(seek).DebugSql(),
			}
		}
	}
	require.Len(t, want, 86)
	require.Equal(t, want, got)
}
