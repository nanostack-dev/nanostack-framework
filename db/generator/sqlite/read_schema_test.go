package sqlite

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/db/generator/metadata"
)

// openDB returns a new database file with the tables of ddl.
func openDB(t *testing.T, ddl string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", "file:"+filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(ddl)
	require.NoError(t, err)
	return db
}

func tableKeys(t *testing.T, schema metadata.Schema, name string) []metadata.ForeignKey {
	t.Helper()
	for _, table := range schema.TablesMetaData {
		if table.Name == name {
			return table.ForeignKeys
		}
	}
	t.Fatalf("no table %s", name)
	return nil
}

// pragma_foreign_key_list gives the referenced table and columns as the
// REFERENCES clause writes them. ReadSchema gives the names of the table and
// of its columns, also for a key that references the primary key, whose
// columns are in key order (not in column order).
func TestReadSchemaForeignKeyNames(t *testing.T) {
	t.Parallel()
	db := openDB(t, `
CREATE TABLE Album (AlbumId INTEGER PRIMARY KEY, Title TEXT);
CREATE TABLE pairs (x INTEGER, y INTEGER, PRIMARY KEY (y, x));
CREATE TABLE track (
    id       INTEGER PRIMARY KEY,
    album_id INTEGER REFERENCES album(albumid),
    album2   INTEGER REFERENCES ALBUM,
    py       INTEGER,
    px       INTEGER,
    FOREIGN KEY (py, px) REFERENCES PAIRS
);`)
	schema, err := ReadSchema(db)
	require.NoError(t, err)
	require.Equal(t, []metadata.ForeignKey{
		{Columns: []string{"album_id"}, ReferencedTable: "Album", ReferencedColumns: []string{"AlbumId"}},
		{Columns: []string{"album2"}, ReferencedTable: "Album", ReferencedColumns: []string{"AlbumId"}},
		{Columns: []string{"py", "px"}, ReferencedTable: "pairs", ReferencedColumns: []string{"y", "x"}},
	}, sortedByColumn(tableKeys(t, schema, "track")))
	require.Equal(t, []metadata.ForeignKey{}, tableKeys(t, schema, "Album"))
}

// sortedByColumn orders keys by their first column, as pragma_foreign_key_list
// gives the keys in no documented order.
func sortedByColumn(fks []metadata.ForeignKey) []metadata.ForeignKey {
	order := map[string]int{"album_id": 0, "album2": 1, "py": 2}
	out := make([]metadata.ForeignKey, len(fks))
	for _, fk := range fks {
		out[order[fk.Columns[0]]] = fk
	}
	return out
}

// SQLite accepts a key to a table or a column that does not exist. ReadSchema
// refuses it with an error that names the key, for both forms of the key.
func TestReadSchemaForeignKeyToMissing(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ ddl, want string }{
		{`CREATE TABLE posts (id INTEGER PRIMARY KEY, x INTEGER REFERENCES missing);`,
			"a foreign key of table 'posts' references table 'missing', which does not exist"},
		{`CREATE TABLE posts (id INTEGER PRIMARY KEY, x INTEGER REFERENCES missing(id));`,
			"a foreign key of table 'posts' references table 'missing', which does not exist"},
		{`CREATE TABLE users (id INTEGER PRIMARY KEY);
CREATE TABLE posts (id INTEGER PRIMARY KEY, x INTEGER REFERENCES users(nope));`,
			"a foreign key of table 'posts' references column 'nope' of table 'users', which does not exist"},
	} {
		_, err := ReadSchema(openDB(t, tc.ddl))
		require.ErrorContains(t, err, tc.want)
	}
}
