package sqlite

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	. "github.com/nanostack-dev/nanostack-framework/db/sqlite"
)

// TestHostileAliasStaysOneName runs each hostile alias, and a subquery column that
// refers to it. The server must return one row with one column, whose name is the
// alias and whose value is 1 (Nanostack: quoted aliases, see db/UPSTREAM.md).
func TestHostileAliasStaysOneName(t *testing.T) {
	names := []string{
		"a\"b`c",
		`a\`,
		`\" UNION SELECT 99 --`,
		`x\" , 'INJECTED' AS q; --`,
		`x" FROM secrets --`,
	}

	for _, name := range names {
		sub := SELECT(Int(1).AS(name)).AsTable("sub")
		requireOneNamedValue(t, SELECT(Int(1).AS(name)), name)
		requireOneNamedValue(t, SELECT(IntegerColumn(name).From(sub)).FROM(sub), name)
	}
}

func requireOneNamedValue(t *testing.T, stmt Statement, name string) {
	t.Helper()

	query, args := stmt.Sql()
	rows, err := db.QueryContext(context.Background(), query, args...)
	require.NoError(t, err, query)
	defer rows.Close()

	columns, err := rows.Columns()
	require.NoError(t, err)
	require.Equal(t, []string{name}, columns, query)

	var values []int64
	for rows.Next() {
		var value int64
		require.NoError(t, rows.Scan(&value))
		values = append(values, value)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []int64{1}, values, query)
}
