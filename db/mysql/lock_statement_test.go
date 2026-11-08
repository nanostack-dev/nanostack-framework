// Modified by Nanostack from go-jet (https://github.com/go-jet/jet), commit d0e152c.
// See db/UPSTREAM.md for the upstream base and the list of changes.

package mysql

import "testing"

func TestLockRead(t *testing.T) {
	assertStatementSql(t, table2.LOCK().READ(), `
LOCK TABLES db.table2 READ;
`)
}

func TestLockWrite(t *testing.T) {
	assertStatementSql(t, table2.LOCK().WRITE(), `
LOCK TABLES db.table2 WRITE;
`)
}

func TestUNLOCK_TABLES(t *testing.T) {
	assertStatementSql(t, UNLOCK_TABLES(), `
UNLOCK TABLES;
`)
}
