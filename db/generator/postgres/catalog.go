package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/nanostack-dev/nanostack-framework/db/generator/metadata"
)

// catalogTypeQuery reads the type name of every column of the base tables of
// schema $1, as information_schema.columns.udt_name defines it: the base type
// of a domain, else the type itself (the array type of an array).
const catalogTypeQuery = `
SELECT c.relname, a.attname, COALESCE(bt.typname, t.typname)
FROM pg_catalog.pg_attribute a
JOIN pg_catalog.pg_class c ON c.oid = a.attrelid
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
JOIN pg_catalog.pg_type t ON t.oid = a.atttypid
LEFT JOIN pg_catalog.pg_type bt ON t.typtype = 'd' AND bt.oid = t.typbasetype
WHERE n.nspname = $1 AND c.relkind IN ('r', 'p') AND a.attnum > 0 AND NOT a.attisdropped`

// addCatalogTypes sets the CatalogType of each column of each table.
func addCatalogTypes(ctx context.Context, db *sql.DB, schemaName string, tables []metadata.Table) error {
	rows, err := db.QueryContext(ctx, catalogTypeQuery, schemaName)
	if err != nil {
		return fmt.Errorf("failed to query column types of schema '%s': %w", schemaName, err)
	}
	defer func() { _ = rows.Close() }()
	types := map[string]map[string]string{}
	for rows.Next() {
		var table, column, typ string
		if err = rows.Scan(&table, &column, &typ); err != nil {
			return fmt.Errorf("failed to scan a column type of schema '%s': %w", schemaName, err)
		}
		if types[table] == nil {
			types[table] = map[string]string{}
		}
		types[table][column] = typ
	}
	if err = rows.Err(); err != nil {
		return fmt.Errorf("failed to read column types of schema '%s': %w", schemaName, err)
	}
	metadata.SetCatalogTypes(tables, types)
	return nil
}

// foreignKeyQuery reads every foreign key of the base tables of schema $1,
// with the referencing and the referenced columns in key order. A key that
// references a partitioned table also has one copy per partition on the
// same referencing table (conparentid is the key); the query leaves those
// copies out. The copy that a partition inherits from its parent's key is on
// another table, so it stays.
const foreignKeyQuery = `
SELECT c.relname, con.conname, rn.nspname, rc.relname,
       to_json(ARRAY(SELECT a.attname
                     FROM unnest(con.conkey) WITH ORDINALITY AS k(attnum, ord)
                     JOIN pg_catalog.pg_attribute a ON a.attrelid = con.conrelid AND a.attnum = k.attnum
                     ORDER BY k.ord))::text,
       to_json(ARRAY(SELECT a.attname
                     FROM unnest(con.confkey) WITH ORDINALITY AS k(attnum, ord)
                     JOIN pg_catalog.pg_attribute a ON a.attrelid = con.confrelid AND a.attnum = k.attnum
                     ORDER BY k.ord))::text
FROM pg_catalog.pg_constraint con
JOIN pg_catalog.pg_class c ON c.oid = con.conrelid
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
JOIN pg_catalog.pg_class rc ON rc.oid = con.confrelid
JOIN pg_catalog.pg_namespace rn ON rn.oid = rc.relnamespace
WHERE n.nspname = $1 AND c.relkind IN ('r', 'p') AND con.contype = 'f'
  AND (con.conparentid = 0
       OR (SELECT p.conrelid FROM pg_catalog.pg_constraint p WHERE p.oid = con.conparentid) <> con.conrelid)`

// addForeignKeys sets the ForeignKeys of each table.
func addForeignKeys(ctx context.Context, db *sql.DB, schemaName string, tables []metadata.Table) error {
	rows, err := db.QueryContext(ctx, foreignKeyQuery, schemaName)
	if err != nil {
		return fmt.Errorf("failed to query foreign keys of schema '%s': %w", schemaName, err)
	}
	defer func() { _ = rows.Close() }()
	keys := map[string][]metadata.ForeignKey{}
	for rows.Next() {
		var table, columns, referenced string
		var fk metadata.ForeignKey
		if err = rows.Scan(&table, &fk.Name, &fk.ReferencedSchema, &fk.ReferencedTable, &columns,
			&referenced); err != nil {
			return fmt.Errorf("failed to scan a foreign key of schema '%s': %w", schemaName, err)
		}
		if err = json.Unmarshal([]byte(columns), &fk.Columns); err != nil {
			return fmt.Errorf("columns of foreign key %s: %w", fk.Name, err)
		}
		if err = json.Unmarshal([]byte(referenced), &fk.ReferencedColumns); err != nil {
			return fmt.Errorf("referenced columns of foreign key %s: %w", fk.Name, err)
		}
		keys[table] = append(keys[table], fk)
	}
	if err = rows.Err(); err != nil {
		return fmt.Errorf("failed to read foreign keys of schema '%s': %w", schemaName, err)
	}
	metadata.SetForeignKeys(tables, keys)
	return nil
}
