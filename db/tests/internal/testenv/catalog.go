package testenv

import (
	"database/sql"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/db/generator/metadata"
	"github.com/nanostack-dev/nanostack-framework/db/generator/mysql"
	"github.com/nanostack-dev/nanostack-framework/db/generator/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/generator/sqlite"
	"github.com/nanostack-dev/nanostack-framework/db/generator/template"
	"github.com/nanostack-dev/nanostack-framework/db/internal/jet"
	mysqldialect "github.com/nanostack-dev/nanostack-framework/db/mysql"
	pgdialect "github.com/nanostack-dev/nanostack-framework/db/postgres"
	sqlitedialect "github.com/nanostack-dev/nanostack-framework/db/sqlite"
	"github.com/nanostack-dev/nanostack-framework/db/tests/dbconfig"
)

// catalogue is what the catalogue of one schema says, by table: each column
// with its type, in column order, and each foreign key, in the order the
// generator writes them. A line is "column type", or "name -> schema.table:
// column=referenced ...", with an empty schema for a key to the same schema.
type catalogue struct {
	columns map[string][]string
	keys    map[string][]string
}

// CheckCatalog reads every schema of the test data of env's source with the
// ReadSchema function of its dialect, renders the table types with
// ColumnTypes and ForeignKeys, parses both methods back from the rendered
// files with go/parser, as nano gen repo reads them, and compares them with
// the catalogue: information_schema on PostgreSQL, MySQL and MariaDB, the
// pragmas on SQLite. Every table must have both methods, and the test data
// of the source must have at least one foreign key. CockroachDB is skipped:
// postgres.ReadSchema needs PostgreSQL.
func CheckCatalog(t *testing.T, env *Env) {
	t.Helper()
	if env == nil {
		t.Fatal("testenv: no environment; run the suite through testenv.Run")
	}
	keys := 0
	defer func() {
		if !t.Failed() && !t.Skipped() && keys == 0 {
			t.Error("the test data has no foreign key, so the check proves nothing")
		}
	}()
	switch {
	case env.Source == Cockroach:
		t.Skip("postgres.ReadSchema needs PostgreSQL")
	case env.Source.IsPostgres():
		db := open(t, "postgres", env.DB.PostgresDSN())
		for _, schema := range pgSchemas {
			t.Run(schema, func(t *testing.T) {
				read, err := postgres.ReadSchema(db, schema)
				keys += checkRendered(t, read, err, pgdialect.Dialect, pgCatalogue(t, db, schema))
			})
		}
	case env.Source == MySQL || env.Source == MariaDB:
		for _, name := range mysqlDatabases {
			t.Run(name, func(t *testing.T) {
				db := open(
					t,
					"mysql",
					fmt.Sprintf("%s:%s@tcp(%s:%d)/%s", user, password, env.DB.Host, env.DB.Port, name),
				)
				read, err := mysql.ReadSchema(db, name)
				keys += checkRendered(t, read, err, mysqldialect.Dialect, mysqlCatalogue(t, db, name))
			})
		}
	case env.Source == SQLite:
		for _, path := range []string{dbconfig.SakilaDBPath, dbconfig.ChinookDBPath, dbconfig.TestSampleDBPath} {
			t.Run(path[strings.LastIndex(path, "/")+1:], func(t *testing.T) {
				db := open(t, "sqlite3", "file:"+path+"?mode=ro")
				read, err := sqlite.ReadSchema(db)
				keys += checkRendered(t, read, err, sqlitedialect.Dialect, sqliteCatalogue(t, db))
			})
		}
	default:
		t.Fatalf("testenv: no catalogue check for %s", env.Source)
	}
}

