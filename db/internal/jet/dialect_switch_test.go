// This file is Nanostack-only (not in go-jet): tests of the dialect switch and of quoted
// aliases. See db/UPSTREAM.md.

package jet_test

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
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
			// FailNow must run on the test goroutine, so assert, not require.
			assert.Equal(t, "\nSELECT $1\nWHERE name ILIKE $2;\n", pg)
			assert.Equal(t, "\nSELECT ?\nWHERE LOWER(name) LIKE LOWER(?);\n", my)
		})
	}
	wg.Wait()
}

func TestWriteAliasKeepsTheAliasOneQuotedName(t *testing.T) {
	t.Parallel()

	type statement interface{ DebugSql() string }

	alias := func(name string) map[string]statement {
		return map[string]statement{
			"postgres": postgres.SELECT(postgres.Int(1).AS(name)),
			"mysql":    mysql.SELECT(mysql.Int(1).AS(name)),
			"sqlite":   sqlite.SELECT(sqlite.Int(1).AS(name)),
		}
	}

	tests := []struct {
		name  string
		alias string
		want  map[string]string
	}{
		{
			// An alias with no quote and no backslash is written as before.
			name:  "plain",
			alias: "table.col",
			want: map[string]string{
				"postgres": `"table.col"`,
				"mysql":    `"table.col"`,
				"sqlite":   `"table.col"`,
			},
		},
		{
			// An alias with a quote is written with the identifier quote, doubled.
			name:  "quote",
			alias: `a"b`,
			want: map[string]string{
				"postgres": `"a""b"`,
				"mysql":    "`a\"b`",
				"sqlite":   "`a\"b`",
			},
		},
		{
			name:  "quote and backtick",
			alias: "a\"b`c",
			want: map[string]string{
				"postgres": "\"a\"\"b`c\"",
				"mysql":    "`a\"b``c`",
				"sqlite":   "`a\"b``c`",
			},
		},
		{
			// MySQL reads a "-quoted alias as a string literal, where a backslash
			// escapes the next character. A backtick-quoted name reads no escape.
			name:  "trailing backslash",
			alias: `a\`,
			want: map[string]string{
				"postgres": `"a\"`,
				"mysql":    "`a\\`",
				"sqlite":   "`a\\`",
			},
		},
		{
			name:  "backslash before a quote",
			alias: `\" UNION SELECT 99 -- `,
			want: map[string]string{
				"postgres": `"\"" UNION SELECT 99 -- "`,
				"mysql":    "`\\\" UNION SELECT 99 -- `",
				"sqlite":   "`\\\" UNION SELECT 99 -- `",
			},
		},
		{
			name:  "backslash and a second column",
			alias: `x\" , 'INJECTED' AS q; -- `,
			want: map[string]string{
				"postgres": `"x\"" , 'INJECTED' AS q; -- "`,
				"mysql":    "`x\\\" , 'INJECTED' AS q; -- `",
				"sqlite":   "`x\\\" , 'INJECTED' AS q; -- `",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			for dialect, stmt := range alias(tc.alias) {
				require.Equal(t, "\nSELECT 1 AS "+tc.want[dialect]+";\n", stmt.DebugSql(), dialect)
			}
		})
	}
}

func TestWriteJsonObjKeyBindsAKeyWithABackslash(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		alias    string
		wantSQL  string
		wantArgs []any
	}{
		{
			// A key without a backslash stays a literal, with the single quote doubled.
			name:     "single quote",
			alias:    "author's name",
			wantSQL:  "\nSELECT JSON_OBJECT(\n          'author''s name', ?\n     ) AS \"json\";\n",
			wantArgs: []any{int64(1)},
		},
		{
			name:     "trailing backslash",
			alias:    `x\`,
			wantSQL:  "\nSELECT JSON_OBJECT(\n          ?, ?\n     ) AS \"json\";\n",
			wantArgs: []any{`x\`, int64(1)},
		},
		{
			// In a literal, \' ends the key and the rest is read as SQL.
			name:     "backslash before a quote",
			alias:    `x\', (SELECT @@version), 'k', 1) #`,
			wantSQL:  "\nSELECT JSON_OBJECT(\n          ?, ?\n     ) AS \"json\";\n",
			wantArgs: []any{`x\', (SELECT @@version), 'k', 1) #`, int64(1)},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			query, args := mysql.SELECT_JSON_OBJ(mysql.Int(1).AS(tc.alias)).Sql()
			require.Equal(t, tc.wantSQL, query)
			require.Equal(t, tc.wantArgs, args)
		})
	}
}

