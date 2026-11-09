package genrepo

import (
	"go/ast"
	"go/types"
	"slices"
	"strings"
)

// tableBase is one table that a declaration names, and the base generated
// for it.
type tableBase struct {
	varObj    *types.Var // the table variable, such as table.ProductUsers
	pkgPath   string     // the table package
	pkgName   string
	tableType types.Type // *table.ProductUsersTable
	goName    string     // ProductUsers
	schema    string     // the schema of the table, as the table file names it
	sqlName   string     // product_users, as the table file names it
	columns   []column   // in the table type's order
	keys      []uniqueKey
	keysFound bool              // the table type has a UniqueKeys method
	catalog   map[string]string // column name -> catalogue type, from the ColumnTypes method
	catalogOK bool              // the table type has a ColumnTypes method
	view      bool              // the table variable is in a view package of nano gen db

	foreignKeys []foreignKey // from ForeignKeys, in its order
	fksFound    bool         // the table type has a ForeignKeys method

	row    types.Type // the generated model, E
	domain types.Type // what the row helpers return, D
	mapper *mapperRef // nil: rows are returned as the model
	file   string     // the first declaration
	line   int
}

// column is one column of a table.
type column struct {
	field    string     // Go field name on the table and the model
	sqlName  string     // column name
	kind     string     // Nanostack DB column type, such as ColumnString
	value    types.Type // model field type without its pointer or Option
	nullable bool       // the model field is a pointer or an Option: NULL is allowed
	primary  bool       // part of the primary key
	lit      literal    // how a value of this column is passed; litNone: no equality predicate
	enum     string     // the quoted, schema-qualified enum type of a litEnum column
	dbType   string     // the catalogue type that ColumnTypes gives, such as text or inet; "" when not known
	notText  bool       // the model reads a string, but dbType is not text: no equality predicate
}

// literal is the Nanostack DB constructor a predicate passes its value
// through.
type literal int

const (
	litNone literal = iota
	litString
	litEnum
	litUUID
	litInt8
	litInt16
	litInt32
	litInt64
	litReal
	litDouble
	litBool
	litTimestampz
	litTimestamp
	litDate
	litTime
	litTimez
)

func (l literal) ordered() bool {
	return l >= litInt8 && l <= litDouble || l >= litTimestampz
}

func (t *tableBase) baseName() string  { return lowerFirst(t.goName) + "Base" }
func (t *tableBase) whereName() string { return lowerFirst(t.goName) + "Where" }
func (t *tableBase) newName() string   { return "new" + t.goName + "Base" }

// declaredNames are the package-level names the base of t declares.
func (t *tableBase) declaredNames() []string {
	return []string{t.baseName(), t.whereName(), t.newName()}
}

// textual reports whether c gets the Contains and EqualsIgnoreCase
// predicates: a string column that is not a key or an identifier, and whose
// catalogue type is text. A column of another type that the model reads as a
// string, such as inet or jsonb, fails under ILIKE and LOWER, so it gets
// neither.
func (c column) textual() bool {
	return c.candidate() && textType(c.dbType)
}

// untypedText returns the string columns of a view without ColumnTypes
// that would get Contains and EqualsIgnoreCase if their type were known.
func (t *tableBase) untypedText() []string {
	if !t.view || t.catalogOK {
		return nil
	}
	var out []string
	for _, c := range t.columns {
		if c.candidate() {
			out = append(out, c.sqlName)
		}
	}
	return out
}

// candidate reports whether the catalogue type of c decides if it is
// textual: a string column that is not a key or an identifier.
func (c column) candidate() bool {
	return c.lit == litString && !c.primary && !strings.HasSuffix(c.field, "ID")
}

// textType reports whether the catalogue type t, as ColumnTypes writes it,
// is a text type: text, varchar, bpchar (char(n)) or citext. citext is an
// extension type, so it can be in another schema than the table.
func textType(t string) bool {
	switch t {
	case "text", "varchar", "bpchar", "citext":
		return true
	}
	_, name, qualified := strings.Cut(t, ".")
	return qualified && name == "citext"
}

// column returns the column of the Go field name.
func (t *tableBase) column(field string) (column, bool) {
	for _, c := range t.columns {
		if c.field == field {
			return c, true
		}
	}
	return column{}, false
}

// fields returns the Go field names of the columns, in table order.
func (t *tableBase) fields() []string {
	out := make([]string, len(t.columns))
	for i, c := range t.columns {
		out[i] = c.field
	}
	return out
}

// scopeFields returns the Go fields of the scope columns of t: its NOT NULL
// columns that scopes names (design D25).
func (t *tableBase) scopeFields(scopes []string) []string {
	var out []string
	for _, c := range t.columns {
		if slices.Contains(scopes, c.sqlName) && !c.nullable {
			out = append(out, c.field)
		}
	}
	return out
}

// table resolves the table argument of a declaration: a table variable of a
// PostgreSQL table package that nano gen db generated.
func (c *collector) table(arg ast.Expr) (*tableBase, bool) {
	v, named, dialect, ok := c.tableVar(arg)
	if !ok {
		return nil, false
	}
	if dialect != postgresPath {
		c.errorf(arg, "remove the declaration; the repository stays hand-written, with pkg/db/transactor calls "+
			"on the table",
			"%s is a %s table; nano gen repo generates bases for PostgreSQL tables only", c.source(arg),
			dialects[dialect])
		return nil, false
	}
	return c.tableOf(arg, v, named)
}