func open(t *testing.T, driver, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open(driver, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// checkRendered renders schema and compares its ColumnTypes and ForeignKeys
// with want. It returns the number of foreign keys of want.
func checkRendered(t *testing.T, schema metadata.Schema, readErr error, dialect jet.Dialect, want catalogue) int {
	t.Helper()
	if readErr != nil {
		t.Fatal(readErr)
	}
	files, err := template.Render(schema, template.Default(dialect).UseSchema(func(s metadata.Schema) template.Schema {
		return template.DefaultSchema(s).UseSQLBuilder(template.DefaultSQLBuilder().UseTable(
			func(t metadata.Table) template.TableSQLBuilder {
				return template.DefaultTableSQLBuilder(t).UseColumnTypes(true).UseForeignKeys(true)
			}))
	}))
	if err != nil {
		t.Fatal(err)
	}
	got := catalogue{columns: map[string][]string{}, keys: map[string][]string{}}
	for _, f := range files {
		if !strings.Contains("/"+f.Path, "/table/") || strings.HasSuffix(f.Path, "/table_use_schema.go") {
			continue
		}
		table, columns, keys := parseCatalogMethods(t, f.Path, f.Data)
		got.columns[table], got.keys[table] = columns, keys
	}
	if tables := slices.Sorted(maps.Keys(want.columns)); !slices.Equal(slices.Sorted(maps.Keys(got.columns)), tables) {
		t.Fatalf("table files for %v, catalogue tables %v", slices.Sorted(maps.Keys(got.columns)), tables)
	}
	total := 0
	for table, columns := range want.columns {
		if !slices.Equal(got.columns[table], columns) {
			t.Errorf("%s: ColumnTypes %q, catalogue %q", table, got.columns[table], columns)
		}
		keys := want.keys[table]
		total += len(keys)
		if len(got.keys[table]) != len(keys) || (len(keys) > 0 && !slices.Equal(got.keys[table], keys)) {
			t.Errorf("%s: ForeignKeys %q, catalogue %q", table, got.keys[table], keys)
		}
	}
	return total
}

// parseCatalogMethods reads ColumnTypes and ForeignKeys from one rendered
// table file, and the table name from the doc comment of ColumnTypes.
func parseCatalogMethods(t *testing.T, path string, src []byte) (table string, columns, keys []string) {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), path, src, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil {
			continue
		}
		switch fn.Name.Name {
		case "ColumnTypes":
			found++
			doc := strings.Join(strings.Fields(strings.ReplaceAll(fn.Doc.Text(), "\n", " ")), " ")
			rest, _ := strings.CutPrefix(doc, "ColumnTypes lists the catalogue type of each column of ")
			table, _, _ = strings.Cut(rest, ",")
			for _, elt := range returnedLiteral(t, path, fn).Elts {
				kv := as[*ast.KeyValueExpr](t, elt)
				columns = append(columns, unquote(t, kv.Key)+" "+unquote(t, kv.Value))
			}
		case "ForeignKeys":
			found++
			for _, elt := range returnedLiteral(t, path, fn).Elts {
				keys = append(keys, foreignKeyLine(t, as[*ast.CompositeLit](t, elt)))
			}
		}
	}
	if found != 2 || table == "" {
		t.Fatalf("%s: want ColumnTypes and ForeignKeys, found %d of them (table %q)", path, found, table)
	}
	return table, columns, keys
}

// foreignKeyLine returns the catalogue line of one ForeignKey literal.
func foreignKeyLine(t *testing.T, lit *ast.CompositeLit) string {
	t.Helper()
	fields := map[string]ast.Expr{}
	for _, f := range lit.Elts {
		kv := as[*ast.KeyValueExpr](t, f)
		fields[as[*ast.Ident](t, kv.Key).Name] = kv.Value
	}
	optional := func(field string) string {
		if v, ok := fields[field]; ok {
			return unquote(t, v)
		}
		return ""
	}
	line := optional("Name") + " -> " + optional("ReferencedSchema") + "." + unquote(t, fields["References"]) + ":"
	columns, refColumns := as[*ast.CompositeLit](
		t,
		fields["Columns"],
	), as[*ast.CompositeLit](
		t,
		fields["ReferencedColumns"],
	)
	if len(columns.Elts) != len(refColumns.Elts) {
		t.Fatalf("%s has %d columns and %d referenced columns", line, len(columns.Elts), len(refColumns.Elts))
	}
	for i := range columns.Elts {
		line += " " + unquote(t, columns.Elts[i]) + "=" + unquote(t, refColumns.Elts[i])
	}
	return line
}

