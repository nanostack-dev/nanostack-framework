package metadata

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func keyNames(fks []ForeignKey) []string {
	names := make([]string, len(fks))
	for i, fk := range fks {
		names[i] = fk.Name + "/" + fk.ReferencedTable
	}
	return names
}

// The keys are in constraint name order byte by byte, whatever order and
// collation the server used. Keys with no name (SQLite) keep their order.
func TestSetForeignKeysSortsByteWise(t *testing.T) {
	t.Parallel()
	tables := []Table{{Name: "t"}, {Name: "u"}, {Name: "none"}}
	SetForeignKeys(tables, map[string][]ForeignKey{
		"t": {{Name: "b_fk"}, {Name: "B_fk"}, {Name: "a_fk"}, {Name: ""}},
		"u": {{ReferencedTable: "z"}, {ReferencedTable: "a"}, {ReferencedTable: "m"}},
	})
	require.Equal(t, []string{"/", "B_fk/", "a_fk/", "b_fk/"}, keyNames(tables[0].ForeignKeys))
	require.Equal(t, []string{"/z", "/a", "/m"}, keyNames(tables[1].ForeignKeys))
	require.NotNil(t, tables[2].ForeignKeys)
	require.Empty(t, tables[2].ForeignKeys)
	for _, table := range tables {
		require.True(t, table.ForeignKeysRead, table.Name)
	}
}

func TestSetCatalogTypesMarksTables(t *testing.T) {
	t.Parallel()
	tables := []Table{{Name: "t", Columns: []Column{{Name: "a"}}}}
	SetCatalogTypes(tables, map[string]map[string]string{"t": {"a": "text"}})
	require.True(t, tables[0].CatalogTypesRead)
	require.Equal(t, "text", tables[0].Columns[0].CatalogType)
}
