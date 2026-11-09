package searchexec_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/db/internal/jet"
	"github.com/nanostack-dev/nanostack-framework/db/keyset"
	"github.com/nanostack-dev/nanostack-framework/db/mysql"
	"github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/search"
	"github.com/nanostack-dev/nanostack-framework/db/searchexec"
	"github.com/nanostack-dev/nanostack-framework/db/sqlite"
)

var (
	name   = jet.StringColumn("name")
	note   = jet.StringColumn("note")
	tenant = jet.StringColumn("tenant")
)

// where serializes cond as the WHERE clause of a statement of each dialect.
func where(cond jet.BoolExpression) map[string]statementSQL {
	pg, pgArgs := postgres.SELECT(postgres.Int(1)).WHERE(cond).Sql()
	my, myArgs := mysql.SELECT(mysql.Int(1)).WHERE(cond).Sql()
	lite, liteArgs := sqlite.SELECT(sqlite.Int(1)).WHERE(cond).Sql()

	return map[string]statementSQL{
		"postgres": {pg, pgArgs[1:]},
		"mysql":    {my, myArgs[1:]},
		"sqlite":   {lite, liteArgs[1:]},
	}
}

type statementSQL struct {
	query string
	args  []any
}

func TestContainsSQLPerDialect(t *testing.T) {
	t.Parallel()

	got := where(searchexec.Contains("ab", name))

	lowerLike := statementSQL{"\nSELECT ?\nWHERE LOWER(name) LIKE LOWER(?) ESCAPE '!';\n", []any{"%ab%"}}
	assert.Equal(t, statementSQL{"\nSELECT $1\nWHERE name ILIKE $2 ESCAPE '!';\n", []any{"%ab%"}}, got["postgres"])
	assert.Equal(t, lowerLike, got["mysql"])
	assert.Equal(t, lowerLike, got["sqlite"])
}

