// This file is Nanostack-only (not in go-jet): the db/searchexec suite on SQLite. See
// db/UPSTREAM.md and db/tests/internal/searchexeccontract.

package sqlite

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/db/keyset"
	"github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/sqlite"
	"github.com/nanostack-dev/nanostack-framework/db/tests/internal/searchexeccontract"
)

func TestSearchexecContract(t *testing.T) {
	// A database of its own, so the suite never writes to the shared test databases.
	conn, err := sql.Open("sqlite3", "file:"+filepath.Join(t.TempDir(), "searchexec.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	searchexeccontract.Run(t, searchexeccontract.Config{
		Flavor: searchexeccontract.SQLite,
		DB:     conn,
		NewTable: func(schema, name string, columns ...postgres.Column) keyset.Source {
			return sqlite.NewTable(schema, name, "", columns...)
		},
		InnerJoin: func(left, right keyset.Source, on postgres.BoolExpression) keyset.Source {
			l, lok := left.(sqlite.Table)
			r, rok := right.(sqlite.Table)
			require.True(t, lok && rok)
			return l.INNER_JOIN(r, on)
		},
	})
}
