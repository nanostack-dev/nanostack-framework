package metadata

// Two facts from the catalogue are for nano gen db and nano gen repo:
// Column.CatalogType and Table.ForeignKeys (Nanostack).
//
// Only the ReadSchema function of each dialect (postgres, mysql and sqlite)
// fills both for each base table, and sets Table.CatalogTypesRead and
// Table.ForeignKeysRead. GetSchema, GenerateDB and the jet command leave them
// empty, so their metadata and output stay upstream's. The ColumnTypes and
// ForeignKeys templates refuse a table that does not have the marks.
//
// Column.CatalogType is the type name of a column as the catalogue gives it:
// pg_type.typname on PostgreSQL, with these changes: a domain column has the
// type at the end of its chain of domains, and an array of a domain has the
// array type of that base type ("_text"); the name has its schema
// ("billing.status") when the type is not in pg_catalog and not in the schema
// of the table, or when the type is in the schema of the table and has the
// name of a pg_catalog type. information_schema.columns.COLUMN_TYPE on
// MySQL and MariaDB (such as "varchar(255)", "tinyint(1)" or "enum('a','b')");
// and the declared type on SQLite (such as "VARCHAR(20)", or "" for a column
// declared without a type). DataType.Name is not the same: go-jet maps it for
// its own use.

// ForeignKey is one foreign key of a base table.
type ForeignKey struct {
	// Name is the name of the constraint. It is empty on SQLite, which keeps
	// no name for a foreign key.
	Name string
	// Columns are the referencing columns, in key order.
	Columns []string
	// ReferencedSchema is the schema of the referenced table: the database on
	// MySQL, and empty on SQLite.
	ReferencedSchema string
	// ReferencedTable is the name of the referenced table.
	ReferencedTable string
	// ReferencedColumns are the referenced columns, in the order of Columns.
	ReferencedColumns []string
}
