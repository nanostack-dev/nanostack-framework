package genrepo

import (
	"errors"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"path"
	"strings"

	"github.com/nanostack-dev/nanostack-framework/gen"
)

// searchDecl is one nanorepo.Search declaration (ADR 0027): the table
// instances the search reads and every option. nano gen repo reads and
// checks the declaration, then writes its builder (design 4.2).
type searchDecl struct {
	name      string
	at        at
	row       types.Type
	tableText string // the table argument as written, such as table.Items
	tableNode ast.Expr
	root      *instance   // the searched table
	reads     []*instance // the root alone, or every table of the From function in join order
	from      *fromFunc
	exprs     []exprFunc  // the expression functions of the options, in option order
	inner     []innerRead // the instances that a subquery of a From or expression function reads

	mapper          *searchMapper
	scopes          []scopeDecl
	scopeFuncs      []scopeFuncDecl
	unscoped        []unscopedDecl
	conditions      []ast.Expr
	filters         []*searchFilter
	fullText        []*colRef
	fullTextExprs   []ast.Expr
	fullTextGoOnly  map[ast.Expr]bool // the FullText columns and FullTextExpr functions wrapped in GoOnly
	sorts           []*searchSort
	tiebreak        []*colRef // as declared; empty for the default
	tiebreakNode    ast.Node  // the Tiebreak call, nil when absent
	order           []column  // the tiebreak columns: Tiebreak, or the primary key in index order
	orderKey        uniqueKey // the key that order covers
	orderDefault    bool      // order is the primary key because Tiebreak is absent
	defaultSort     *defaultSort
	defaultLimit    int32 // 0 when absent
	api             types.Type
	apiNode         ast.Node // the API call, nil without API
	ignoresFullText string
	ignoresNode     ast.Node // the IgnoresFullText call
	apiHandWritten  []string
	handNode        ast.Node // the first APIHandWritten call
	// mapper is the API mapper that nano gen repo writes, nil without API
	// or after a problem of the binding.
	apiMapper *apiMapper

	// builder is what nano gen repo writes for the search; unbuilt holds the
	// problems of a shape it cannot write, which do not hide the search
	// from -explain.
	builder *builder
	unbuilt []error

	// exemptions are the lines of the exemption file that the tenant check
	// found (design S6.6); tenantLines say how each instance is covered.
	exemptions  []string
	tenantLines []string

	given   map[string]at       // options that a search takes once, by group -> where
	givenAs map[string]string   // the group of a once-option -> the option given, such as MapPage
	stale   map[*tableBase]bool // tables whose missing facts are reported (E17)
}

// instance is one table instance that a search reads: a table and its alias
// (design S5).
type instance struct {
	table *tableBase
	alias string
	text  string   // the table variable as written, such as table.Items
	node  ast.Expr // where the instance is written
	join  string   // "" for the first table of the join, else "INNER JOIN" or "LEFT JOIN"
	on    ast.Expr // the ON condition, nil for the first table
	// nullable is true on the nullable side of an outer join: every column of
	// the instance can be NULL in a result row, whatever its model says.
	nullable bool
	// key is the unique key that the ON condition equates, so that the join
	// adds at most one row per row before it (E31). nil for the first table.
	key *uniqueKey
}

func (i *instance) String() string {
	if i.alias != "" {
		return i.table.sqlName + " AS " + i.alias
	}
	return i.table.sqlName
}

// name is the name that SQL gives the instance: its alias, or its table.
func (i *instance) name() string {
	if i.alias != "" {
		return i.alias
	}
	return i.table.sqlName
}

// is reports whether the instance is the table t with alias.
func (i *instance) is(t *tableBase, alias string) bool { return i.table == t && i.alias == alias }

// exprFunc is the function of an expression option: FilterExpr, Flag,
// Condition, SortExpr or FullTextExpr.
type exprFunc struct {
	role string   // how a message names it, such as "Flag Fresh"
	fn   ast.Expr // the argument that names the function
}

// innerRead is an instance that a subquery reads, inside the function of
// role, with its join kind and ON condition. The first instance of a
// subquery carries the WHERE condition that correlates it.
type innerRead struct {
	role  string
	inst  *instance
	where ast.Expr
}

