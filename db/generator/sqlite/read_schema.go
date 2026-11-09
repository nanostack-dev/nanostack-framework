package sqlite

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
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
//
// pragma_foreign_key_list gives the referenced table and columns as the
// REFERENCES clause writes them, and SQLite compares these names without
// case. ReferencedTable and ReferencedColumns are the names of the table and
// of its columns, so they match the table files and ColumnTypes. A key to a
// table or a column that does not exist is an error.
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
	_ = rows.Close()
	for i := range fks {
		if err = resolveReference(ctx, db, table, &fks[i], implicit[i]); err != nil {
			return nil, err
		}
	}
	return fks, nil
}

// resolveReference sets the ReferencedTable and the ReferencedColumns of fk
// to the names of the table and of its columns. When implicit, the key names
// no referenced column, and references the primary key.
func resolveReference(ctx context.Context, db *sql.DB, table string, fk *metadata.ForeignKey, implicit bool) error {
	var name string
	err := db.QueryRowContext(ctx,
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ? COLLATE NOCASE`,
		fk.ReferencedTable).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("a foreign key of table '%s' references table '%s', which does not exist",
			table, fk.ReferencedTable)
	}
	if err != nil {
		return fmt.Errorf("failed to find table '%s', which table '%s' references: %w", fk.ReferencedTable,
			table, err)
	}
	columns, pk, err := tableColumns(ctx, db, name)
	if err != nil {
		return err
	}
	fk.ReferencedTable = name
	if implicit {
		if len(pk) != len(fk.Columns) {
			return fmt.Errorf("a foreign key of table '%s' has %d columns and references the primary key "+
				"of table '%s', which has %d", table, len(fk.Columns), name, len(pk))
		}
		fk.ReferencedColumns = pk
		return nil
	}
	for j, ref := range fk.ReferencedColumns {
		k := slices.IndexFunc(columns, func(c string) bool { return strings.EqualFold(c, ref) })
		if k < 0 {
			return fmt.Errorf("a foreign key of table '%s' references column '%s' of table '%s', "+
				"which does not exist", table, ref, name)
		}
		fk.ReferencedColumns[j] = columns[k]
	}
	return nil
}

// tableColumns returns the columns of table in column order, and its primary
// key columns in key order.
func tableColumns(ctx context.Context, db *sql.DB, table string) (columns, pk []string, err error) {
	rows, err := db.QueryContext(ctx, `SELECT name, pk FROM pragma_table_info(?) ORDER BY cid`, table)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query the columns of table '%s': %w", table, err)
	}
	defer func() { _ = rows.Close() }()
	type keyColumn struct {
		name string
		pos  int
	}
	var keys []keyColumn
	for rows.Next() {
		var name string
		var pos int
		if err = rows.Scan(&name, &pos); err != nil {
			return nil, nil, fmt.Errorf("failed to scan a column of table '%s': %w", table, err)
		}
		columns = append(columns, name)
		if pos > 0 {
			keys = append(keys, keyColumn{name, pos})
		}
	}
	if err = rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("failed to read the columns of table '%s': %w", table, err)
	}
	slices.SortFunc(keys, func(a, b keyColumn) int { return cmp.Compare(a.pos, b.pos) })
	for _, k := range keys {
		pk = append(pk, k.name)
	}
	return columns, pk, nil
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
