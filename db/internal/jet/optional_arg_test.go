// This file is Nanostack-only (not in go-jet). See db/UPSTREAM.md.

package jet

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
)

func TestOptionLiteral(t *testing.T) {
	assertClauseSerialize(t, Literal(functional.Some(1)), "$1", 1)
	assertClauseSerialize(t, Literal(functional.None[int]()), "$1", nil)
	assertClauseDebugSerialize(t, Literal(functional.Some(1)), "1")
	assertClauseDebugSerialize(t, Literal(functional.None[int]()), "NULL")
	assertClauseDebugSerialize(t, Literal(functional.Some("a")), "'a'")
}

func TestOptionFixedLiteral(t *testing.T) {
	assertClauseSerialize(t, FixedLiteral(functional.Some("a")), "'a'")
	assertClauseSerialize(t, FixedLiteral(functional.None[string]()), "NULL")
}

func TestOptionRawArgs(t *testing.T) {
	expr := Raw("#a = #b", map[string]any{"#a": functional.None[string](), "#b": functional.Some(int64(2))})
	assertClauseSerialize(t, expr, "($1 = $2)", nil, int64(2))
	assertClauseDebugSerialize(t, expr, "(NULL = 2)", nil, int64(2)) // raw debug keeps the args
}

type optionModel struct {
	Col1   int64
	ColInt functional.Option[int64]
	Col3   functional.Option[int64]
}

func TestOptionUnwindRowFromModel(t *testing.T) {
	row := UnwindRowFromModel([]Column{table1Col1, table1ColInt, table1Col3},
		optionModel{Col1: 1, ColInt: functional.Some(int64(0))})

	out := SQLBuilder{Dialect: defaultDialect}
	for _, s := range row {
		s.serialize(InsertStatementType, &out)
	}
	require.Equal(t, "$1 $2 $3", out.Buff.String())
	require.Equal(t, []any{int64(1), int64(0), nil}, out.Args, "Some(0) is a value, None is NULL")
}

func TestOptionUnwindRowFromValues(t *testing.T) {
	row := UnwindRowFromValues(functional.Some(1), []any{functional.None[string]()})

	out := SQLBuilder{Dialect: defaultDialect}
	for _, s := range row {
		s.serialize(InsertStatementType, &out)
	}
	require.Equal(t, []any{1, nil}, out.Args)
}

// A *functional.Option is rejected on every write path with the fix in the message, as
// qrm rejects a *functional.Option destination field on reads.
func TestOptionPointerArgumentPanics(t *testing.T) {
	some, none := functional.Some("a"), functional.None[string]()
	const want = "jet: a pointer to functional.Option is not supported as an SQL argument " +
		"(*functional.Option[string]); use functional.Option[string]"

	serialize := func(s Serializer, debug bool) {
		out := SQLBuilder{Dialect: defaultDialect, Debug: debug}
		s.serialize(SelectStatementType, &out)
	}
	require.PanicsWithValue(t, want, func() { serialize(Literal(&some), false) })
	require.PanicsWithValue(t, want, func() { serialize(Literal(&none), true) }, "DebugSql")
	require.PanicsWithValue(t, want, func() { serialize(FixedLiteral(&some), false) })
	require.PanicsWithValue(t, want, func() {
		serialize(Raw("#a", map[string]any{"#a": &some}), false)
	})
	row := UnwindRowFromValues(&some, nil)
	require.PanicsWithValue(t, want, func() { serialize(row[0], false) }, "VALUES")

	var nilPtr *functional.Option[string]
	require.PanicsWithValue(t, want, func() { serialize(Literal(nilPtr), false) }, "a nil *Option is rejected too")
}

func TestOptionPointerModelFieldPanics(t *testing.T) {
	some := functional.Some(int64(1))
	type model struct {
		Col1   int64
		ColInt *functional.Option[int64]
	}
	const want = "jet: a pointer to functional.Option is not supported (column col_int, " +
		"field type *functional.Option[int64]); use functional.Option[int64]"
	for _, m := range []model{{Col1: 1, ColInt: &some}, {Col1: 1}} {
		require.PanicsWithValue(t, want, func() {
			UnwindRowFromModel([]Column{table1Col1, table1ColInt}, m)
		})
	}
}
