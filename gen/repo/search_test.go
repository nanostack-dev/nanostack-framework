package genrepo

import (
	"bytes"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// The searches of testdata/search read without a problem, and -explain
// shows what nano gen repo found: the instances of each From function, with
// the columns of a LEFT JOIN marked nullable, an alias, and the unique key
// that makes each join add at most one row (one equated with a scope field);
// the instances of a subquery; a Tiebreak that covers the primary key
// (shaped like echopoint B10); the primary key as the default tiebreak; and
// every option.
func TestSearchExplain(t *testing.T) {
	t.Parallel()
	res, err := Generate(Options{Dir: "testdata/search", Scopes: []string{"tenant_id"}})
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	WriteSearchExplain(&b, res.Searches)
	golden(t, "testdata/search/want-explain.txt", b.Bytes())
	explain := b.String()
	for _, want := range []string{
		"tiebreak   sku, id: covers items_pkey (id)\n",
		"read       tenants; LEFT JOIN; ON table.Tenants.ID.EQ(table.Items.TenantID); one row by tenants_pkey (id); " +
			"every column nullable\n",
		"ON table.Tenants.Name.EQ(postgres.String(s.TenantName)); one row by tenants_name_key (name)\n",
		"sort       owner_name: owner.name\n",
		"subquery   SortExpr Siblings siblingCount reads items AS sibling\n",
		"tiebreak   id: the primary key items_pkey (id), by default\n",
		"sort       name: tenants.name, nullable\n",
		"read       tenants AS owner; INNER JOIN; ",
		"fulltext   tenantName(), Go only\n",
	} {
		if !strings.Contains(explain, want) {
			t.Errorf("-explain lacks %q:\n%s", want, explain)
		}
	}
}

// Main -explain prints the searches too, and filters them by name.
func TestMainExplainSearch(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	args := []string{"-dir", "testdata/search", "-explain", "ItemOptionalTenant"}
	if code := Main(args, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if out := stdout.String(); !strings.HasPrefix(out, "search ItemOptionalTenant on items (rows model.Items)\n") ||
		strings.Contains(out, "ItemBySku") {
		t.Errorf("stdout:\n%s", out)
	}
}

// A ColumnTypes value of another schema keeps its schema. A qualified name
// is text only for citext: "public.text" is a user type with the name of a
// pg_catalog type.
func TestTextType(t *testing.T) {
	t.Parallel()
	for typ, want := range map[string]bool{
		"text": true, "varchar": true, "bpchar": true, "citext": true, "ext.citext": true,
		"uuid": false, "inet": false, "jsonb": false, "_text": false, "timestamptz": false, "item_status": false,
		"ledger.text_status": false, "public.text": false, "ledger.varchar": false, "public.bpchar": false,
		"public.citext": true,
	} {
		if got := textType(typ); got != want {
			t.Errorf("textType(%q) = %v, want %v", typ, got, want)
		}
	}
}

// The pipeline runs nano gen repo on a file that declares only a search, so
// the declaration is never ignored.
func TestDeclaresSearch(t *testing.T) {
	t.Parallel()
	for src, want := range map[string]bool{
		"package p\nimport \"github.com/nanostack-dev/nanostack-framework/db/nanorepo\"\n" +
			"func _() { nanorepo.Search[R](\"Item\", table.Items) }\n": true,
		"package p\nimport nr \"github.com/nanostack-dev/nanostack-framework/db/nanorepo\"\n" +
			"func _() { nr.Search[R](\"Item\", table.Items) }\n": true,
		"package p\nfunc _() { other.Search[R](\"Item\", table.Items) }\n": false,
	} {
		f, err := parser.ParseFile(token.NewFileSet(), "p.go", src, 0)
		if err != nil {
			t.Fatal(err)
		}
		if got := Declares(f); got != want {
			t.Errorf("Declares = %v, want %v for\n%s", got, want, src)
		}
	}
}

// ColumnTypes and ForeignKeys are read from the table file that nano gen db
// writes, with no database.
func TestReadCatalogue(t *testing.T) {
	t.Parallel()
	f, err := parser.ParseFile(token.NewFileSet(), fixture+"/db/shop/public/table/items.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	src := &tableFiles{types: map[string]map[string]string{}, foreignKeys: map[string][]foreignKey{}}
	readColumnTypes(f, src)
	readForeignKeys(f, src)
	types := src.types["ItemsTable"]
	if types["tenant_id"] != "uuid" || types["status"] != "item_status" || types["tags"] != "_text" ||
		len(types) != 18 {
		t.Errorf("ColumnTypes: %v", types)
	}
	fks := src.foreignKeys["ItemsTable"]
	if len(fks) != 1 || fks[0].String() != "items_tenant_id_fkey (tenant_id) -> tenants (id)" {
		t.Errorf("ForeignKeys: %v", fks)
	}
	f, err = parser.ParseFile(token.NewFileSet(), fixture+"/db/shop/public/table/audit_events.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	readForeignKeys(f, src)
	if none, found := src.foreignKeys["AuditEventsTable"]; !found || len(none) != 0 {
		t.Errorf("a table with no foreign key: %v, %v", none, found)
	}
}
