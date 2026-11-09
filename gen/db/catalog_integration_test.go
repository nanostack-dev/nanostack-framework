//go:build integration

package gendb

import (
	"database/sql"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The catalogue tests of ColumnTypes and ForeignKeys, on the keys fixture
// (see integration_test.go). The tests of the db module check the same
// methods on PostgreSQL, MySQL and SQLite (db/tests, TestCatalogMethodsMatchCatalog).

// ColumnTypes of every table file lists every column of its table, in
// column order, with the type information_schema.columns gives as
// udt_schema and udt_name, resolved by pgTypeName: the base type at the end
// of a chain of domains, the array type of an array, the enum's name, and the
// schema of a type of another schema or of a type that has the name of a
// pg_catalog type.
func TestColumnTypesMatchCatalog(t *testing.T) {
	db, drop := migrated(t)
	defer drop()
	domains := map[[2]string][2]string{} // domain -> its base, one level
	queryRows(t, db, `SELECT domain_schema, domain_name, udt_schema, udt_name FROM information_schema.domains`,
		func(cols []string) { domains[[2]string{cols[0], cols[1]}] = [2]string{cols[2], cols[3]} })
	pgCatalog := map[string]bool{}
	queryRows(t, db, `SELECT typname FROM pg_catalog.pg_type WHERE typnamespace = 'pg_catalog'::regnamespace`,
		func(cols []string) { pgCatalog[cols[0]] = true })
	catalog := map[string][]string{} // table -> "column type" in column order
	queryRows(t, db, `
SELECT c.table_name, c.column_name, c.udt_schema, c.udt_name
FROM information_schema.columns c
JOIN information_schema.tables t ON t.table_schema = c.table_schema AND t.table_name = c.table_name
WHERE c.table_schema = 'public' AND t.table_type = 'BASE TABLE'
ORDER BY c.table_name, c.ordinal_position`, func(cols []string) {
		catalog[cols[0]] = append(catalog[cols[0]], cols[1]+" "+pgTypeName(domains, pgCatalog, cols[2], cols[3]))
	})
	emitted := map[string][]string{}
	for file, lit := range methodLiterals(t, "ColumnTypes") {
		var entries []string
		for _, elt := range lit.Elts {
			kv := as[*ast.KeyValueExpr](t, elt)
			entries = append(entries, unquote(t, kv.Key)+" "+unquote(t, kv.Value))
		}
		emitted[tableOf(file)] = entries
	}
	if len(emitted) != len(catalog) {
		t.Fatalf("ColumnTypes on %d table files, the catalogue has %d tables", len(emitted), len(catalog))
	}
	for table, want := range catalog {
		if got := emitted[table]; !slices.Equal(got, want) {
			t.Errorf("%s: ColumnTypes %q, catalogue %q", table, got, want)
		}
	}
	for _, want := range []string{
		"user_email text",                  // a domain
		"backup_email text",                // a domain of a domain
		"emails _text",                     // an array of a domain
		"ledger_status ledger.user_status", // an enum of another schema, with the name of a public enum
		"route public.path",                // a public enum with the name of a pg_catalog type
	} {
		if !slices.Contains(emitted["events"], want) {
			t.Errorf("events: ColumnTypes %q has no %q", emitted["events"], want)
		}
	}
	if want := "status user_status"; !slices.Contains(emitted["users"], want) {
		t.Errorf("users: ColumnTypes %q has no %q", emitted["users"], want)
	}
}

// pgTypeName follows a domain to its base until the base is not a domain. An
// array of a domain ("_" and the name of the domain) becomes the array of its
// base. The name has its schema unless the type is in pg_catalog, or is in
// public and pg_catalog has no type of that name.
func pgTypeName(domains map[[2]string][2]string, pgCatalog map[string]bool, typeSchema, typeName string) string {
	array := ""
	if _, ok := domains[[2]string{typeSchema, strings.TrimPrefix(typeName, "_")}]; ok &&
		strings.HasPrefix(typeName, "_") {
		array, typeName = "_", typeName[1:]
	}
	for {
		base, ok := domains[[2]string{typeSchema, typeName}]
		if !ok {
			break
		}
		typeSchema, typeName = base[0], base[1]
	}
	typeName = array + typeName
	if typeSchema == "pg_catalog" || (typeSchema == "public" && !pgCatalog[typeName]) {
		return typeName
	}
	return typeSchema + "." + typeName
}

// ForeignKeys of every table file lists every foreign key of its table, as
// information_schema gives it: the referencing columns in key order, and
// the referenced column of each, by its position in the referenced unique
// constraint. A key to a table of another schema names that schema.
func TestForeignKeysMatchCatalog(t *testing.T) {
	db, drop := migrated(t)
	defer drop()
	catalog := map[string][]string{} // table -> one line per key, in constraint name order
	keyLine := map[string]int{}      // table.constraint -> index in catalog[table]
	queryRows(t, db, `
SELECT kcu.table_name, kcu.constraint_name, ref.table_schema, ref.table_name, kcu.column_name, ref.column_name
FROM information_schema.referential_constraints rc
JOIN information_schema.key_column_usage kcu
  ON kcu.constraint_schema = rc.constraint_schema AND kcu.constraint_name = rc.constraint_name
JOIN information_schema.key_column_usage ref
  ON ref.constraint_schema = rc.unique_constraint_schema AND ref.constraint_name = rc.unique_constraint_name
 AND ref.ordinal_position = kcu.position_in_unique_constraint
WHERE kcu.table_schema = 'public'
ORDER BY kcu.table_name, kcu.constraint_name COLLATE "C", kcu.ordinal_position`, func(cols []string) {
		table, name, refSchema, refTable, column, refColumn := cols[0], cols[1], cols[2], cols[3], cols[4], cols[5]
		key := table + "." + name
		i, seen := keyLine[key]
		if !seen {
			if refSchema == "public" {
				refSchema = ""
			}
			catalog[table] = append(catalog[table], name+" -> "+refSchema+"."+refTable+":")
			i = len(catalog[table]) - 1
			keyLine[key] = i
		}
		catalog[table][i] += " " + column + "=" + refColumn
	})
	emitted := map[string][]string{}
	for file, lit := range methodLiterals(t, "ForeignKeys") {
		lines := []string{}
		for _, elt := range lit.Elts {
			fields := map[string]ast.Expr{}
			for _, f := range as[*ast.CompositeLit](t, elt).Elts {
				kv := as[*ast.KeyValueExpr](t, f)
				fields[as[*ast.Ident](t, kv.Key).Name] = kv.Value
			}
			refSchema := ""
			if v, ok := fields["ReferencedSchema"]; ok {
				refSchema = unquote(t, v)
			}
			line := unquote(t, fields["Name"]) + " -> " + refSchema + "." + unquote(t, fields["References"]) + ":"
			columns := as[*ast.CompositeLit](t, fields["Columns"])
			refColumns := as[*ast.CompositeLit](t, fields["ReferencedColumns"])
			if len(columns.Elts) != len(refColumns.Elts) {
				t.Fatalf("%s: %s has %d columns and %d referenced columns", file, line, len(columns.Elts),
					len(refColumns.Elts))
			}
			for i := range columns.Elts {
				line += " " + unquote(t, columns.Elts[i]) + "=" + unquote(t, refColumns.Elts[i])
			}
			lines = append(lines, line)
		}
		emitted[tableOf(file)] = lines
	}
	// information_schema joins the keys by constraint name, and also lists the
	// copies of a key to a partitioned table, one per partition, that
	// PostgreSQL adds to the referencing table. The partitioned tables are
	// checked against exact lists below.
	partitioned := map[string][]string{
		"regions":   {},
		"regions_1": {},
		"sites":     {"sites_region_fkey -> .regions: region_id=id part=part"},
		"sites_1":   {"sites_region_fkey -> .regions: region_id=id part=part"},
	}
	for table, want := range partitioned {
		if got, ok := emitted[table]; !ok || !slices.Equal(got, want) {
			t.Errorf("%s: ForeignKeys %q, want %q", table, got, want)
		}
		delete(emitted, table)
		delete(catalog, table)
	}
	for table, got := range emitted {
		if want := catalog[table]; !slices.Equal(got, want) && (len(got) > 0 || len(want) > 0) {
			t.Errorf("%s: ForeignKeys %q, catalogue %q", table, got, want)
		}
	}
	for table := range catalog {
		if _, ok := emitted[table]; !ok {
			t.Errorf("%s has foreign keys and no table file with ForeignKeys", table)
		}
	}
	for table, want := range map[string]string{
		"events":  "events_user_fkey -> .users: user_email=email tenant_id=tenant_id",
		"tenants": "tenants_account_id_fkey -> ledger.accounts: account_id=id",
		"users":   "users_invited_by_fkey -> .users: invited_by=id",
	} {
		if !slices.Contains(emitted[table], want) {
			t.Errorf("%s: ForeignKeys %q has no %q", table, emitted[table], want)
		}
	}
}

// methodLiterals returns, by golden table file, the composite literal that
// the named method of its table type returns.
func methodLiterals(t *testing.T, method string) map[string]*ast.CompositeLit {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(keysDir, "golden/keys/public/table/*.go"))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]*ast.CompositeLit{}
	for _, file := range files {
		f, parseErr := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Name.Name != method {
				continue
			}
			ret, isReturn := fn.Body.List[0].(*ast.ReturnStmt)
			if !isReturn || len(ret.Results) != 1 {
				t.Fatalf("%s: %s does not return one literal", file, method)
			}
			lit, isLit := ret.Results[0].(*ast.CompositeLit)
			if !isLit {
				t.Fatalf("%s: %s does not return a composite literal", file, method)
			}
			out[file] = lit
		}
	}
	return out
}

// tableOf returns the table of a golden table file, such as users for
// users.go.
func tableOf(file string) string { return strings.TrimSuffix(filepath.Base(file), ".go") }

// queryRows runs query and calls row with the text of each row's columns.
func queryRows(t *testing.T, db *sql.DB, query string, row func(cols []string)) {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), query)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	names, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		cols := make([]string, len(names))
		ptrs := make([]any, len(names))
		for i := range cols {
			ptrs[i] = &cols[i]
		}
		if err = rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		row(cols)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
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
