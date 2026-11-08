// Modified by Nanostack from go-jet (https://github.com/go-jet/jet), commit d0e152c.
// See db/UPSTREAM.md for the upstream base and the list of changes.

package mysql

import (
	"github.com/nanostack-dev/nanostack-framework/db/internal/testutils"
	. "github.com/nanostack-dev/nanostack-framework/db/mysql"
	. "github.com/nanostack-dev/nanostack-framework/db/tests/.gentestdata/mysql/dvds/table"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestLockRead(t *testing.T) {
	query := Customer.LOCK().READ()

	testutils.AssertStatementSql(t, query, `
LOCK TABLES dvds.customer READ;
`)
	tx, err := db.DB.Begin() // can't prepare LOCK statement
	require.NoError(t, err)
	defer func() {
		// Nanostack: LOCK TABLES is not transactional, so Rollback alone leaves the lock on the
		// pooled connection and later tests fail with "was not locked with LOCK TABLES".
		_, err := tx.Exec("UNLOCK TABLES")
		require.NoError(t, err)
		err = tx.Rollback()
		require.NoError(t, err)
	}()

	testutils.AssertExec(t, query, tx)
}

func TestLockWrite(t *testing.T) {
	query := Customer.LOCK().WRITE()

	testutils.AssertStatementSql(t, query, `
LOCK TABLES dvds.customer WRITE;
`)

	tx, err := db.DB.Begin() // can't prepare LOCK statement
	require.NoError(t, err)
	defer func() {
		// Nanostack: LOCK TABLES is not transactional, so Rollback alone leaves the lock on the
		// pooled connection and later tests fail with "was not locked with LOCK TABLES".
		_, err := tx.Exec("UNLOCK TABLES")
		require.NoError(t, err)
		err = tx.Rollback()
		require.NoError(t, err)
	}()

	testutils.AssertExec(t, query, tx)
}

func TestUnlockTables(t *testing.T) {
	query := UNLOCK_TABLES()

	testutils.AssertStatementSql(t, query, `
UNLOCK TABLES;
`)

	tx, err := db.DB.Begin() // can't prepare LOCK statement
	require.NoError(t, err)
	defer func() {
		err := tx.Rollback()
		require.NoError(t, err)
	}()

	testutils.AssertExec(t, query, tx)
}