func TestWriteIdentifierEscapesTheQuoteCharacter(t *testing.T) {
	t.Parallel()

	name := "a\"b`c"

	t.Run("subquery column", func(t *testing.T) {
		t.Parallel()

		pgSub := postgres.SELECT(postgres.Int(1).AS(name)).AsTable("sub")
		require.Equal(
			t,
			"\nSELECT sub.\"a\"\"b`c\" AS \"a\"\"b`c\"\nFROM (\n          SELECT 1 AS \"a\"\"b`c\"\n     ) AS sub;\n",
			postgres.SELECT(postgres.IntegerColumn(name).From(pgSub)).FROM(pgSub).DebugSql(),
		)

		mySub := mysql.SELECT(mysql.Int(1).AS(name)).AsTable("sub")
		require.Equal(
			t,
			"\nSELECT sub.`a\"b``c` AS `a\"b``c`\nFROM (\n          SELECT 1 AS `a\"b``c`\n     ) AS sub;\n",
			mysql.SELECT(mysql.IntegerColumn(name).From(mySub)).FROM(mySub).DebugSql(),
		)

		liteSub := sqlite.SELECT(sqlite.Int(1).AS(name)).AsTable("sub")
		require.Equal(
			t,
			"\nSELECT sub.`a\"b``c` AS `a\"b``c`\nFROM (\n          SELECT 1 AS `a\"b``c`\n     ) AS sub;\n",
			sqlite.SELECT(sqlite.IntegerColumn(name).From(liteSub)).FROM(liteSub).DebugSql(),
		)
	})

	t.Run("CTE name", func(t *testing.T) {
		t.Parallel()

		pgCTE := postgres.CTE(name)
		require.Equal(
			t,
			"\nWITH \"a\"\"b`c\" AS (\n     SELECT 1 AS \"v\"\n)\nSELECT \"a\"\"b`c\".v AS \"v\"\nFROM \"a\"\"b`c\";\n",
			postgres.WITH(pgCTE.AS(postgres.SELECT(postgres.Int(1).AS("v"))))(
				postgres.SELECT(postgres.IntegerColumn("v").From(pgCTE)).FROM(pgCTE),
			).DebugSql(),
		)

		myCTE := mysql.CTE(name)
		require.Equal(
			t,
			"\nWITH `a\"b``c` AS (\n     SELECT 1 AS \"v\"\n)\nSELECT `a\"b``c`.v AS \"v\"\nFROM `a\"b``c`;\n",
			mysql.WITH(myCTE.AS(mysql.SELECT(mysql.Int(1).AS("v"))))(
				mysql.SELECT(mysql.IntegerColumn("v").From(myCTE)).FROM(myCTE),
			).DebugSql(),
		)

		liteCTE := sqlite.CTE(name)
		require.Equal(
			t,
			"\nWITH `a\"b``c` AS (\n     SELECT 1 AS \"v\"\n)\nSELECT `a\"b``c`.v AS \"v\"\nFROM `a\"b``c`;\n",
			sqlite.WITH(liteCTE.AS(sqlite.SELECT(sqlite.Int(1).AS("v"))))(
				sqlite.SELECT(sqlite.IntegerColumn("v").From(liteCTE)).FROM(liteCTE),
			).DebugSql(),
		)
	})

	t.Run("builder", func(t *testing.T) {
		t.Parallel()

		out := jet.SQLBuilder{Dialect: mysql.Dialect}
		out.WriteIdentifier("a`b")
		require.Equal(t, "`a``b`", out.Buff.String())
	})
}
