package metadata

import (
	"cmp"
	"slices"
)

// SetForeignKeys sets the ForeignKeys of each table from keys, by table name,
// sorted by constraint name, byte by byte, so the order does not depend on the
// collation of the server. The sort is stable: keys with no name (SQLite)
// keep the order of the catalogue. It marks each table with ForeignKeysRead.
func SetForeignKeys(tables []Table, keys map[string][]ForeignKey) {
	for i := range tables {
		fks := slices.Clone(keys[tables[i].Name])
		slices.SortStableFunc(fks, func(a, b ForeignKey) int { return cmp.Compare(a.Name, b.Name) })
		if fks == nil {
			fks = []ForeignKey{}
		}
		tables[i].ForeignKeys = fks
		tables[i].ForeignKeysRead = true
	}
}

// SetCatalogTypes sets the CatalogType of each column of each table from
// types, by table name and column name. It marks each table with
// CatalogTypesRead.
func SetCatalogTypes(tables []Table, types map[string]map[string]string) {
	for i := range tables {
		table := &tables[i]
		table.CatalogTypesRead = true
		for j := range table.Columns {
			column := &table.Columns[j]
			column.CatalogType = types[table.Name][column.Name]
		}
	}
}
