package mysql

import (
	"context"
	"crypto/rand"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	. "github.com/nanostack-dev/nanostack-framework/db/mysql"
	"github.com/nanostack-dev/nanostack-framework/db/searchexec"
)

// containsTable is a scratch table for the searchexec.Contains tests: tenant "a" has rows
// 1 to 10, tenant "b" has rows 11 and 12. name, generalName and binName hold the same
// value with three collations.
type containsTable struct {
	Table

	id                                       ColumnInteger
	tenant, name, generalName, binName, note ColumnString
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

// accentInsensitiveCollation is the accent- and case-insensitive UCA collation of the
// server: the default of utf8mb4 on MySQL 8, and its MariaDB counterpart.
func accentInsensitiveCollation() string {
	if sourceIsMariaDB() {
		return "utf8mb4_uca1400_ai_ci"
	}

	return "utf8mb4_0900_ai_ci"
}

// newContainsTable creates the table with a new random name, so the statement-cache runs
// and a shuffled order never meet a cached statement of a dropped table.
func newContainsTable(t *testing.T) containsTable {
	t.Helper()

	ctx := context.Background()
	tableName := "nanostack_contains_" + strings.ToLower(rand.Text())
	qualified := "test_sample." + tableName
	ai := accentInsensitiveCollation()

	_, err := db.ExecContext(ctx, "CREATE TABLE "+qualified+` (
		id INT PRIMARY KEY,
		tenant VARCHAR(10) NOT NULL,
		name VARCHAR(100) COLLATE `+ai+`,
		general_name VARCHAR(100) COLLATE utf8mb4_general_ci,
		bin_name VARCHAR(100) COLLATE utf8mb4_bin,
		note VARCHAR(100) COLLATE `+ai+`
	) DEFAULT CHARSET = utf8mb4 COLLATE = `+ai)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, dropErr := db.ExecContext(context.Background(), "DROP TABLE "+qualified)
		require.NoError(t, dropErr)
	})

	for _, row := range containsRows {
		_, err = db.ExecContext(ctx, "INSERT INTO "+qualified+
			" (id, tenant, name, general_name, bin_name, note) VALUES (?, ?, ?, ?, ?, ?)",
			row.id, row.tenant, row.name, row.name, row.name, row.note)
		require.NoError(t, err)
	}

	tbl := containsTable{
		id:          IntegerColumn("id"),
		tenant:      StringColumn("tenant"),
		name:        StringColumn("name"),
		generalName: StringColumn("general_name"),
		binName:     StringColumn("bin_name"),
		note:        StringColumn("note"),
	}
	tbl.Table = NewTable("test_sample", tableName, "",
		tbl.id, tbl.tenant, tbl.name, tbl.generalName, tbl.binName, tbl.note)

	return tbl
}

// ids returns the ids of the rows where cond is true, in ascending order.
func (c containsTable) ids(t *testing.T, q queryer, cond BoolExpression) []int {
	t.Helper()

	query, args := SELECT(c.id).FROM(c.Table).WHERE(cond).ORDER_BY(c.id.ASC()).Sql()
	rows, err := q.QueryContext(context.Background(), query, args...)
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
		return c.ids(t, db, searchexec.Contains(term, exprs...))
	}

	// ESCAPE '!' is the same SQL in every sql_mode, and '\' stays an ordinary character
	// although it is MySQL's default LIKE escape.
	for _, mode := range []string{"default", "", "NO_BACKSLASH_ESCAPES", "ANSI_QUOTES"} {
		t.Run("wildcards and the escape character are literal/sql_mode="+mode, func(t *testing.T) {
			ctx := context.Background()
			// A transaction keeps one connection, so the session sql_mode applies to
			// every statement. The mode is put back before the connection goes back.
			tx, err := db.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer func() { require.NoError(t, tx.Rollback()) }()

			if mode != "default" {
				saved := queryString(t, tx, "SELECT @@SESSION.sql_mode")
				_, err = tx.ExecContext(ctx, "SET SESSION sql_mode = '"+mode+"'")
				require.NoError(t, err)
				defer func() {
					_, resetErr := tx.ExecContext(ctx, "SET SESSION sql_mode = '"+saved+"'")
					require.NoError(t, resetErr)
				}()
			}

			for _, col := range []StringExpression{c.name, c.binName} {
				in := func(term string) []int { return c.ids(t, tx, searchexec.Contains(term, col)) }
				require.Equal(t, []int{1}, in("%"))
				require.Equal(t, []int{2}, in("_"))
				require.Equal(t, []int{3}, in("!"))
				require.Equal(t, []int{4}, in(`\`))
				require.Equal(t, []int{1}, in("a%b"))
				require.Equal(t, []int{2}, in("a_b"))
				require.Equal(t, []int{3}, in("a!b"))
				require.Equal(t, []int{4}, in(`a\b`))
				require.Equal(t, []int{}, in("!%"))
			}
		})
	}

	// LOWER on both sides: a binary collation compares with case, and the match still
	// does not.
	t.Run("ASCII case", func(t *testing.T) {
		for _, col := range []StringExpression{c.name, c.generalName, c.binName} {
			require.Equal(t, []int{6}, contains("hello", col))
			require.Equal(t, []int{6}, contains("WORLD", col))
			require.Equal(t, []int{6}, contains("lO wO", col))
		}
	})

	t.Run("Unicode case", func(t *testing.T) {
		for _, col := range []StringExpression{c.name, c.generalName, c.binName} {
			require.Equal(t, []int{7}, contains("école", col))
			require.Equal(t, []int{8}, contains("CAFÉ", col))
		}
	})

	// Pinned: an accent-insensitive collation ignores accents in LIKE, so the term "e"
	// finds "é" here and not on PostgreSQL or SQLite. A binary collation does not.
	t.Run("accents follow the collation", func(t *testing.T) {
		for _, col := range []StringExpression{c.name, c.generalName} {
			require.Equal(t, []int{8}, contains("cafe", col))
			require.Equal(t, []int{7}, contains("ecole", col))
		}
		require.Equal(t, []int{}, contains("cafe", c.binName))
		require.Equal(t, []int{}, contains("ecole", c.binName))
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
		require.Equal(t, []int{9}, c.ids(t, db, scope.AND(searchexec.Contains("xyz", c.name, c.note))))
		require.Equal(t, []int{9}, c.ids(t, db, searchexec.Contains("xyz", c.name, c.note).AND(scope)))
		require.Equal(t, []int{9}, c.ids(t, db, AND(scope, searchexec.Contains("xyz", c.name, c.note))))
	})
}