// nullable reports whether col of the instance can be NULL in a result row.
func (i *instance) columnNullable(col column) bool { return i.nullable || col.nullable }

// fromFunc is the function that From names.
type fromFunc struct {
	name       string
	takesScope bool     // func(P) T: the function reads the scope value
	arg        ast.Expr // the argument of From, where its problems are reported
	decl       *ast.FuncDecl
	param      types.Type // P, the type of the scope value; nil for func() T
	// scopeVars are the parameter of the function and the parameters of the
	// helpers it passes that parameter to: the scope value of the caller.
	scopeVars map[*types.Var]bool
}

// colRef is a column that an option names, such as table.Items.Name or
// table.Tenants.AS("owner").Name.
type colRef struct {
	expr      ast.Expr
	table     *tableBase
	alias     string
	tableText string // the table variable as written, such as table.Tenants
	col       column
}

func (r *colRef) String() string { return types.ExprString(r.expr) }

// searchMapper is Map or MapPage.
type searchMapper struct {
	page bool
	fn   ast.Expr
}

type scopeDecl struct {
	kind string // Scope, ScopeOrNull or ScopeNull
	cols []*colRef
}

type scopeFuncDecl struct {
	fn  ast.Expr
	col *colRef
}

type unscopedDecl struct {
	col *colRef
	why string
}

// searchFilter is one field of the query builder.
type searchFilter struct {
	kind   string  // Filter, FilterAs, FilterExpr, FilterExists or Flag
	name   string  // the field name
	col    *colRef // the filtered column of Filter, FilterAs and FilterExists
	as     types.Type
	child  *tableBase // FilterExists
	link   *colRef    // FilterExists
	ref    *colRef    // FilterExists: the root column that link references
	key    string     // FilterExists: the foreign key of link
	expr   ast.Expr   // the function of FilterExpr and Flag
	ops    []searchOp
	goOnly bool
	node   ast.Node
}

// searchOp is one operator of a filter.
type searchOp struct {
	name   string   // Eq, In, InFold, Contains, ContainsAny, Present or Range
	api    []string // the API names that API or Range gives
	bounds string   // "HalfOpen" or "Exclusive" for a Range, else ""
	node   ast.Expr
}

// searchSort is one sort of a search.
type searchSort struct {
	kind  string  // Sort, SortKey, SortExpr or SortHandWritten
	col   *colRef // Sort and SortKey
	field string  // SortExpr: the Row field
	expr  ast.Expr
	api   []string // the sort_by values
	nulls string   // "NullsFirst" or "NullsLast" for a SortKey, else ""
}

// defaultSort is DefaultSort or DefaultSortExpr.
type defaultSort struct {
	col   *colRef // DefaultSort
	field string  // DefaultSortExpr
	desc  bool
	node  ast.Node
}

// searchCall reads one nanorepo.Search call. A search with a problem is not
// added: its problems are.
func (c *collector) searchCall(call *ast.CallExpr, inst types.Instance, typed bool) {
	if !typed || inst.TypeArgs.Len() != 1 || len(call.Args) < 2 {
		c.errorf(call, `write nanorepo.Search[model.YourRow]("YourSearch", table.YourTable, options...)`,
			"nanorepo.Search does not type-check")
		return
	}
	file, line := c.l.position(c.l.pkg.Fset, call.Pos())
	s := &searchDecl{at: at{file, line}, row: inst.TypeArgs.At(0), given: map[string]at{},
		givenAs: map[string]string{}, stale: map[*tableBase]bool{}, fullTextGoOnly: map[ast.Expr]bool{}}
	before := len(c.problems)
	if !c.searchName(s, call.Args[0]) {
		return
	}
	// The name is registered before the options are read, so a second
	// search of the name is reported also when the first has a problem.
	if first, dup := c.searchNames[s.name]; dup {
		c.errorf(call, "rename one of the two searches",
			"search %s is declared twice: %s and %s", s.name, first, s.at)
		return
	}
	c.searchNames[s.name] = s.at
	s.tableText, s.tableNode = c.source(call.Args[1]), call.Args[1]
	root := c.searchTable(s, call.Args[1])
	if root == nil {
		return
	}
	s.root = &instance{table: root, text: s.tableText, node: call.Args[1]}
	for _, arg := range call.Args[2:] {
		c.searchOption(s, arg, false)
	}
	if s.from == nil {
		s.reads = []*instance{s.root}
	}
	if len(c.problems) > before {
		return
	}
	c.readSet(s)
	if len(c.problems) > before {
		return
	}
	c.checkSearch(s)
	if len(c.problems) > before {
		return
	}
	c.planBuilder(s)
	c.planAPI(s)
	c.d.searches = append(c.d.searches, s)
}

