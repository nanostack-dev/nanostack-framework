package metadata

import (
	"cmp"
	"slices"
)

// SetForeignKeys sets the ForeignKeys of each table from keys, by table name,
// sorted by constraint name, byte by byte, so the order does not depend on the
// collation of the server. The sort is stable: keys with no name (SQLite)
// keep the order of the catalogue.
func SetForeignKeys(tables []Table, keys map[string][]ForeignKey) {
	for i := range tables {
		fks := keys[tables[i].Name]
		slices.SortStableFunc(fks, func(a, b ForeignKey) int { return cmp.Compare(a.Name, b.Name) })
		tables[i].ForeignKeys = fks
	}
}

// SetCatalogTypes sets the CatalogType of each column of each table from
// types, by table name and column name.
func SetCatalogTypes(tables []Table, types map[string]map[string]string) {
	for i := range tables {
		for j := range tables[i].Columns {
			tables[i].Columns[j].CatalogType = types[tables[i].Name][tables[i].Columns[j].Name]
		}
	}
}
