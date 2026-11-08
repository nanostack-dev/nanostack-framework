// This file is Nanostack-only (not in go-jet). See db/UPSTREAM.md.

package sqlite

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nanostack-dev/nanostack-framework/db/internal/jet"
	"github.com/nanostack-dev/nanostack-framework/db/internal/testutils"
	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
)

func TestLiteralOpt(t *testing.T) {
	ts := time.Date(2024, 1, 2, 3, 4, 5, 6000, time.UTC)
	id := uuid.MustParse("a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11")

	cases := []struct {
		name             string
		plain, some, non jet.Serializer
		debugNone        string
	}{
		{"Bool", Bool(true), BoolOpt(functional.Some(true)), BoolOpt(functional.None[bool]()), "NULL"},
		{"Int", Int(1), IntOpt(functional.Some[int64](1)), IntOpt(functional.None[int64]()), "NULL"},
		{"Int8", Int8(1), Int8Opt(functional.Some[int8](1)), Int8Opt(functional.None[int8]()), "NULL"},
		{"Int16", Int16(1), Int16Opt(functional.Some[int16](1)), Int16Opt(functional.None[int16]()), "NULL"},
		{"Int32", Int32(1), Int32Opt(functional.Some[int32](1)), Int32Opt(functional.None[int32]()), "NULL"},
		{"Int64", Int64(1), Int64Opt(functional.Some[int64](1)), Int64Opt(functional.None[int64]()), "NULL"},
		{"Uint8", Uint8(1), Uint8Opt(functional.Some[uint8](1)), Uint8Opt(functional.None[uint8]()), "NULL"},
		{"Uint16", Uint16(1), Uint16Opt(functional.Some[uint16](1)), Uint16Opt(functional.None[uint16]()), "NULL"},
		{"Uint32", Uint32(1), Uint32Opt(functional.Some[uint32](1)), Uint32Opt(functional.None[uint32]()), "NULL"},
		{"Uint64", Uint64(1), Uint64Opt(functional.Some[uint64](1)), Uint64Opt(functional.None[uint64]()), "NULL"},
		{"Float", Float(1.5), FloatOpt(functional.Some(1.5)), FloatOpt(functional.None[float64]()), "NULL"},
		{"Decimal", Decimal("1.5"), DecimalOpt(functional.Some("1.5")), DecimalOpt(functional.None[string]()), "NULL"},
		{"String", String("a"), StringOpt(functional.Some("a")), StringOpt(functional.None[string]()), "NULL"},
		{"UUID", UUID(id), UUIDOpt(functional.Some(id)), UUIDOpt(functional.None[uuid.UUID]()), "NULL"},
		{"Blob", Blob([]byte("a")), BlobOpt(functional.Some([]byte("a"))), BlobOpt(functional.None[[]byte]()), "NULL"},
		{"Date", Date(2024, 1, 2), DateOpt(functional.Some(ts)), DateOpt(functional.None[time.Time]()), "DATE(NULL)"},
		{
			"Time",
			Time(3, 4, 5, 6000),
			TimeOpt(functional.Some(ts)),
			TimeOpt(functional.None[time.Time]()),
			"TIME(NULL)",
		},
		{
			"DateTime",
			DateTime(2024, 1, 2, 3, 4, 5, 6000),
			DateTimeOpt(functional.Some(ts)),
			DateTimeOpt(functional.None[time.Time]()),
			"DATETIME(NULL)",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			testutils.AssertOptLiteral(t, Dialect, c.plain, c.some, c.non, c.debugNone)
		})
	}
}

// A time in another location is written as its UTC instant, the value SQLite's DATE,
// TIME and DATETIME give for the same time stored with its offset through VALUES or
// MODEL. 03:04:05+05:00 on Jan 2 is 22:04:05 UTC on Jan 1, so the date changes too.
func TestLiteralOptTimeIsUTC(t *testing.T) {
	plus5 := time.FixedZone("PLUS5", 5*60*60)
	ts := time.Date(2024, 1, 2, 3, 4, 5, 6000, plus5)

	cases := []struct {
		name             string
		plain, some, non jet.Serializer
		debugNone        string
	}{
		{"Date", Date(2024, 1, 1), DateOpt(functional.Some(ts)), DateOpt(functional.None[time.Time]()), "DATE(NULL)"},
		{
			"Time",
			Time(22, 4, 5, 6000),
			TimeOpt(functional.Some(ts)),
			TimeOpt(functional.None[time.Time]()),
			"TIME(NULL)",
		},
		{
			"DateTime",
			DateTime(2024, 1, 1, 22, 4, 5, 6000),
			DateTimeOpt(functional.Some(ts)),
			DateTimeOpt(functional.None[time.Time]()),
			"DATETIME(NULL)",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			testutils.AssertOptLiteral(t, Dialect, c.plain, c.some, c.non, c.debugNone)
		})
	}
}
