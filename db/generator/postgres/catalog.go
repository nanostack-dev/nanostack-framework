package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/nanostack-dev/nanostack-framework/db/generator/metadata"
)

// catalogTypeQuery reads the type of every column of the base tables of
// schema $1: the column, the schema and the name of the type, and whether
// pg_catalog has a type of the same name. The type of a domain column is the
// type at the end of its chain of domains (a domain can have a domain as its
// base). The type of an array of a domain is the array type of that base
// type. information_schema.columns.udt_name resolves one level of domain only,
// and does not resolve the element of an array.
const catalogTypeQuery = `
WITH RECURSIVE chain(domain, base) AS (
    SELECT t.oid, t.typbasetype FROM pg_catalog.pg_type t WHERE t.typtype = 'd'
    UNION ALL
    SELECT ch.domain, t.typbasetype
    FROM chain ch JOIN pg_catalog.pg_type t ON t.oid = ch.base AND t.typtype = 'd'),
resolved(domain, base) AS (
    SELECT ch.domain, ch.base
    FROM chain ch JOIN pg_catalog.pg_type t ON t.oid = ch.base AND t.typtype <> 'd')
SELECT c.relname, a.attname, rn.nspname, rt.typname,
       EXISTS (SELECT 1 FROM pg_catalog.pg_type p
               WHERE p.typnamespace = 'pg_catalog'::regnamespace AND p.typname = rt.typname)
FROM pg_catalog.pg_attribute a
JOIN pg_catalog.pg_class c ON c.oid = a.attrelid
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
JOIN pg_catalog.pg_type t ON t.oid = a.atttypid
LEFT JOIN resolved d ON d.domain = t.oid
LEFT JOIN pg_catalog.pg_type et ON t.typcategory = 'A' AND et.oid = t.typelem AND et.typtype = 'd'
LEFT JOIN resolved ed ON ed.domain = et.oid
LEFT JOIN pg_catalog.pg_type ebt ON ebt.oid = ed.base
JOIN pg_catalog.pg_type rt ON rt.oid = COALESCE(d.base, NULLIF(ebt.typarray, 0), t.oid)
JOIN pg_catalog.pg_namespace rn ON rn.oid = rt.typnamespace
WHERE n.nspname = $1 AND c.relkind IN ('r', 'p') AND a.attnum > 0 AND NOT a.attisdropped`

// addCatalogTypes sets the CatalogType of each column of each table. The
// name of a type has its schema ("billing.status") when the type is not in
// pg_catalog and not in schemaName, so two types of the same name in two
// schemas never get the same value. A type of schemaName that has the name
// of a pg_catalog type ("public.path") has its schema too, so it never looks
// like the pg_catalog type.
func addCatalogTypes(ctx context.Context, db *sql.DB, schemaName string, tables []metadata.Table) error {
	rows, err := db.QueryContext(ctx, catalogTypeQuery, schemaName)
	if err != nil {
		return fmt.Errorf("failed to query column types of schema '%s': %w", schemaName, err)
	}
	defer func() { _ = rows.Close() }()
	types := map[string]map[string]string{}
	for rows.Next() {
		var table, column, typeSchema, typ string
		var shadows bool
		if err = rows.Scan(&table, &column, &typeSchema, &typ, &shadows); err != nil {
			return fmt.Errorf("failed to scan a column type of schema '%s': %w", schemaName, err)
		}
		if types[table] == nil {
			types[table] = map[string]string{}
		}
		types[table][column] = qualifiedTypeName(schemaName, typeSchema, typ, shadows)
	}
	if err = rows.Err(); err != nil {
		return fmt.Errorf("failed to read column types of schema '%s': %w", schemaName, err)
	}
	metadata.SetCatalogTypes(tables, types)
	return nil
}

// qualifiedTypeName returns the catalogue type name of a column of a table
// of schema: typeName when the type is in pg_catalog, or in schema and
// shadowsCatalog is false (pg_catalog has no type of that name); else
// typeSchema.typeName.
func qualifiedTypeName(schema, typeSchema, typeName string, shadowsCatalog bool) string {
	if typeSchema == "pg_catalog" || (typeSchema == schema && !shadowsCatalog) {
		return typeName
	}
	return typeSchema + "." + typeName
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
