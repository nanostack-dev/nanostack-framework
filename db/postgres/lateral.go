// Modified by Nanostack from go-jet (https://github.com/go-jet/jet), commit d0e152c.
// See db/UPSTREAM.md for the upstream base and the list of changes.

package postgres

import "github.com/nanostack-dev/nanostack-framework/db/internal/jet"

// LATERAL derived tables constructor from select statement
func LATERAL(selectStmt SelectStatement) lateralImpl {
	return lateralImpl{
		selectStmt: selectStmt,
	}
}

type lateralImpl struct {
	selectStmt SelectStatement
}

func (l lateralImpl) AS(alias string) SelectTable {
	subQuery := &selectTableImpl{
		SelectTable: jet.NewLateral(l.selectStmt, alias),
	}

	subQuery.readableTableInterfaceImpl.root = subQuery

	return subQuery
}
