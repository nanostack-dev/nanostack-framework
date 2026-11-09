// This file is Nanostack-only (not in go-jet): tests of the dialect switch and of quoted
// aliases. See db/UPSTREAM.md.

package jet_test

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/db/internal/jet"
	"github.com/nanostack-dev/nanostack-framework/db/mysql"
	"github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/sqlite"
)

var name = jet.StringColumn("name")

// contains is the shape db/searchexec needs: ILIKE on PostgreSQL, LOWER() LIKE elsewhere.
func contains(term string) jet.BoolExpression {
	return jet.BoolDialectSwitch(func(d jet.Dialect) jet.BoolExpression {
		if d == postgres.Dialect {
			return jet.BoolExp(jet.CustomExpression(name, jet.Token("ILIKE"), jet.String(term)))
		}

		return jet.LOWER(name).LIKE(jet.LOWER(jet.String(term)))
	})
}

func TestDialectSwitchPicksTheStatementDialect(t *testing.T) {
	t.Parallel()

	tests := []struct {
		dialect string
		sql     func() (string, []any)
		want    string
	}{
		{
			dialect: "postgres",
			sql:     postgres.SELECT(postgres.Int(1)).WHERE(contains("ab")).Sql,
			want:    "\nSELECT $1\nWHERE name ILIKE $2;\n",
		},
		{
			dialect: "mysql",
			sql:     mysql.SELECT(mysql.Int(1)).WHERE(contains("ab")).Sql,
			want:    "\nSELECT ?\nWHERE LOWER(name) LIKE LOWER(?);\n",
		},
		{
			dialect: "sqlite",
			sql:     sqlite.SELECT(sqlite.Int(1)).WHERE(contains("ab")).Sql,
			want:    "\nSELECT ?\nWHERE LOWER(name) LIKE LOWER(?);\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.dialect, func(t *testing.T) {
			t.Parallel()

			query, args := tc.sql()
			require.Equal(t, tc.want, query)
			// Only the selected branch adds arguments.
			require.Equal(t, []any{int64(1), "ab"}, args)
		})
	}
}

func TestDialectSwitchOneValueServesEveryDialect(t *testing.T) {
	t.Parallel()

	calls := map[jet.Dialect]int{}
	sw := jet.DialectSwitch(func(d jet.Dialect) jet.Expression {
		calls[d]++
		return jet.String(d.Name())
	})

	pg, pgArgs := postgres.SELECT(sw).Sql()
	my, myArgs := mysql.SELECT(sw).Sql()
	lite, liteArgs := sqlite.SELECT(sw).Sql()

	require.Equal(t, "\nSELECT $1;\n", pg)
	require.Equal(t, []any{"PostgreSQL"}, pgArgs)
	require.Equal(t, "\nSELECT ?;\n", my)
	require.Equal(t, []any{"MySQL"}, myArgs)
	require.Equal(t, "\nSELECT ?;\n", lite)
	require.Equal(t, []any{"SQLite"}, liteArgs)
	require.Equal(t, map[jet.Dialect]int{postgres.Dialect: 1, mysql.Dialect: 1, sqlite.Dialect: 1}, calls)
}

func TestDialectSwitchDebugSQLUsesTheStatementDialect(t *testing.T) {
	t.Parallel()

	require.Equal(t, "\nSELECT 1\nWHERE name ILIKE 'a''b';\n",
		postgres.SELECT(postgres.Int(1)).WHERE(contains("a'b")).DebugSql())
	require.Equal(t, "\nSELECT 1\nWHERE LOWER(name) LIKE LOWER('a''b');\n",
		mysql.SELECT(mysql.Int(1)).WHERE(contains("a'b")).DebugSql())
}

func TestDialectSwitchTypedFormsCompose(t *testing.T) {
	t.Parallel()

	perDialect := func(pg, other int64) jet.IntegerExpression {
		return jet.IntDialectSwitch(func(d jet.Dialect) jet.IntegerExpression {
			if d == postgres.Dialect {
				return jet.Int(pg)
			}

			return jet.Int(other)
		})
	}

	stmt := func(d jet.Dialect) string {
		cond := contains("x").AND(jet.Bool(true)).OR(contains("y"))
		sum := perDialect(10, 20).ADD(jet.Int(1)).AS("total")

		switch d {
		case postgres.Dialect:
			return postgres.SELECT(sum).WHERE(cond).ORDER_BY(perDialect(1, 2).DESC()).DebugSql()
		case mysql.Dialect:
			return mysql.SELECT(sum).WHERE(cond).ORDER_BY(perDialect(1, 2).DESC()).DebugSql()
		default:
			return sqlite.SELECT(sum).WHERE(cond).ORDER_BY(perDialect(1, 2).DESC()).DebugSql()
		}
	}

	require.Equal(t, `
SELECT (10 + 1) AS "total"
WHERE ((name ILIKE 'x') AND TRUE) OR (name ILIKE 'y')
ORDER BY 1 DESC;
`, stmt(postgres.Dialect))
	require.Equal(t, `
SELECT (20 + 1) AS "total"
WHERE ((LOWER(name) LIKE LOWER('x')) AND TRUE) OR (LOWER(name) LIKE LOWER('y'))
ORDER BY 2 DESC;
`, stmt(mysql.Dialect))
	require.Equal(t, `
SELECT (20 + 1) AS "total"
WHERE ((LOWER(name) LIKE LOWER('x')) AND TRUE) OR (LOWER(name) LIKE LOWER('y'))
ORDER BY 2 DESC;
`, stmt(sqlite.Dialect))
}

