// Modified by Nanostack from go-jet (https://github.com/go-jet/jet), commit d0e152c.
// See db/UPSTREAM.md for the upstream base and the list of changes.

package postgres

import "github.com/nanostack-dev/nanostack-framework/db/internal/jet"

// SelectTable is interface for postgres temporary tables like sub-queries, VALUES, CTEs etc...
type SelectTable interface {
	readableTable
	jet.SelectTable
}

type selectTableImpl struct {
	jet.SelectTable
	readableTableInterfaceImpl
}

func newSelectTable(serializerWithProjections jet.SerializerHasProjections, alias string, columnAliases []jet.ColumnExpression) SelectTable {
	subQuery := &selectTableImpl{
		SelectTable: jet.NewSelectTable(serializerWithProjections, alias, columnAliases),
	}

	subQuery.readableTableInterfaceImpl.root = subQuery

	return subQuery
}
