package metadata

import (
	"database/sql"
	"fmt"
)

// ReadSchema retrieves Schema information from the database, as GetSchema
// does, and prints nothing.
func ReadSchema(db *sql.DB, querySet DialectQuerySet, schemaName string) (Schema, error) {
	tablesMetaData, err := querySet.GetTablesMetaData(db, schemaName, BaseTable)
	if err != nil {
		return Schema{}, fmt.Errorf("failed to get %s tables metadata: %w", schemaName, err)
	}

	viewMetaData, err := querySet.GetTablesMetaData(db, schemaName, ViewTable)
	if err != nil {
		return Schema{}, fmt.Errorf("failed to get %s view metadata: %w", schemaName, err)
	}

	enumsMetaData, err := querySet.GetEnumsMetaData(db, schemaName)
	if err != nil {
		return Schema{}, fmt.Errorf("failed to get %s enum metadata: %w", schemaName, err)
	}

	return Schema{
		Name:           schemaName,
		TablesMetaData: tablesMetaData,
		ViewsMetaData:  viewMetaData,
		EnumsMetaData:  enumsMetaData,
	}, nil
}