func TestDialectSwitchEveryTypedForm(t *testing.T) {
	t.Parallel()

	raw := func(d jet.Dialect) jet.Expression { return jet.Raw(d.Name()) }
	forms := map[string]jet.Expression{
		"Bool":  jet.BoolDialectSwitch(func(d jet.Dialect) jet.BoolExpression { return jet.BoolExp(raw(d)) }),
		"Int":   jet.IntDialectSwitch(func(d jet.Dialect) jet.IntegerExpression { return jet.IntExp(raw(d)) }),
		"Float": jet.FloatDialectSwitch(func(d jet.Dialect) jet.FloatExpression { return jet.FloatExp(raw(d)) }),
		"String": jet.StringDialectSwitch(
			func(d jet.Dialect) jet.StringExpression { return jet.StringExp(raw(d)) },
		),
		"Date":  jet.DateDialectSwitch(func(d jet.Dialect) jet.DateExpression { return jet.DateExp(raw(d)) }),
		"Time":  jet.TimeDialectSwitch(func(d jet.Dialect) jet.TimeExpression { return jet.TimeExp(raw(d)) }),
		"Timez": jet.TimezDialectSwitch(func(d jet.Dialect) jet.TimezExpression { return jet.TimezExp(raw(d)) }),
		"Timestamp": jet.TimestampDialectSwitch(
			func(d jet.Dialect) jet.TimestampExpression { return jet.TimestampExp(raw(d)) },
		),
		"Timestampz": jet.TimestampzDialectSwitch(
			func(d jet.Dialect) jet.TimestampzExpression { return jet.TimestampzExp(raw(d)) },
		),
		"Interval": jet.IntervalDialectSwitch(
			func(d jet.Dialect) jet.IntervalExpression { return jet.IntervalExp(raw(d)) },
		),
		"Blob": jet.BlobDialectSwitch(func(d jet.Dialect) jet.BlobExpression { return jet.BlobExp(raw(d)) }),
	}

	for form, expr := range forms {
		require.Equal(t, "\nSELECT PostgreSQL;\n", postgres.SELECT(expr).DebugSql(), form)
		require.Equal(t, "\nSELECT MySQL;\n", mysql.SELECT(expr).DebugSql(), form)
		require.Equal(t, "\nSELECT SQLite;\n", sqlite.SELECT(expr).DebugSql(), form)
	}
}

func TestDialectSwitchGetsAnyDialect(t *testing.T) {
	t.Parallel()

	other := jet.NewDialect(jet.DialectParams{
		Name:                "Other",
		AliasQuoteChar:      '"',
		IdentifierQuoteChar: '"',
		ArgumentPlaceholder: func(int) string { return "?" },
	})

	var got jet.Dialect
	out := jet.SQLBuilder{Dialect: other}
	jet.Serialize(jet.DialectSwitch(func(d jet.Dialect) jet.Expression {
		got = d
		return jet.Int(7)
	}), jet.SelectStatementType, &out)

	require.Same(t, other, got)
	require.Equal(t, "?", out.Buff.String())
	require.Equal(t, []any{int64(7)}, out.Args)
}

func TestDialectSwitchRefusesNil(t *testing.T) {
	t.Parallel()

	require.PanicsWithValue(t, "jet: DialectSwitch needs a function", func() {
		jet.DialectSwitch(nil)
	})
	require.PanicsWithValue(t, "jet: DialectSwitch needs a function", func() {
		jet.BoolDialectSwitch(nil)
	})

	none := jet.BoolDialectSwitch(func(jet.Dialect) jet.BoolExpression { return nil })
	require.PanicsWithValue(t, "jet: DialectSwitch returned no expression for dialect MySQL", func() {
		mysql.SELECT(mysql.Int(1)).WHERE(none).Sql()
	})
}

func TestDialectSwitchIsSafeForConcurrentUse(t *testing.T) {
	t.Parallel()

	cond := contains("ab")

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			pg, _ := postgres.SELECT(postgres.Int(1)).WHERE(cond).Sql()
			my, _ := mysql.SELECT(mysql.Int(1)).WHERE(cond).Sql()
			require.Equal(t, "\nSELECT $1\nWHERE name ILIKE $2;\n", pg)
			require.Equal(t, "\nSELECT ?\nWHERE LOWER(name) LIKE LOWER(?);\n", my)
		})
	}
	wg.Wait()
}

func TestWriteAliasEscapesTheQuoteCharacter(t *testing.T) {
	t.Parallel()

	alias := `a"b`
	hostile := `x" FROM secrets --`

	require.Equal(t, "\nSELECT 1 AS \"a\"\"b\";\n", postgres.SELECT(postgres.Int(1).AS(alias)).DebugSql())
	require.Equal(t, "\nSELECT 1 AS \"a\"\"b\";\n", mysql.SELECT(mysql.Int(1).AS(alias)).DebugSql())
	require.Equal(t, "\nSELECT 1 AS \"a\"\"b\";\n", sqlite.SELECT(sqlite.Int(1).AS(alias)).DebugSql())

	// The whole alias stays one quoted name.
	require.Equal(t, "\nSELECT 1 AS \"x\"\" FROM secrets --\";\n",
		postgres.SELECT(postgres.Int(1).AS(hostile)).DebugSql())

	// An alias without the quote character is written as before.
	require.Equal(t, "\nSELECT 1 AS \"table.col\";\n", postgres.SELECT(postgres.Int(1).AS("table.col")).DebugSql())

	out := jet.SQLBuilder{Dialect: postgres.Dialect}
	out.WriteAlias(`"`)
	require.Equal(t, `""""`, out.Buff.String())
}