func TestContainsPattern(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		term string
		want string
	}{
		{name: "plain", term: "ab", want: "%ab%"},
		{name: "percent", term: "50%", want: "%50!%%"},
		{name: "underscore", term: "a_b", want: "%a!_b%"},
		{name: "escape character", term: "a!b", want: "%a!!b%"},
		{name: "backslash is ordinary", term: `a\b`, want: `%a\b%`},
		{name: "all of them", term: `%_!\`, want: `%!%!_!!\%`},
		{name: "escape characters already doubled", term: "!!%", want: "%!!!!!%%"},
		{name: "trimmed", term: " \t ab c\n ", want: "%ab c%"},
		// The database folds the case, not Go: strings.ToLower and LOWER disagree on
		// some letters.
		{name: "case kept", term: "ÉCOLE İ", want: "%ÉCOLE İ%"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			for dialect, got := range where(searchexec.Contains(tc.term, name)) {
				assert.Equal(t, []any{tc.want}, got.args, dialect)
			}
		})
	}
}

func TestContainsBlankTermIsTrue(t *testing.T) {
	t.Parallel()

	for _, term := range []string{"", " ", "\t\n "} {
		cond := searchexec.Contains(term, name, note)
		require.NotNil(t, cond)

		got := where(cond)
		assert.Equal(t, statementSQL{"\nSELECT $1\nWHERE TRUE;\n", []any{}}, got["postgres"])
		assert.Equal(t, statementSQL{"\nSELECT ?\nWHERE TRUE;\n", []any{}}, got["mysql"])
		assert.Equal(t, statementSQL{"\nSELECT ?\nWHERE TRUE;\n", []any{}}, got["sqlite"])
	}
}

// A term that is not valid UTF-8, or that holds NUL, cannot match a text column. Without
// the rule PostgreSQL refuses the statement and MySQL can match every row.
func TestContainsInvalidTermIsFalse(t *testing.T) {
	t.Parallel()

	for _, term := range []string{"a\x00b", "\x00", " \x00 ", "\xff", "caf\xc3"} {
		cond := searchexec.Contains(term, name, note)
		require.NotNil(t, cond)

		got := where(cond)
		assert.Equal(t, statementSQL{"\nSELECT $1\nWHERE FALSE;\n", []any{}}, got["postgres"], "%q", term)
		assert.Equal(t, statementSQL{"\nSELECT ?\nWHERE FALSE;\n", []any{}}, got["mysql"], "%q", term)
		assert.Equal(t, statementSQL{"\nSELECT ?\nWHERE FALSE;\n", []any{}}, got["sqlite"], "%q", term)
	}
}

// A blank term composes like any other condition: it must not be nil, which panics in AND.
func TestContainsBlankTermComposes(t *testing.T) {
	t.Parallel()

	got := where(tenant.EQ(jet.String("a")).AND(searchexec.Contains("  ", name)))
	assert.Equal(t, statementSQL{"\nSELECT $1\nWHERE (tenant = $2) AND TRUE;\n", []any{"a"}}, got["postgres"])
}

func TestContainsSeveralExpressions(t *testing.T) {
	t.Parallel()

	got := where(searchexec.Contains("ab", name, note))

	assert.Equal(t, statementSQL{
		"\nSELECT $1\nWHERE (\n          (name ILIKE $2 ESCAPE '!')\n              OR (note ILIKE $3 ESCAPE '!')\n      );\n",
		[]any{"%ab%", "%ab%"},
	}, got["postgres"])
	assert.Equal(t, statementSQL{
		"\nSELECT ?\nWHERE (\n          (LOWER(name) LIKE LOWER(?) ESCAPE '!')\n              OR (LOWER(note) LIKE LOWER(?) ESCAPE '!')\n      );\n",
		[]any{"%ab%", "%ab%"},
	}, got["mysql"])
	assert.Equal(t, got["mysql"], got["sqlite"])
}

// The matches of several expressions stay one unit next to a scope, so the statement can
// never read scope AND e1 OR e2.
func TestContainsKeepsPrecedenceNextToAScope(t *testing.T) {
	t.Parallel()

	got := where(tenant.EQ(jet.String("a")).AND(searchexec.Contains("ab", name, note)))

	assert.Equal(t, statementSQL{
		"\nSELECT $1\nWHERE (tenant = $2) AND (\n          (name ILIKE $3 ESCAPE '!')\n              OR (note ILIKE $4 ESCAPE '!')\n      );\n",
		[]any{"a", "%ab%", "%ab%"},
	}, got["postgres"])
	assert.Equal(t, statementSQL{
		"\nSELECT ?\nWHERE (tenant = ?) AND (\n          (LOWER(name) LIKE LOWER(?) ESCAPE '!')\n              OR (LOWER(note) LIKE LOWER(?) ESCAPE '!')\n      );\n",
		[]any{"a", "%ab%", "%ab%"},
	}, got["mysql"])
	assert.Equal(t, got["mysql"], got["sqlite"])

	one := where(tenant.EQ(jet.String("a")).AND(searchexec.Contains("ab", name)))
	assert.Equal(t, "\nSELECT $1\nWHERE (tenant = $2) AND (name ILIKE $3 ESCAPE '!');\n", one["postgres"].query)
}

// The escape character is in the SQL text, not a bound argument, so the debug SQL of each
// dialect shows the same clause.
func TestContainsDebugSQL(t *testing.T) {
	t.Parallel()

	cond := searchexec.Contains("a'b", name)

	assert.Equal(t, "\nSELECT 1\nWHERE name ILIKE '%a''b%' ESCAPE '!';\n",
		postgres.SELECT(postgres.Int(1)).WHERE(cond).DebugSql())
	assert.Equal(t, "\nSELECT 1\nWHERE LOWER(name) LIKE LOWER('%a''b%') ESCAPE '!';\n",
		mysql.SELECT(mysql.Int(1)).WHERE(cond).DebugSql())
	assert.Equal(t, "\nSELECT 1\nWHERE LOWER(name) LIKE LOWER('%a''b%') ESCAPE '!';\n",
		sqlite.SELECT(sqlite.Int(1)).WHERE(cond).DebugSql())
}

func TestContainsPanicsWithNoExpression(t *testing.T) {
	t.Parallel()

	assert.PanicsWithValue(t, "searchexec: Contains needs at least one expression", func() {
		searchexec.Contains("ab")
	})
	// Also for a blank term: the call is wrong whatever the term.
	assert.PanicsWithValue(t, "searchexec: Contains needs at least one expression", func() {
		searchexec.Contains("")
	})
}

func TestContainsPanicsOnANilExpression(t *testing.T) {
	t.Parallel()

	assert.PanicsWithValue(t, "searchexec: Contains expression 1 is nil", func() {
		searchexec.Contains("ab", name, nil)
	})
}

// longestOR returns the most OR operators that one pair of parentheses of query joins.
func longestOR(query string) int {
	var counts []int
	longest := 0
	for i := 0; i < len(query); i++ {
		switch {
		case query[i] == '(':
			counts = append(counts, 0)
		case query[i] == ')':
			counts = counts[:len(counts)-1]
		case strings.HasPrefix(query[i:], " OR ") && len(counts) > 0:
			counts[len(counts)-1]++
			longest = max(longest, counts[len(counts)-1])
		}
	}
	return longest
}

// SQLite refuses an expression tree deeper than 1000 levels, and its parser nests each
// operand of an OR one level deeper. A list of up to search.MaxInValues terms or values
// joins its matches in nested groups of at most 64, so it runs there. The answer does not
// change: OR is associative.
func TestLongListsJoinTheirMatchesInGroups(t *testing.T) {
	t.Parallel()

	terms := make([]string, search.MaxInValues)
	for i := range terms {
		terms[i] = fmt.Sprint("t", i)
	}
	for _, n := range []int{2, 64, 65, 200, search.MaxInValues} {
		anyOf := where(searchexec.ContainsAny(terms[:n], name))["sqlite"]
		assert.Equal(t, min(n-1, 63), longestOR(anyOf.query), "ContainsAny of %d terms", n)
		assert.Len(t, anyOf.args, n, "one pattern per term")

		fold := dCount(t, func(q dSearch, _ keyset.Source) dSearch {
			return searchexec.NewInFold[string](q.Query, dEventOrg).In(terms[:n]...)
		})["sqlite"]
		assert.Equal(t, min(n-1, 63), longestOR(fold.query), "InFold of %d values", n)
		assert.Len(t, fold.args, n+1, "the scope value and one pattern per value")
	}
	// One group keeps today's SQL.
	assert.Equal(t, "SELECT $1 WHERE ( (name ILIKE $2 ESCAPE '!') OR (name ILIKE $3 ESCAPE '!') );",
		flat(where(searchexec.ContainsAny([]string{"a", "b"}, name))["postgres"].query))
}
