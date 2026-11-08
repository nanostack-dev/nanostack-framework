// Modified by Nanostack from go-jet (https://github.com/go-jet/jet), commit d0e152c.
// See db/UPSTREAM.md for the upstream base and the list of changes.

package metadata

// Table metadata struct
type Table struct {
	Name    string `sql:"primary_key"`
	Comment string
	Columns []Column
	// UniqueIndexes are the unique indexes of a base table, from postgres.ReadSchema (Nanostack, see unique_index.go).
	UniqueIndexes []UniqueIndex
}

// MutableColumns returns list of mutable columns for table
func (t Table) MutableColumns() []Column {
	var ret []Column

	for _, column := range t.Columns {
		if column.IsPrimaryKey || column.IsGenerated {
			continue
		}

		ret = append(ret, column)
	}

	return ret
}

// DefaultColumns returns list of columns with default values set for table
func (t Table) DefaultColumns() []Column {
	var ret []Column

	for _, column := range t.Columns {
		if column.HasDefault {
			ret = append(ret, column)
		}
	}

	return ret
}