// tableVar resolves a table argument to its variable, its named struct type
// and the package of the dialect whose Table it embeds. It reports an
// argument that is not a table variable of a Nanostack DB table package.
func (c *collector) tableVar(arg ast.Expr) (*types.Var, *types.Named, string, bool) {
	info := c.l.pkg.TypesInfo
	var id *ast.Ident
	switch x := arg.(type) {
	case *ast.Ident:
		id = x
	case *ast.SelectorExpr:
		if _, isPkg := info.Uses[identOf(x.X)].(*types.PkgName); isPkg {
			id = x.Sel
		}
	}
	v, _ := info.Uses[identOf(id)].(*types.Var)
	if id == nil || v == nil || v.Pkg() == nil || v.Parent() != v.Pkg().Scope() {
		c.errorf(arg, "pass the table variable of the generated table package, such as table.Users",
			"the table argument %s is not a table variable", c.source(arg))
		return nil, nil, "", false
	}
	named := tableNamed(v.Type())
	dialect := ""
	if named != nil {
		dialect = embeddedDialect(named, 0)
	}
	if dialect == "" {
		c.errorf(arg, "pass a table variable that nano gen db generated, such as table.Users",
			"%s is not a Nanostack DB table", c.source(arg))
		return nil, nil, "", false
	}
	return v, named, dialect, true
}

// tableOf reads what the table package of v declares for it: its names,
// columns, unique keys and catalogue facts.
func (c *collector) tableOf(arg ast.Expr, v *types.Var, named *types.Named) (*tableBase, bool) {
	t := &tableBase{varObj: v, pkgPath: v.Pkg().Path(), pkgName: v.Pkg().Name(), tableType: v.Type(),
		goName: v.Name(), view: v.Pkg().Name() == "view"}
	src, err := c.l.tableSource(v.Pkg().Path())
	if err != nil {
		c.problems = append(c.problems, err)
		return nil, false
	}
	names, ok := src.tables[v.Name()]
	if !ok {
		c.errorf(arg, "regenerate the table package with nano gen db",
			"the table package %s does not declare %s as nano gen db does (var %s = new%sTable(...))",
			v.Pkg().Path(), v.Name(), v.Name(), strings.TrimSuffix(named.Obj().Name(), "Table"))
		return nil, false
	}
	t.schema, t.sqlName = names.schema, names.table
	t.columns = tableColumns(named, names.columns)
	keys, found := src.keys[named.Obj().Name()]
	t.resolveKeys(keys, found)
	t.catalog, t.catalogOK = src.types[named.Obj().Name()]
	t.foreignKeys, t.fksFound = src.foreignKeys[named.Obj().Name()]
	return t, true
}

// tableNamed returns the named struct type of a table variable: T for a *T
// or a T.
func tableNamed(t types.Type) *types.Named {
	t = types.Unalias(t)
	if p, ok := t.(*types.Pointer); ok {
		t = types.Unalias(p.Elem())
	}
	n, ok := t.(*types.Named)
	if !ok {
		return nil
	}
	if _, isStruct := n.Underlying().(*types.Struct); !isStruct {
		return nil
	}
	return n
}

// embeddedDialect returns the package of the Nanostack DB Table interface
// that t embeds, directly or through embedded structs, or "".
func embeddedDialect(t *types.Named, depth int) string {
	st, ok := t.Underlying().(*types.Struct)
	if !ok || depth > 3 {
		return ""
	}
	for f := range st.Fields() {
		if !f.Embedded() {
			continue
		}
		n, isNamed := types.Unalias(f.Type()).(*types.Named)
		if !isNamed || n.Obj().Pkg() == nil {
			continue
		}
		if _, known := dialects[n.Obj().Pkg().Path()]; known && n.Obj().Name() == "Table" {
			return n.Obj().Pkg().Path()
		}
		if d := embeddedDialect(n, depth+1); d != "" {
			return d
		}
	}
	return ""
}

// tableColumns lists the column fields of a table type, with the column
// names the table file gives them.
func tableColumns(t *types.Named, sqlNames map[string]string) []column {
	st, ok := t.Underlying().(*types.Struct)
	if !ok {
		return nil
	}
	return appendColumns(nil, st, sqlNames)
}

// appendColumns appends the column fields of st, and of the struct types it
// embeds other than the dialect's Table, to out.
func appendColumns(out []column, st *types.Struct, sqlNames map[string]string) []column {
	for f := range st.Fields() {
		if f.Embedded() {
			if inner := embeddedStruct(f.Type()); inner != nil {
				out = appendColumns(out, inner, sqlNames)
			}
			continue
		}
		kind := columnKind(f.Type())
		if kind == "" || f.Name() == "EXCLUDED" {
			continue
		}
		name, known := sqlNames[f.Name()]
		if !known {
			name = snake(f.Name())
		}
		out = append(out, column{field: f.Name(), sqlName: name, kind: kind})
	}
	return out
}

// embeddedStruct returns the struct of an embedded field type that is not a
// dialect's Table, or nil.
func embeddedStruct(t types.Type) *types.Struct {
	n, ok := types.Unalias(t).(*types.Named)
	if !ok || n.Obj().Pkg() == nil {
		return nil
	}
	if _, dialect := dialects[n.Obj().Pkg().Path()]; dialect {
		return nil
	}
	st, _ := n.Underlying().(*types.Struct)
	return st
}

// columnKind returns the Nanostack DB column type of a table field, such as
// ColumnString, or "" for a field that is not a column (ColumnList).
func columnKind(t types.Type) string {
	var name string
	switch x := t.(type) {
	case *types.Alias:
		name = x.Obj().Name()
	case *types.Named:
		name = x.Obj().Name()
	default:
		return ""
	}
	if !strings.HasPrefix(name, "Column") || name == "ColumnList" {
		return ""
	}
	return name
}
