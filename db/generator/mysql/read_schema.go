package mysql

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/nanostack-dev/nanostack-framework/db/generator/metadata"
)

// ReadSchema reads the metadata of database dbName from db, as GenerateDB
// does, and prints nothing. Each base table also carries its foreign keys and
// the COLUMN_TYPE of each column, which GenerateDB does not read. Pass the result to template.Render to
// generate the files in memory. It works on MySQL and MariaDB.
func ReadSchema(db *sql.DB, dbName string) (metadata.Schema, error) {
	schema, err := metadata.ReadSchema(db, &mySqlQuerySet{}, dbName)
	if err != nil {
		return metadata.Schema{}, fmt.Errorf("failed to get '%s' schema metadata: %w", dbName, err)
	}
	ctx := context.Background()
	if err = addCatalogTypes(ctx, db, dbName, schema.TablesMetaData); err != nil {
		return metadata.Schema{}, err
	}
	if err = addForeignKeys(ctx, db, dbName, schema.TablesMetaData); err != nil {
		return metadata.Schema{}, err
	}
	return schema, nil
}

// addCatalogTypes sets the CatalogType of each column of each table:
// information_schema.COLUMNS.COLUMN_TYPE, which keeps "tinyint(1)" and
// "enum('a','b')" where DATA_TYPE drops them.
func addCatalogTypes(ctx context.Context, db *sql.DB, dbName string, tables []metadata.Table) error {
	rows, err := db.QueryContext(ctx,
		`SELECT TABLE_NAME, COLUMN_NAME, COLUMN_TYPE FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = ?`, dbName)
	if err != nil {
		return fmt.Errorf("failed to query column types of database '%s': %w", dbName, err)
	}
	defer func() { _ = rows.Close() }()
	types := map[string]map[string]string{}
	for rows.Next() {
		var table, column, typ string
		if err = rows.Scan(&table, &column, &typ); err != nil {
			return fmt.Errorf("failed to scan a column type of database '%s': %w", dbName, err)
		}
		if types[table] == nil {
			types[table] = map[string]string{}
		}
		types[table][column] = typ
	}
	if err = rows.Err(); err != nil {
		return fmt.Errorf("failed to read column types of database '%s': %w", dbName, err)
	}
	metadata.SetCatalogTypes(tables, types)
	return nil
}

// foreignKeyQuery reads one row per column of each foreign key of the base
// tables of database ?, in key order. KEY_COLUMN_USAGE has a referenced
// table only on the rows of a foreign key.
const foreignKeyQuery = `
SELECT k.TABLE_NAME, k.CONSTRAINT_NAME, k.REFERENCED_TABLE_SCHEMA, k.REFERENCED_TABLE_NAME,
       k.COLUMN_NAME, k.REFERENCED_COLUMN_NAME
FROM information_schema.KEY_COLUMN_USAGE k
JOIN information_schema.TABLES t ON t.TABLE_SCHEMA = k.TABLE_SCHEMA AND t.TABLE_NAME = k.TABLE_NAME
WHERE k.TABLE_SCHEMA = ? AND t.TABLE_TYPE = 'BASE TABLE' AND k.REFERENCED_TABLE_NAME IS NOT NULL
ORDER BY k.TABLE_NAME, k.CONSTRAINT_NAME, k.ORDINAL_POSITION`

// addForeignKeys sets the ForeignKeys of each table.
func addForeignKeys(ctx context.Context, db *sql.DB, dbName string, tables []metadata.Table) error {
	rows, err := db.QueryContext(ctx, foreignKeyQuery, dbName)
	if err != nil {
		return fmt.Errorf("failed to query foreign keys of database '%s': %w", dbName, err)
	}
	defer func() { _ = rows.Close() }()
	keys := map[string][]metadata.ForeignKey{}
	index := map[[2]string]int{} // table and constraint name -> index in keys[table]
	for rows.Next() {
		var table, name, refSchema, refTable, column, refColumn string
		if err = rows.Scan(&table, &name, &refSchema, &refTable, &column, &refColumn); err != nil {
			return fmt.Errorf("failed to scan a foreign key of database '%s': %w", dbName, err)
		}
		i, seen := index[[2]string{table, name}]
		if !seen {
			keys[table] = append(keys[table],
				metadata.ForeignKey{Name: name, ReferencedSchema: refSchema, ReferencedTable: refTable})
			i = len(keys[table]) - 1
			index[[2]string{table, name}] = i
		}
		fk := &keys[table][i]
		fk.Columns = append(fk.Columns, column)
		fk.ReferencedColumns = append(fk.ReferencedColumns, refColumn)
	}
	if err = rows.Err(); err != nil {
		return fmt.Errorf("failed to read foreign keys of database '%s': %w", dbName, err)
	}
	metadata.SetForeignKeys(tables, keys)
	return nil
}
