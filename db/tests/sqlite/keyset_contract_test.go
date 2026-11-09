// This file is Nanostack-only (not in go-jet): the db/keyset contract test on
// SQLite. See db/UPSTREAM.md and db/tests/internal/keysetcontract.

package sqlite

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/db/keyset"
	"github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/sqlite"
	"github.com/nanostack-dev/nanostack-framework/db/tests/internal/keysetcontract"
)

func TestKeysetContract(t *testing.T) {
	// A database of its own, so the contract never writes to the shared test
	// databases.
	conn, err := sql.Open("sqlite3", "file:"+filepath.Join(t.TempDir(), "keyset.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	keysetcontract.Run(t, keysetcontract.Config{
		Flavor: keysetcontract.SQLite,
		DB:     conn,
		NewTable: func(schema, name string, columns ...postgres.Column) keyset.Source {
			return sqlite.NewTable(schema, name, "", columns...)
		},
		Text: sqlite.String,
		Subquery: func(
			table keyset.Source, projection postgres.Projection, where postgres.BoolExpression,
		) postgres.Expression {
			children, ok := table.(sqlite.Table)
			require.True(t, ok)
			return children.SELECT(projection).WHERE(where)
		},
	})
}