// searchName reads the name argument: a constant exported Go identifier,
// which names the generated types.
func (c *collector) searchName(s *searchDecl, arg ast.Expr) bool {
	tv := c.l.pkg.TypesInfo.Types[arg]
	if tv.Value == nil || tv.Value.Kind() != constant.String {
		c.errorf(arg, `pass the name as a string literal, such as "WebhookRequest"`,
			"the name of nanorepo.Search is %s, not a constant string", c.source(arg))
		return false
	}
	s.name = constant.StringVal(tv.Value)
	if !token.IsIdentifier(s.name) || !token.IsExported(s.name) {
		c.errorf(arg, `name the search with an exported Go identifier, such as "WebhookRequest"`,
			"search %q: the name is not an exported Go identifier, and it names the generated types "+
				"(%sSearch)", s.name, s.name)
		return false
	}
	return true
}

// searchTable resolves a table that a search reads: a PostgreSQL table of a
// package that nano gen db generated with ColumnTypes and ForeignKeys, with
// the facts of its model. Tables are read once per run.
func (c *collector) searchTable(s *searchDecl, arg ast.Expr) *tableBase {
	v, named, dialect, ok := c.tableVar(arg)
	if !ok {
		return nil
	}
	if dialect != postgresPath {
		c.errorf(arg, "remove the declaration and write the search by hand on db/searchexec",
			"search %s: %s is a %s table; nano gen repo writes PostgreSQL searches in this release",
			s.name, c.source(arg), dialects[dialect])
		return nil
	}
	if t, read := c.searchTables[v]; read {
		return c.searchFacts(s, arg, t)
	}
	t, ok := c.tableOf(arg, v, named)
	if !ok {
		return nil
	}
	c.searchTables[v] = t
	if !c.searchModel(arg, t) {
		c.searchTables[v] = nil
		return nil
	}
	return c.searchFacts(s, arg, t)
}

// searchFacts checks that the table package lists the facts a search needs
// (E17), once per search and table. t is nil when reading its model
// failed, which is reported once.
func (c *collector) searchFacts(s *searchDecl, arg ast.Expr, t *tableBase) *tableBase {
	if t == nil || s.stale[t] {
		return nil
	}
	var missing []string
	for _, m := range []struct {
		name  string
		found bool
	}{{"UniqueKeys", t.keysFound}, {"ColumnTypes", t.catalogOK}, {"ForeignKeys", t.fksFound}} {
		if !m.found {
			missing = append(missing, m.name)
		}
	}
	if len(missing) > 0 {
		s.stale[t] = true
		c.errorf(arg, "run nano gen db, which writes them",
			"search %s reads %s, and the table package has no %s for it", s.name, t.sqlName,
			strings.Join(missing, " and no "))
		return nil
	}
	for _, col := range t.columns {
		if _, typed := t.catalog[col.sqlName]; !typed {
			s.stale[t] = true
			c.errorf(arg, "run nano gen db, which writes ColumnTypes from the catalogue",
				"search %s reads %s, and its ColumnTypes has no type for column %s", s.name, t.sqlName, col.sqlName)
			return nil
		}
	}
	return t
}

