package sqlite

import (
	"context"
	"crypto/rand"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/db/searchexec"
	. "github.com/nanostack-dev/nanostack-framework/db/sqlite"
)

// containsTable is a scratch table for the searchexec.Contains tests: tenant "a" has rows
// 1 to 10, tenant "b" has rows 11 and 12.
type containsTable struct {
	Table

	id                 ColumnInteger
	tenant, name, note ColumnString
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

	ctx := context.Background()
	tableName := "nanostack_contains_" + strings.ToLower(rand.Text())

	_, err := db.ExecContext(ctx, "CREATE TABLE "+tableName+
		" (id INTEGER PRIMARY KEY, tenant TEXT NOT NULL, name TEXT, note TEXT)")
	require.NoError(t, err)
	t.Cleanup(func() {
		_, dropErr := db.ExecContext(context.Background(), "DROP TABLE "+tableName)
		require.NoError(t, dropErr)
	})

	for _, row := range containsRows {
		_, err = db.ExecContext(ctx, "INSERT INTO "+tableName+" (id, tenant, name, note) VALUES (?, ?, ?, ?)",
			row.id, row.tenant, row.name, row.note)
		require.NoError(t, err)
	}

	tbl := containsTable{
		id:     IntegerColumn("id"),
		tenant: StringColumn("tenant"),
		name:   StringColumn("name"),
		note:   StringColumn("note"),
	}
	tbl.Table = NewTable("", tableName, "", tbl.id, tbl.tenant, tbl.name, tbl.note)

	return tbl
}

// ids returns the ids of the rows where cond is true, in ascending order.
func (c containsTable) ids(t *testing.T, cond BoolExpression) []int {
	t.Helper()

	query, args := SELECT(c.id).FROM(c.Table).WHERE(cond).ORDER_BY(c.id.ASC()).Sql()
	rows, err := db.QueryContext(context.Background(), query, args...)
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

	// LOWER on both sides also holds while PRAGMA case_sensitive_like is on.
	t.Run("ASCII case with case_sensitive_like", func(t *testing.T) {
		ctx := context.Background()
		tx, err := db.BeginTx(ctx, nil)
		require.NoError(t, err)
		defer func() { require.NoError(t, tx.Rollback()) }()

		_, err = tx.ExecContext(ctx, "PRAGMA case_sensitive_like = ON")
		require.NoError(t, err)
		defer func() {
			_, resetErr := tx.ExecContext(ctx, "PRAGMA case_sensitive_like = OFF")
			require.NoError(t, resetErr)
		}()

		query, args := SELECT(c.id).FROM(c.Table).WHERE(searchexec.Contains("WORLD", c.name)).Sql()
		var id int
		require.NoError(t, tx.QueryRowContext(ctx, query, args...).Scan(&id))
		require.Equal(t, 6, id)
	})

	// Pinned: SQLite's LOWER and LIKE fold ASCII letters only (with no ICU extension), so
	// "é" does not find "É" here. PostgreSQL and MySQL find it.
	t.Run("Unicode case is ASCII only", func(t *testing.T) {
		require.Equal(t, []int{}, contains("école", c.name))
		require.Equal(t, []int{}, contains("CAFÉ", c.name))
		require.Equal(t, []int{7}, contains("École", c.name))
		require.Equal(t, []int{8}, contains("CAFé", c.name))
	})

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
	// were not one unit, tenant = 'a' AND name LIKE x OR note LIKE x would return them.
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
}
