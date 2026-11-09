package metadata

// Two facts from the catalogue are for nano gen db and nano gen repo:
// Column.CatalogType and Table.ForeignKeys (Nanostack).
//
// The ReadSchema function of each dialect (postgres, mysql and sqlite) fills
// both for each base table. GetSchema, GenerateDB and the jet command leave
// them empty, so their metadata and output stay upstream's.
//
// Column.CatalogType is the type name of a column as the catalogue gives it:
// pg_type.typname on PostgreSQL, as information_schema.columns.udt_name gives
// it (a domain column has the type of its base, an array column the name of
// its array type, such as "_text"); information_schema.columns.COLUMN_TYPE on
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