// as returns e as a T, or fails the test.
func as[T ast.Node](t *testing.T, e ast.Node) T {
	t.Helper()
	v, ok := e.(T)
	if !ok {
		t.Fatalf("%T is not a %T", e, v)
	}
	return v
}

func returnedLiteral(t *testing.T, path string, fn *ast.FuncDecl) *ast.CompositeLit {
	t.Helper()
	ret, ok := fn.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		t.Fatalf("%s: %s does not return one literal", path, fn.Name.Name)
	}
	lit, ok := ret.Results[0].(*ast.CompositeLit)
	if !ok {
		t.Fatalf("%s: %s does not return a composite literal", path, fn.Name.Name)
	}
	return lit
}

func unquote(t *testing.T, e ast.Expr) string {
	t.Helper()
	lit, ok := e.(*ast.BasicLit)
	if !ok {
		t.Fatalf("%T is not a string literal", e)
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// keyBuilder collects the catalogue lines of foreign keys, one row per
// column, in key order.
type keyBuilder struct {
	c     catalogue
	index map[string]int // table + "." + key -> index in c.keys[table]
}

func newCatalogue() *keyBuilder {
	return &keyBuilder{
		c:     catalogue{columns: map[string][]string{}, keys: map[string][]string{}},
		index: map[string]int{},
	}
}

func (b *keyBuilder) column(table, name, typ string) {
	b.c.columns[table] = append(b.c.columns[table], name+" "+typ)
}

// key adds one column of a foreign key. id tells the keys of a table apart:
// the constraint name, or the key's id on SQLite.
func (b *keyBuilder) key(table, id, name, refSchema, refTable, column, refColumn string) {
	k := table + "." + id
	i, seen := b.index[k]
	if !seen {
		b.c.keys[table] = append(b.c.keys[table], name+" -> "+refSchema+"."+refTable+":")
		i = len(b.c.keys[table]) - 1
		b.index[k] = i
	}
	b.c.keys[table][i] += " " + column + "=" + refColumn
}

// rows runs query and calls row with the text of each row's columns.
func rows(t *testing.T, db *sql.DB, query string, row func(cols []string), args ...any) {
	t.Helper()
	r, err := db.QueryContext(t.Context(), query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	names, err := r.Columns()
	if err != nil {
		t.Fatal(err)
	}
	for r.Next() {
		cols := make([]sql.NullString, len(names))
		ptrs := make([]any, len(names))
		for i := range cols {
			ptrs[i] = &cols[i]
		}
		if err = r.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		text := make([]string, len(cols))
		for i, c := range cols {
			text[i] = c.String
		}
		row(text)
	}
	if err = r.Err(); err != nil {
		t.Fatal(err)
	}
}

// pgCatalogue reads information_schema: udt_name for the types, and for the
// keys the referenced column of each referencing column, by its position in
// the referenced unique constraint.
func pgCatalogue(t *testing.T, db *sql.DB, schema string) catalogue {
	t.Helper()
	b := newCatalogue()
	rows(t, db, `
SELECT c.table_name, c.column_name, c.udt_name
FROM information_schema.columns c
JOIN information_schema.tables t ON t.table_schema = c.table_schema AND t.table_name = c.table_name
WHERE c.table_schema = $1 AND t.table_type = 'BASE TABLE'
ORDER BY c.table_name, c.ordinal_position`, func(c []string) { b.column(c[0], c[1], c[2]) }, schema)
	rows(t, db, `
SELECT kcu.table_name, kcu.constraint_name, ref.table_schema, ref.table_name, kcu.column_name, ref.column_name
FROM information_schema.referential_constraints rc
JOIN information_schema.key_column_usage kcu
  ON kcu.constraint_schema = rc.constraint_schema AND kcu.constraint_name = rc.constraint_name
JOIN information_schema.key_column_usage ref
  ON ref.constraint_schema = rc.unique_constraint_schema AND ref.constraint_name = rc.unique_constraint_name
 AND ref.ordinal_position = kcu.position_in_unique_constraint
WHERE kcu.table_schema = $1
ORDER BY kcu.table_name, kcu.constraint_name COLLATE "C", kcu.ordinal_position`, func(c []string) {
		b.key(c[0], c[1], c[1], sameSchema(c[2], schema), c[3], c[4], c[5])
	}, schema)
	return b.c
}

// mysqlCatalogue reads information_schema: COLUMN_TYPE for the types, and
// REFERENTIAL_CONSTRAINTS with the columns of KEY_COLUMN_USAGE for the keys.
func mysqlCatalogue(t *testing.T, db *sql.DB, name string) catalogue {
	t.Helper()
	b := newCatalogue()
	rows(t, db, `
SELECT c.TABLE_NAME, c.COLUMN_NAME, c.COLUMN_TYPE
FROM information_schema.COLUMNS c
JOIN information_schema.TABLES t ON t.TABLE_SCHEMA = c.TABLE_SCHEMA AND t.TABLE_NAME = c.TABLE_NAME
WHERE c.TABLE_SCHEMA = ? AND t.TABLE_TYPE = 'BASE TABLE'
ORDER BY c.TABLE_NAME, c.ORDINAL_POSITION`, func(c []string) { b.column(c[0], c[1], c[2]) }, name)
	rows(t, db, `
SELECT rc.TABLE_NAME, rc.CONSTRAINT_NAME, rc.UNIQUE_CONSTRAINT_SCHEMA, rc.REFERENCED_TABLE_NAME,
       k.COLUMN_NAME, k.REFERENCED_COLUMN_NAME
FROM information_schema.REFERENTIAL_CONSTRAINTS rc
JOIN information_schema.KEY_COLUMN_USAGE k
  ON k.CONSTRAINT_SCHEMA = rc.CONSTRAINT_SCHEMA AND k.TABLE_NAME = rc.TABLE_NAME
 AND k.CONSTRAINT_NAME = rc.CONSTRAINT_NAME
WHERE rc.CONSTRAINT_SCHEMA = ?
ORDER BY rc.TABLE_NAME, CAST(rc.CONSTRAINT_NAME AS BINARY), k.ORDINAL_POSITION`, func(c []string) {
		b.key(c[0], c[1], c[1], sameSchema(c[2], name), c[3], c[4], c[5])
	}, name)
	return b.c
}

// sqliteCatalogue reads the pragmas: the declared type of each column, and
// pragma_foreign_key_list for the keys, which have no name. A key declared
// without its referenced columns references the primary key.
func sqliteCatalogue(t *testing.T, db *sql.DB) catalogue {
	t.Helper()
	b := newCatalogue()
	var tables []string
	rows(t, db, `SELECT name FROM sqlite_master WHERE type = 'table' AND name != 'sqlite_sequence' ORDER BY name`,
		func(c []string) { tables = append(tables, c[0]) })
	for _, table := range tables {
		rows(t, db, `SELECT name, type FROM pragma_table_xinfo(?) ORDER BY cid`, func(c []string) {
			b.column(table, c[0], strings.TrimSuffix(c[1], " GENERATED ALWAYS"))
		}, table)
		rows(t, db, `
SELECT k.id, k."table", k."from",
       COALESCE(k."to", (SELECT p.name FROM pragma_table_info(k."table") p WHERE p.pk = k.seq + 1))
FROM pragma_foreign_key_list(?) k
ORDER BY k.id, k.seq`, func(c []string) {
			b.key(table, c[0], "", "", c[1], c[2], c[3])
		}, table)
	}
	return b.c
}

// sameSchema returns "" when refSchema is the schema of the table, as the
// generated code leaves ReferencedSchema out then.
func sameSchema(refSchema, schema string) string {
	if refSchema == schema {
		return ""
	}
	return refSchema
}
