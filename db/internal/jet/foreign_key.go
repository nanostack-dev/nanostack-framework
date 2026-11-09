package jet

// ForeignKey is one foreign key of a table, as nanostack gen db writes it in the
// ForeignKeys method of a generated table type. The generator reads that
// method from source to find the joins that a key declares; nothing reads it
// at run time.
type ForeignKey struct {
	// Name is the name of the constraint. It is empty on SQLite, which keeps
	// no name for a foreign key.
	Name string
	// Columns are the referencing columns of the table, in key order.
	Columns []string
	// References is the name of the referenced table.
	References string
	// ReferencedColumns are the referenced columns, in the order of Columns.
	ReferencedColumns []string
	// ReferencedSchema is the schema of the referenced table (the database
	// on MySQL) when it is not the schema of the table. It is empty when both
	// tables are in the same schema.
	ReferencedSchema string
}
