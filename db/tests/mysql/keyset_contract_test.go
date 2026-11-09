// This file is Nanostack-only (not in go-jet): the db/keyset contract test on
// MySQL and MariaDB. See db/UPSTREAM.md and db/tests/internal/keysetcontract.

package mysql

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/db/keyset"
	"github.com/nanostack-dev/nanostack-framework/db/mysql"
	"github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/tests/dbconfig"
	"github.com/nanostack-dev/nanostack-framework/db/tests/internal/keysetcontract"
)

func TestKeysetContract(t *testing.T) {
	// The contract reads times as time.Time, in a UTC session: a TIMESTAMP is
	// read and compared in the session time zone.
	dsn := dbconfig.MySQLConnectionString(sourceIsMariaDB(), "test_sample") +
		"?parseTime=true&loc=UTC&time_zone=%27%2B00%3A00%27"
	strict := openKeysetDB(t, dsn)
	unstrict := openKeysetDB(t, dsn+"&sql_mode=%27%27")
	flavor := keysetcontract.MySQL
	if sourceIsMariaDB() {
		flavor = keysetcontract.MariaDB
	}
	keysetcontract.Run(t, keysetcontract.Config{
		Flavor:   flavor,
		DB:       strict,
		Unstrict: unstrict,
		Schema:   "test_sample",
		NewTable: func(schema, name string, columns ...postgres.Column) keyset.Source {
			return mysql.NewTable(schema, name, "", columns...)
		},
		Text: mysql.String,
		Subquery: func(
			table keyset.Source, projection postgres.Projection, where postgres.BoolExpression,
		) postgres.Expression {
			children, ok := table.(mysql.Table)
			require.True(t, ok)
			return children.SELECT(projection).WHERE(where)
		},
	})
}

func openKeysetDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	conn, err := sql.Open("mysql", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}
