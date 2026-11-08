package metadata

// UniqueIndex is one unique index of a table: its primary key, a unique
// constraint or a unique index. postgres.ReadSchema reads them for each base
// table; GetSchema and the other dialects leave Table.UniqueIndexes empty.
type UniqueIndex struct {
	// Name is the name of the index, which is also the name of the
	// constraint that owns it.
	Name string
	// Columns are the key columns in index order, without INCLUDE columns.
	// An expression key is the empty string.
	Columns []string
	// Predicate is the WHERE clause of a partial index, as pg_get_expr
	// prints it, such as "(external_id IS NOT NULL)". Empty for a full index.
	Predicate string
	// NotKey says why equality on Columns can still match more than one
	// row, such as "expression key". Empty when the index proves that it
	// matches at most one.
	NotKey string
}
