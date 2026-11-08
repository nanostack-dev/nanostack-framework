// This file is Nanostack-only (not in go-jet). See db/UPSTREAM.md.

package postgres

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
		{"Bool", Bool(true), BoolOpt(functional.Some(true)), BoolOpt(functional.None[bool]()), "NULL::boolean"},
		{"Int", Int(1), IntOpt(functional.Some[int64](1)), IntOpt(functional.None[int64]()), "NULL"},
		{"Int8", Int8(1), Int8Opt(functional.Some[int8](1)), Int8Opt(functional.None[int8]()), "NULL::smallint"},
		{"Int16", Int16(1), Int16Opt(functional.Some[int16](1)), Int16Opt(functional.None[int16]()), "NULL::smallint"},
		{"Int32", Int32(1), Int32Opt(functional.Some[int32](1)), Int32Opt(functional.None[int32]()), "NULL::integer"},
		{"Int64", Int64(1), Int64Opt(functional.Some[int64](1)), Int64Opt(functional.None[int64]()), "NULL::bigint"},
		{"Uint8", Uint8(1), Uint8Opt(functional.Some[uint8](1)), Uint8Opt(functional.None[uint8]()), "NULL::smallint"},
		{
			"Uint16",
			Uint16(1),
			Uint16Opt(functional.Some[uint16](1)),
			Uint16Opt(functional.None[uint16]()),
			"NULL::integer",
		},
		{
			"Uint32",
			Uint32(1),
			Uint32Opt(functional.Some[uint32](1)),
			Uint32Opt(functional.None[uint32]()),
			"NULL::bigint",
		},
		{
			"Uint64",
			Uint64(1),
			Uint64Opt(functional.Some[uint64](1)),
			Uint64Opt(functional.None[uint64]()),
			"NULL::bigint",
		},
		{"Float", Float(1.5), FloatOpt(functional.Some(1.5)), FloatOpt(functional.None[float64]()), "NULL"},
		{"Real", Real(1.5), RealOpt(functional.Some[float32](1.5)), RealOpt(functional.None[float32]()), "NULL::real"},
		{
			"Double",
			Double(1.5),
			DoubleOpt(functional.Some(1.5)),
			DoubleOpt(functional.None[float64]()),
			"NULL::double precision",
		},
		{
			"Decimal",
			Decimal("1.5"),
			DecimalOpt(functional.Some("1.5")),
			DecimalOpt(functional.None[string]()),
			"NULL::decimal",
		},
		{"String", String("a"), StringOpt(functional.Some("a")), StringOpt(functional.None[string]()), "NULL::text"},
		{"Text", Text("a"), TextOpt(functional.Some("a")), TextOpt(functional.None[string]()), "NULL::text"},
		{
			"Char",
			Char(3)("a"),
			CharOpt(3)(functional.Some("a")),
			CharOpt(3)(functional.None[string]()),
			"NULL::char(3)",
		},
		{
			"VarChar",
			VarChar(3)("a"),
			VarCharOpt(3)(functional.Some("a")),
			VarCharOpt(3)(functional.None[string]()),
			"NULL::varchar(3)",
		},
		{
			"Json",
			Json(`{"a":1}`),
			JsonOpt(functional.Some(`{"a":1}`)),
			JsonOpt(functional.None[string]()),
			"NULL::json",
		},
		{"UUID", UUID(id), UUIDOpt(functional.Some(id)), UUIDOpt(functional.None[uuid.UUID]()), "NULL::uuid"},
		{
			"Bytea",
			Bytea([]byte("a")),
			ByteaOpt(functional.Some([]byte("a"))),
			ByteaOpt(functional.None[[]byte]()),
			"NULL::bytea",
		},
		{"Date", DateT(ts), DateOpt(functional.Some(ts)), DateOpt(functional.None[time.Time]()), "NULL::date"},
		{
			"Time",
			TimeT(ts),
			TimeOpt(functional.Some(ts)),
			TimeOpt(functional.None[time.Time]()),
			"NULL::time without time zone",
		},
		{
			"Timez",
			TimezT(ts),
			TimezOpt(functional.Some(ts)),
			TimezOpt(functional.None[time.Time]()),
			"NULL::time with time zone",
		},
		{
			"Timestamp",
			TimestampT(ts),
			TimestampOpt(functional.Some(ts)),
			TimestampOpt(functional.None[time.Time]()),
			"NULL::timestamp without time zone",
		},
		{
			"Timestampz",
			TimestampzT(ts),
			TimestampzOpt(functional.Some(ts)),
			TimestampzOpt(functional.None[time.Time]()),
			"NULL::timestamp with time zone",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			testutils.AssertOptLiteral(t, Dialect, c.plain, c.some, c.non, c.debugNone)
		})
	}
}

func TestLiteralOptTypedNull(t *testing.T) {
	assertSerialize(t, StringOpt(functional.None[string]()), "$1::text", nil)
	assertDebugSerialize(t, StringOpt(functional.None[string]()), "NULL::text")
}
