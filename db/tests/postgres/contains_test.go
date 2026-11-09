package postgres

import (
	"context"
	"crypto/rand"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	. "github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/searchexec"
)

// containsTable is a scratch table for the searchexec.Contains tests: tenant "a" has rows
// 1 to 10, tenant "b" has rows 11 and 12.
type containsTable struct {
	Table

	id                 ColumnInteger
	tenant, name, note ColumnString
	// cName holds the value of name in COLLATE "C".
	cName ColumnString
}

var containsRows = []struct {
	id         int
	tenant     string
	name, note any
}{
	{1, "a", "a%b", nil},
	{2, "a", "a_b", nil},
	{3, "a", "a!b", nil},
	{4, "a", `a\b`, nil},
	{5, "a", "axb", nil},
	{6, "a", "Hello World", nil},
	{7, "a", "ÉCOLE", nil},
	{8, "a", "café", nil},
	{9, "a", nil, "note with xyz"},
	{10, "a", nil, nil},
	{11, "b", "xyz", "xyz"},
	{12, "b", "other", "has xyz"},
}

// newContainsTable creates the table with a new random name, so the statement-cache runs
// and a shuffled order never meet a cached statement of a dropped table.
func newContainsTable(t *testing.T) containsTable {
	t.Helper()

	tableName := "nanostack_contains_" + strings.ToLower(rand.Text())
	qualified := "test_sample." + tableName

	// CockroachDB has no "C" collation (it refuses the locale), so there c_name is plain TEXT.
	cCollate := ` COLLATE "C"`
	if sourceIsCockroachDB() {
		cCollate = ""
	}

	_, err := db.ExecContext(ctx, "CREATE TABLE "+qualified+
		" (id INT PRIMARY KEY, tenant TEXT NOT NULL, name TEXT, note TEXT, c_name TEXT"+cCollate+")")
	require.NoError(t, err)
	t.Cleanup(func() {
		_, dropErr := db.ExecContext(context.Background(), "DROP TABLE "+qualified)
		require.NoError(t, dropErr)
	})

	for _, row := range containsRows {
		_, err = db.ExecContext(
			ctx,
			"INSERT INTO "+qualified+" (id, tenant, name, note, c_name) VALUES ($1, $2, $3, $4, $3)",
			row.id,
			row.tenant,
			row.name,
			row.note,
		)
		require.NoError(t, err)
	}

	tbl := containsTable{
		id:     IntegerColumn("id"),
		tenant: StringColumn("tenant"),
		name:   StringColumn("name"),
		note:   StringColumn("note"),
		cName:  StringColumn("c_name"),
	}
	tbl.Table = NewTable("test_sample", tableName, "", tbl.id, tbl.tenant, tbl.name, tbl.note, tbl.cName)

	return tbl
}

// ids returns the ids of the rows where cond is true, in ascending order.
func (c containsTable) ids(t *testing.T, cond BoolExpression) []int {
	t.Helper()

	query, args := SELECT(c.id).FROM(c.Table).WHERE(cond).ORDER_BY(c.id.ASC()).Sql()
	rows, err := db.QueryContext(ctx, query, args...)
	require.NoError(t, err, query)
	defer rows.Close()

	ids := []int{}
	for rows.Next() {
		var id int
		require.NoError(t, rows.Scan(&id))
		ids = append(ids, id)
	}
	require.NoError(t, rows.Err())

	return ids
}

func TestContains(t *testing.T) {
	c := newContainsTable(t)
	contains := func(term string, exprs ...StringExpression) []int {
		return c.ids(t, searchexec.Contains(term, exprs...))
	}

	t.Run("wildcards and the escape character are literal", func(t *testing.T) {
		require.Equal(t, []int{1}, contains("%", c.name))
		require.Equal(t, []int{2}, contains("_", c.name))
		require.Equal(t, []int{3}, contains("!", c.name))
		require.Equal(t, []int{4}, contains(`\`, c.name))
		require.Equal(t, []int{1}, contains("a%b", c.name))
		require.Equal(t, []int{2}, contains("a_b", c.name))
		require.Equal(t, []int{3}, contains("a!b", c.name))
		require.Equal(t, []int{4}, contains(`a\b`, c.name))
		require.Equal(t, []int{}, contains("!%", c.name))
	})

	t.Run("ASCII case", func(t *testing.T) {
		require.Equal(t, []int{6}, contains("hello", c.name))
		require.Equal(t, []int{6}, contains("WORLD", c.name))
		require.Equal(t, []int{6}, contains("lO wO", c.name))
	})

	t.Run("Unicode case", func(t *testing.T) {
		require.Equal(t, []int{7}, contains("école", c.name))
		require.Equal(t, []int{8}, contains("CAFÉ", c.name))
	})

	// PostgreSQL's default collations do not ignore accents (MySQL's _ai_ collations do).
	t.Run("accents count", func(t *testing.T) {
		require.Equal(t, []int{}, contains("cafe", c.name))
		require.Equal(t, []int{}, contains("ecole", c.name))
	})

	t.Run("the term is trimmed", func(t *testing.T) {
		require.Equal(t, []int{6}, contains("  hello \t", c.name))
		require.Equal(t, []int{}, contains("hello  world", c.name))
	})

	t.Run("a blank term filters nothing", func(t *testing.T) {
		all := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
		require.Equal(t, all, contains("", c.name))
		require.Equal(t, all, contains(" \t\n", c.name, c.note))
	})

	t.Run("several expressions", func(t *testing.T) {
		require.Equal(t, []int{11}, contains("xyz", c.name))
		require.Equal(t, []int{9, 11, 12}, contains("xyz", c.name, c.note))
		require.Equal(t, []int{9, 11, 12}, contains("XYZ", c.note, c.name))
	})

	// Rows 11 and 12 of tenant "b" match the term in name and in note. If the matches
	// were not one unit, tenant = 'a' AND name ILIKE x OR note ILIKE x would return them.
	t.Run("the scope applies to every match", func(t *testing.T) {
		scope := c.tenant.EQ(String("a"))
		require.Equal(t, []int{9}, c.ids(t, scope.AND(searchexec.Contains("xyz", c.name, c.note))))
		require.Equal(t, []int{9}, c.ids(t, searchexec.Contains("xyz", c.name, c.note).AND(scope)))
		require.Equal(t, []int{9}, c.ids(t, AND(scope, searchexec.Contains("xyz", c.name, c.note))))
	})

	// A text column cannot hold invalid UTF-8, and PostgreSQL text cannot hold NUL, so
	// Contains gives FALSE: no row and no error on every dialect.
	t.Run("a term that is not valid UTF-8 or holds NUL matches no row", func(t *testing.T) {
		for _, term := range []string{"a\x00b", "\x00", "\xff", "caf\xc3"} {
			require.Equal(t, []int{}, contains(term, c.name, c.note), "%q", term)
		}
		scope := c.tenant.EQ(String("a"))
		require.Equal(t, []int{}, c.ids(t, scope.AND(searchexec.Contains("\xff", c.name, c.note))))
	})

	// Pinned: ILIKE folds the case through the column's ctype. Under COLLATE "C" it folds
	// ASCII letters only, as SQLite does.
	t.Run(`COLLATE "C" folds ASCII letters only`, func(t *testing.T) {
		skipForCockroachDB(t)

		require.Equal(t, []int{6}, contains("HELLO", c.cName))
		require.Equal(t, []int{}, contains("école", c.cName))
		require.Equal(t, []int{7}, contains("ÉCole", c.cName))
	})
}
