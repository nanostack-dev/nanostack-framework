package keyset_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/db/keyset"
	jet "github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/search"
)

// hostileCase is one cursor value given to one key.
type hostileCase struct {
	label string
	raw   string
}

func micros(t time.Time) string {
	epoch := time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)
	return fmt.Sprint((t.Unix()-epoch.Unix())*1_000_000 + int64(t.Nanosecond()/1_000))
}

func timeCases() []hostileCase {
	at := func(year int, month time.Month, day, hour, minute, second, micro int) hostileCase {
		v := time.Date(year, month, day, hour, minute, second, micro*1_000, time.UTC)
		return hostileCase{label: v.Format("2006-01-02T15:04:05.999999"), raw: micros(v)}
	}
	return []hostileCase{
		at(2026, time.March, 4, 10, 0, 0, 0),
		at(-1, time.December, 31, 23, 59, 59, 999_999),
		at(0, time.January, 1, 0, 0, 0, 0),
		at(999, time.December, 31, 23, 59, 59, 999_999),
		at(1000, time.January, 1, 0, 0, 0, 0),
		at(1970, time.January, 1, 0, 0, 0, 999_999),
		at(1970, time.January, 1, 0, 0, 1, 0),
		at(2038, time.January, 19, 3, 14, 7, 999_999),
		at(2038, time.January, 19, 3, 14, 8, 0),
		at(9999, time.December, 31, 23, 59, 59, 999_999),
		at(10000, time.January, 1, 0, 0, 0, 0),
		{label: "past the PostgreSQL range", raw: "9223372036854775807"},
		{label: "text", raw: `"2026-03-04T10:00:00Z"`},
		{label: "fraction", raw: "1.5"},
		{label: "null", raw: "null"},
	}
}

func hostileCases() map[dField][]hostileCase {
	text := []hostileCase{
		{label: "plain", raw: `"plain"`},
		{label: "NUL", raw: `"a\u0000b"`},
		{label: "number", raw: "123"},
		{label: "object", raw: "{}"},
		{label: "null", raw: "null"},
	}
	//nolint:exhaustive // the NULLs-first and nullable forms share a codec with a listed key
	return map[dField][]hostileCase{
		dName: text,
		dNote: {{label: "null on a nullable key", raw: "null"}, {label: "NUL", raw: `"a\u0000b"`}},
		dStatus: {
			{label: "label", raw: `"active"`},
			{label: "empty (MySQL index 0)", raw: `""`},
			{label: "not a label", raw: `"zzz"`},
			{label: "NUL", raw: `"a\u0000b"`},
			{label: "number", raw: "1"},
			{label: "null", raw: "null"},
		},
		dRef: {
			{label: "canonical", raw: `"018f6b8e-0000-7000-8000-000000000001"`},
			{label: "upper case", raw: `"018F6B8E-0000-7000-8000-000000000001"`},
			{label: "not a uuid", raw: `"not-a-uuid"`},
			{label: "empty", raw: `""`},
			{label: "number", raw: "1"},
		},
		dRank: {
			{label: "one", raw: "1"},
			{label: "int64 max", raw: "9223372036854775807"},
			{label: "int64 min", raw: "-9223372036854775808"},
			{label: "past int64", raw: "9223372036854775808"},
			{label: "fraction", raw: "1.5"},
			{label: "exponent", raw: "1e3"},
			{label: "text", raw: `"1"`},
		},
		dScore: {
			{label: "finite", raw: "1.5"},
			{label: "largest double", raw: fmt.Sprint(math.MaxFloat64)},
			{label: "NaN", raw: `"NaN"`},
			{label: "Infinity", raw: `"Infinity"`},
			{label: "-Infinity", raw: `"-Infinity"`},
			{label: "unknown word", raw: `"inf"`},
			{label: "past double", raw: "1e400"},
			{label: "bool", raw: "true"},
		},
		dRatio: {
			{label: "finite", raw: "0.1"},
			{label: "past real", raw: "1e39"},
			{label: "NaN", raw: `"NaN"`},
			{label: "Infinity", raw: `"Infinity"`},
		},
		dCreated: timeCases(),
		dStamp:   timeCases(),
		dAt:      timeCases(),
		dIP: {
			{label: "address", raw: `"10.0.0.1"`},
			{label: "network", raw: `"10.0.0.0/8"`},
			{label: "zone", raw: `"fe80::1%eth0"`},
			{label: "not an address", raw: `"host"`},
		},
	}
}

// TestHostileCursors gives every codec on every dialect the values a forged
// cursor can carry. A value the column cannot hold must be ErrInvalidCursor
// (400) before any statement runs, never a database error (500).
func TestHostileCursors(t *testing.T) {
	fields := []dField{dName, dNote, dStatus, dRef, dRank, dScore, dRatio, dCreated, dStamp, dAt, dIP}
	cases := hostileCases()
	var golden strings.Builder
	for _, field := range fields {
		fmt.Fprintf(&golden, "## %s\n", field)
		for _, value := range cases[field] {
			fmt.Fprintf(&golden, "%-28s", value.label)
			for _, dialect := range dDialects {
				table := newDTable(dialect)
				order := table.order(t, asc(field))
				cursor, err := search.EncodeCursor(order.Signature(), []any{json.RawMessage(value.raw), "w_1"})
				require.NoError(t, err)
				fake := &fakeDB{answers: []answer{countAnswer(0)}}
				_, err = keyset.Page(fake.open(), func(r dRow) dRow { return r }, jet.StringColumn("id")).
					From(table.source).Keyset(order).Run(t.Context(), search.Pagination{Limit: 1, Cursor: cursor})
				switch {
				case err == nil:
					fmt.Fprintf(&golden, " %s=ok", dialect)
				case errors.Is(err, search.ErrInvalidCursor), errors.Is(err, keyset.ErrDialect):
					require.Empty(t, fake.recorded(), "a refused cursor runs no statement")
					fmt.Fprintf(&golden, " %s=%s", dialect, errorName(err))
				default:
					t.Fatalf("%s %s on %s: %v", field, value.label, dialect, err)
				}
			}
			golden.WriteString("\n")
		}
		golden.WriteString("\n")
	}
	compareGolden(t, "hostile_cursors.golden", golden.String())
}
