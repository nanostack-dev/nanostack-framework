package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	. "github.com/nanostack-dev/nanostack-framework/db/mysql"
)

// hostileNames are aliases that end a badly quoted name. Each one must stay one name
// on the server (Nanostack: quoted aliases, see db/UPSTREAM.md).
var hostileNames = []string{
	"a\"b`c",
	`a\`,
	`\" UNION SELECT 99 #`,
	`x\" , 'INJECTED' AS q; #`,
	`\" FROM dual WHERE 1=0 UNION SELECT 42 #`,
	`a'b`,
	`x\', (SELECT @@version), 'k', 1) #`,
}

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// TestHostileAliasStaysOneName runs each hostile alias, and a subquery column that
// refers to it, in the default sql_mode and in each sql_mode that changes how MySQL
// reads a quote. The server must return one row with one column, whose name is the
// alias and whose value is 1.
func TestHostileAliasStaysOneName(t *testing.T) {
	ctx := context.Background()

	for _, mode := range []string{"default", "", "NO_BACKSLASH_ESCAPES", "ANSI_QUOTES"} {
		t.Run("sql_mode="+mode, func(t *testing.T) {
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

			for _, name := range hostileNames {
				sub := SELECT(Int(1).AS(name)).AsTable("sub")
				requireOneNamedValue(t, tx, SELECT(Int(1).AS(name)), name)
				requireOneNamedValue(t, tx, SELECT(IntegerColumn(name).From(sub)).FROM(sub), name)
				requireOneJSONKey(t, tx, SELECT_JSON_OBJ(Int(1).AS(name)), name)
			}
		})
	}
}

func queryString(t *testing.T, q queryer, query string) string {
	t.Helper()

	rows, err := q.QueryContext(context.Background(), query)
	require.NoError(t, err)
	defer rows.Close()

	var value string
	require.True(t, rows.Next())
	require.NoError(t, rows.Scan(&value))
	require.NoError(t, rows.Err())

	return value
}

func requireOneNamedValue(t *testing.T, q queryer, stmt Statement, name string) {
	t.Helper()

	query, args := stmt.Sql()
	rows, err := q.QueryContext(context.Background(), query, args...)
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

// requireOneJSONKey checks that the statement returns one JSON object, whose only key is
// the alias and whose value is 1.
func requireOneJSONKey(t *testing.T, q queryer, stmt Statement, name string) {
	t.Helper()

	query, args := stmt.Sql()
	rows, err := q.QueryContext(context.Background(), query, args...)
	require.NoError(t, err, query)
	defer rows.Close()

	var objects []map[string]int64
	for rows.Next() {
		var raw []byte
		require.NoError(t, rows.Scan(&raw))
		var object map[string]int64
		require.NoError(t, json.Unmarshal(raw, &object), string(raw))
		objects = append(objects, object)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []map[string]int64{{name: 1}}, objects, query)
}