// searchModel finds the model of t, the type of the same name in the model
// package next to the table package, as nano gen db writes them, and sets
// the model facts of each column: Go type, nullability and primary key.
func (c *collector) searchModel(arg ast.Expr, t *tableBase) bool {
	modelPath := path.Join(path.Dir(t.pkgPath), "model")
	pkg, err := c.l.typesOf(modelPath)
	if err != nil {
		c.problems = append(c.problems, err)
		return false
	}
	var model types.Type
	if pkg != nil {
		if obj, isType := pkg.Scope().Lookup(t.goName).(*types.TypeName); isType {
			model = obj.Type()
		}
	}
	st, isStruct := types.Unalias(types.Default(typeOrNil(model))).Underlying().(*types.Struct)
	if model == nil || !isStruct {
		c.errorf(arg, "regenerate the models with nano gen db",
			"the model package %s has no struct %s for table %s", modelPath, t.goName, t.sqlName)
		return false
	}
	t.model = model
	if col := c.applyModel(t, model, st); col != nil {
		c.errorf(arg, "regenerate the models with nano gen db",
			"the model %s.%s has no field %s for column %s of table %s", pkg.Name(), t.goName, col.field,
			col.sqlName, t.sqlName)
		return false
	}
	return true
}

func typeOrNil(t types.Type) types.Type {
	if t == nil {
		return types.Typ[types.Invalid]
	}
	return t
}

// searchWithoutRepository is the problem of a package that declares
// searches and binds no repository.
func (l *loaded) searchWithoutRepository(s *searchDecl) error {
	return gen.Errorf(s.at.file, s.at.line,
		"bind the repository that serves the search: nanorepo.Implement[YourRepository, yourRepositoryImpl]("+
			s.tableText+", rowToDomain), or nanorepo.Base[YourRow]("+s.tableText+"), in the same func _()",
		"search %s is declared, and package %s binds no repository: a search runs on a method of the "+
			"repository implementation", s.name, l.pkg.Name)
}

// compileErrors returns the compile errors within a declaration call, with
// a misspelt column of a table variable told as E1.
func (c *collector) compileErrors(call *ast.CallExpr) []error {
	misspelt := c.misspeltColumns(call)
	errs := errorsWithin(c.l.pkg, call, c.l.relPath, "fix the declaration so that it compiles")
	return append(misspelt, notOnLines(errs, misspelt)...)
}

// misspeltColumns reports each selector of call that names a field a table
// variable does not have (E1), with the column it most likely meant. The
// compile error says the same with less help.
func (c *collector) misspeltColumns(call *ast.CallExpr) []error {
	info := c.l.pkg.TypesInfo
	var problems []error
	ast.Inspect(call, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || info.Selections[sel] != nil || info.Uses[sel.Sel] != nil {
			return true
		}
		v, isVar := info.Uses[tableIdent(sel.X)].(*types.Var)
		if !isVar || v.Pkg() == nil || v.Parent() != v.Pkg().Scope() {
			return true
		}
		named := tableNamed(v.Type())
		if named == nil || embeddedDialect(named, 0) == "" {
			return true
		}
		var fields []string
		for _, col := range tableColumns(named, nil) {
			fields = append(fields, col.field)
		}
		fix := "name a column of " + c.source(sel.X)
		if best := suggest(sel.Sel.Name, fields); best != "" {
			fix = "did you mean " + c.source(sel.X) + "." + best + "?"
		}
		table := snake(v.Name())
		if src, err := c.l.tableSource(v.Pkg().Path()); err == nil {
			if names, found := src.tables[v.Name()]; found {
				table = names.table
			}
		}
		file, line := c.l.position(c.l.pkg.Fset, sel.Pos())
		problems = append(problems, gen.Errorf(file, line, fix, "%s does not compile: %s has no column %s",
			c.source(sel), table, sel.Sel.Name))
		return false
	})
	return problems
}

// notOnLines returns the problems of errs on a line that no problem of
// reported is on.
func notOnLines(errs, reported []error) []error {
	lines := map[at]bool{}
	for _, r := range reported {
		if e, ok := errors.AsType[*gen.Error](r); ok {
			lines[at{e.File, e.Line}] = true
		}
	}
	var out []error
	for _, err := range errs {
		if e, ok := errors.AsType[*gen.Error](err); ok && lines[at{e.File, e.Line}] {
			continue
		}
		out = append(out, err)
	}
	return out
}
