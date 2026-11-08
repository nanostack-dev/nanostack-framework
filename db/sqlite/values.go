// Modified by Nanostack from go-jet (https://github.com/go-jet/jet), commit d0e152c.
// See db/UPSTREAM.md for the upstream base and the list of changes.

package sqlite

import "github.com/nanostack-dev/nanostack-framework/db/internal/jet"

type values struct {
	jet.Values
}

// VALUES is a table value constructor that computes a set of one or more rows as a temporary constant table.
// Each row is defined by the ROW constructor, which takes one or more expressions.
//
// Example usage:
//
//	VALUES(
//		ROW(Int32(204), Float32(1.21)),
//		ROW(Int32(207), Float32(1.02)),
//	)
func VALUES(rows ...RowExpression) values {
	return values{Values: jet.Values(rows)}
}

// AS assigns an alias to the temporary VALUES table, allowing it to be referenced
// within SQL FROM clauses, just like a regular table.
func (v values) AS(alias string) SelectTable {
	return newSelectTable(v, alias, nil)
}
