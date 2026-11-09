package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/nanostack-dev/nanostack-framework/db/generator/metadata"
)

// ReadSchema reads the metadata of the main database of db, as GenerateDB
// does, and prints nothing. Each table also carries its foreign keys and the
// declared type of each column, which GenerateDB does not read. Pass the result to template.Render to generate
// the files in memory.
func ReadSchema(db *sql.DB) (metadata.Schema, error) {
	schema, err := metadata.ReadSchema(db, &sqliteQuerySet{}, "")
	if err != nil {
		return metadata.Schema{}, fmt.Errorf("failed to query database metadata: %w", err)
	}
	ctx := context.Background()
	keys := map[string][]metadata.ForeignKey{}
	types := map[string]map[string]string{}
	for _, table := range schema.TablesMetaData {
		fks, fkErr := foreignKeys(ctx, db, table.Name)
		if fkErr != nil {
			return metadata.Schema{}, fkErr
		}
		keys[table.Name] = fks
		if types[table.Name], err = declaredTypes(ctx, db, table.Name); err != nil {
			return metadata.Schema{}, err
		}
	}
	metadata.SetForeignKeys(schema.TablesMetaData, keys)
	metadata.SetCatalogTypes(schema.TablesMetaData, types)
	return schema, nil
}

// foreignKeys returns the foreign keys of table in the order of
// pragma_foreign_key_list, with their columns in key order. SQLite keeps no
// name for a foreign key, so Name is empty. A key declared without its
// referenced columns references the primary key of the other table, and its
// ReferencedColumns are the columns of that primary key.
func foreignKeys(ctx context.Context, db *sql.DB, table string) ([]metadata.ForeignKey, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, "table", "from", "to" FROM pragma_foreign_key_list(?) ORDER BY id, seq`, table)
	if err != nil {
		return nil, fmt.Errorf("failed to query foreign keys of table '%s': %w", table, err)
	}
	defer func() { _ = rows.Close() }()
	var fks []metadata.ForeignKey
	lastID := -1
	implicit := map[int]bool{} // index in fks -> the key names no referenced column
	for rows.Next() {
		var id int
		var refTable, column string
		var refColumn sql.NullString
		if err = rows.Scan(&id, &refTable, &column, &refColumn); err != nil {
			return nil, fmt.Errorf("failed to scan a foreign key of table '%s': %w", table, err)
		}
		if id != lastID {
			fks = append(fks, metadata.ForeignKey{ReferencedTable: refTable})
			lastID = id
		}
		i := len(fks) - 1
		fks[i].Columns = append(fks[i].Columns, column)
		if refColumn.Valid {
			fks[i].ReferencedColumns = append(fks[i].ReferencedColumns, refColumn.String)
		} else {
			implicit[i] = true
		}
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read foreign keys of table '%s': %w", table, err)
	}
	for i := range implicit {
		pk, pkErr := primaryKey(ctx, db, fks[i].ReferencedTable)
		if pkErr != nil {
			return nil, pkErr
		}
		if len(pk) != len(fks[i].Columns) {
			return nil, fmt.Errorf("a foreign key of table '%s' has %d columns and references the primary key "+
				"of table '%s', which has %d", table, len(fks[i].Columns), fks[i].ReferencedTable, len(pk))
		}
		fks[i].ReferencedColumns = pk
	}
	return fks, nil
}

// primaryKey returns the primary key columns of table, in key order.
func primaryKey(ctx context.Context, db *sql.DB, table string) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT name FROM pragma_table_info(?) WHERE pk > 0 ORDER BY pk`, table)
	if err != nil {
		return nil, fmt.Errorf("failed to query the primary key of table '%s': %w", table, err)
	}
	defer func() { _ = rows.Close() }()
	var pk []string
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("failed to scan the primary key of table '%s': %w", table, err)
		}
		pk = append(pk, name)
	}
	return pk, rows.Err()
}

// declaredTypes returns the declared type of each column of table, by
// column name, as pragma_table_xinfo gives it, without the " GENERATED
// ALWAYS" that it adds to a generated column (go-jet's column query reads
// the same pragma).
func declaredTypes(ctx context.Context, db *sql.DB, table string) (map[string]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT name, type FROM pragma_table_xinfo(?)`, table)
	if err != nil {
		return nil, fmt.Errorf("failed to query column types of table '%s': %w", table, err)
	}
	defer func() { _ = rows.Close() }()
	types := map[string]string{}
	for rows.Next() {
		var name, typ string
		if err = rows.Scan(&name, &typ); err != nil {
			return nil, fmt.Errorf("failed to scan a column type of table '%s': %w", table, err)
		}
		types[name] = strings.TrimSuffix(typ, " GENERATED ALWAYS")
	}
	return types, rows.Err()
}
