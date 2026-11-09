package genrepo

import (
	"go/ast"
	"go/token"
	"go/types"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/nanostack-dev/nanostack-framework/gen"
)

// defaultLimit is the page size of a search without DefaultLimit (design
// section 4.2).
const defaultLimit = 20

// builder is what nano gen repo writes for one search (design 4.2): the
// builder type, its scope type, its sort enum, its spec and one accessor per
// filter. The search method is not part of it.
type builder struct {
	s        *searchDecl
	fields   []scopeField // the fields of the scope value, in option order
	sorts    []sortConst  // the sort enum, in option order
	tiebreak []*sortKey
	filters  []*builderField
	from     types.Object // the From function, nil without From
	conds    []types.Object
	ctx      []contextScope
	fullText []fullTextPart
	dflt     *sortConst // the default sort, nil without DefaultSort
}

// scopeField is one field of the scope value: one Scope or ScopeOrNull,
// whose value goes to every column it names.
type scopeField struct {
	name string
	typ  types.Type
	kind string // Scope or ScopeOrNull
	cols []*colRef
}

// sortConst is one value of the sort enum.
type sortConst struct {
	name string // after <Name>Sort, such as ReceivedAt
	api  string // the value, such as received_at
	key  *sortKey
}

// sortKey is one keyset key on a column of the searched table.
type sortKey struct {
	ctor     string // the db/keyset constructor
	col      *colRef
	field    string     // the Row field
	value    types.Type // what the accessor returns
	conv     string     // "": return the field; else the conversion of a NOT NULL field, such as int64
	ptrConv  string     // the conversion of the value of a nullable field, such as int64
	enumType string     // EnumKey: the enum's SQL type name
	labels   []string   // EnumKey: the model constants, in declaration order
	nulls    string     // NullsFirst or NullsLast, else ""
}

// builderField is the accessor of one filter.
type builderField struct {
	f      *searchFilter
	kind   string       // In, Eq, EqIn, InFold, Text, Presence, TimeRange or Flag
	value  types.Type   // V of In, Eq, EqIn and InFold
	lit    string       // the searchexec literal function of In, Eq and EqIn
	labels []string     // EnumLit: the model constants of the enum, in declaration order
	fn     types.Object // the function of FilterExpr and Flag
}

// contextScope is one ScopeFunc.
type contextScope struct {
	fn  types.Object
	col *colRef
}

// fullTextPart is one expression of FullText: a column or a function.
type fullTextPart struct {
	col *colRef
	fn  types.Object
}

// enumTypeName is the form keyset.EnumKey takes for the enum's type name.
var enumTypeName = regexp.MustCompile(`^[a-z_][a-z0-9_.]*$`)

// reservedFields are the methods of the builder that searchexec.Query
// promotes, and the generated FullText: no filter may take their names.
var reservedFields = []string{ //nolint:gochecknoglobals // read-only table.
	"Query", "Scope", "SortBy", "After", "Offset", "Limit", "FullText",
}

// planBuilder works out the builder of a search that reads without a
// problem. A shape that nano gen repo cannot write yet, or cannot write at
// all, is a problem of the search, kept apart from the problems of the
// declarations so that -explain still shows the search.
func (c *collector) planBuilder(s *searchDecl) {
	before := len(c.problems)
	b := &builder{s: s}
	c.builderScope(b)
	c.builderTenant(b)
	c.builderRow(b)
	c.builderFrom(b)
	for _, cond := range s.conditions {
		b.conds = append(b.conds, c.funcObj(s, "Condition", cond))
	}
	for _, sf := range s.scopeFuncs {
		b.ctx = append(b.ctx, contextScope{fn: c.funcObj(s, "ScopeFunc", sf.fn), col: sf.col})
	}
	c.builderSorts(b)
	c.builderFilters(b)
	for _, col := range s.fullText {
		b.fullText = append(b.fullText, fullTextPart{col: col})
	}
	for _, fn := range s.fullTextExprs {
		b.fullText = append(b.fullText, fullTextPart{fn: c.funcObj(s, "FullTextExpr", fn)})
	}
	s.builder = b
	s.unbuilt = append(s.unbuilt, c.problems[before:]...)
	c.problems = c.problems[:before]
}

// builderScope sets the fields of the scope value: one per Scope and
// ScopeOrNull, named after its first column and typed by its model field.
func (c *collector) builderScope(b *builder) {
	for _, sc := range b.s.scopes {
		if sc.kind == "ScopeNull" || len(sc.cols) == 0 {
			continue
		}
		col := sc.cols[0]
		b.fields = append(b.fields, scopeField{name: col.col.field, typ: col.col.value, kind: sc.kind, cols: sc.cols})
	}
}

// builderTenant checks that the search has a tenant condition, as
// searchexec.DefineSpec does when the package starts: a Scope, ScopeOrNull,
// ScopeNull or ScopeFunc. Unscoped is a record, not a condition. Which
// columns need one is the tenant check's question; this is the floor that
// keeps a generated package from panicking.
func (c *collector) builderTenant(b *builder) {
	s := b.s
	if len(s.scopes) > 0 || len(s.scopeFuncs) > 0 {
		return
	}
	fix := "scope the search with nanorepo.Scope(column) or nanorepo.ScopeFunc(fn, column)"
	if fields := s.root.table.scopeFields(c.l.scopes); len(fields) > 0 {
		fix = "scope the search: nanorepo.Scope(" + s.tableText + "." + fields[0] + ")"
	}
	c.errorf(s.tableNode, fix, "search %s has no tenant condition (no Scope, ScopeOrNull, ScopeNull or "+
		"ScopeFunc): searchexec.DefineSpec refuses it when the package starts", s.name)
}

// scopeType is the scope value of the search: struct{} with no field, the
// field's type with one, else the generated <Name>Scope.
func (b *builder) scopeName() string { return b.s.name + "Scope" }

// builderRow checks that the Row is the model of the searched table, whose
// columns the builder selects (a Row of its own is the next step of nano gen
// repo, design S9.1).
func (c *collector) builderRow(b *builder) {
	s := b.s
	if model := s.root.table.model; model == nil || !types.Identical(s.row, model) {
		c.errorf(s.tableNode, "search "+c.qualified(typeOrNil(model))+", the model of "+s.root.table.sqlName+
			", until nano gen repo selects the columns of a Row of its own",
			"search %s: the Row is %s, not the model of %s, and nano gen repo selects only the columns of "+
				"the model yet", s.name, c.qualified(s.row), s.root.table.sqlName)
	}
}

// builderFrom checks that a From function that takes a value takes the
// scope value of the search.
func (c *collector) builderFrom(b *builder) {
	s := b.s
	if s.from == nil {
		return
	}
	b.from = c.funcObj(s, "From", s.from.arg)
	if !s.from.takesScope {
		return
	}
	param := s.from.param
	if named, ok := types.Unalias(param).(*types.Named); ok && len(b.fields) > 1 &&
		named.Obj().Pkg() == c.l.pkg.Types && named.Obj().Name() == b.scopeName() {
		return
	}
	if !isInvalid(param) && len(b.fields) == 1 && types.Identical(param, b.fields[0].typ) {
		return
	}
	if len(b.fields) == 0 && types.Identical(param, types.NewStruct(nil, nil)) {
		return
	}
	if isInvalid(param) && len(b.fields) > 1 && c.namesScope(s) {
		return // the first load has no <Name>Scope yet: the final type check checks it
	}
	want := c.scopeTypeText(b)
	c.errorf(s.from.arg, "declare "+s.from.name+" as func(s "+want+") with the scope value of the search",
		"search %s: From %s takes %s, and the scope value of the search is %s", s.name, s.from.name,
		c.qualified(param), want)
}

// namesScope reports whether the parameter of the From function is
// written as the generated scope type.
func (c *collector) namesScope(s *searchDecl) bool {
	params := s.from.decl.Type.Params.List
	id := identOf(ast.Unparen(params[0].Type))
	return id != nil && id.Name == s.name+"Scope"
}

// scopeTypeText renders the scope value of b for a message.
func (c *collector) scopeTypeText(b *builder) string {
	switch len(b.fields) {
	case 0:
		return "struct{}"
	case 1:
		return c.qualified(b.fields[0].typ)
	}
	return b.scopeName()
}

func isInvalid(t types.Type) bool { return t == nil || t == types.Typ[types.Invalid] }

// funcObj resolves the function an option names. The read set check has
// already read it, so a name that does not resolve is a gap.
func (c *collector) funcObj(s *searchDecl, option string, e ast.Expr) types.Object {
	if obj, ok := c.l.pkg.TypesInfo.Uses[tableIdent(ast.Unparen(e))].(*types.Func); ok &&
		obj.Signature().Recv() == nil {
		return obj
	}
	c.errorf(e, "pass the function by its name, such as "+option+"(requestHost)",
		"search %s: %s(%s) does not name a function", s.name, option, c.source(e))
	return nil
}

// builderNames are the package-level names the builder of s declares.
func (b *builder) names() []string {
	s := b.s
	out := []string{s.name + "Search", s.name + "Sort", "New" + s.name + "Search", specVar(s)}
	if len(b.fields) > 1 {
		out = append(out, b.scopeName())
	}
	for _, sort := range b.sorts {
		out = append(out, s.name+"Sort"+sort.name)
	}
	return slices.Compact(out)
}

// specVar is the package variable that holds the spec of s.
func specVar(s *searchDecl) string { return lowerFirst(s.name) + "Search" }

// checkBuilderNames reports a hand-written package-level name that a
// builder would declare, and two searches whose builders share a name.
func (l *loaded) checkBuilderNames(d *declarations) []error {
	var problems []error
	owner := map[string]*searchDecl{}
	for _, t := range d.tables {
		for _, name := range t.declaredNames() {
			owner[name] = nil
		}
	}
	scope := l.pkg.Types.Scope()
	for _, s := range d.searches {
		if s.builder == nil {
			continue
		}
		for _, name := range s.builder.names() {
			if other, dup := owner[name]; dup {
				what := "the base of a table"
				if other != nil {
					what = "search " + other.name
				}
				problems = append(problems, gen.Errorf(s.at.file, s.at.line, "rename the search",
					"search %s and %s both declare %s", s.name, what, name))
				continue
			}
			owner[name] = s
			if obj := scope.Lookup(name); obj != nil && obj.Pos().IsValid() {
				file, line := l.position(l.pkg.Fset, obj.Pos())
				problems = append(problems, gen.Errorf(file, line, "rename it, or rename the search",
					"%s is declared here, and nano gen repo declares it for search %s", name, s.name))
			}
		}
		problems = append(problems, l.checkBuilderMethods(s)...)
	}
	return problems
}

// checkBuilderMethods reports a hand-written method of the builder of s
// that has the name of a generated accessor or of FullText. The files nano
// gen repo wrote are empty in l.pkg, so every method found is hand-written.
func (l *loaded) checkBuilderMethods(s *searchDecl) []error {
	generated := map[string]bool{}
	for _, f := range s.builder.filters {
		generated[f.f.name] = true
	}
	if len(s.builder.fullText) > 0 {
		generated["FullText"] = true
	}
	var problems []error
	for _, f := range l.pkg.Syntax {
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Recv == nil || len(fd.Recv.List) != 1 || !generated[fd.Name.Name] {
				continue
			}
			if recv, _ := recvType(ast.Unparen(fd.Recv.List[0].Type)); recv != s.name+"Search" {
				continue
			}
			fix := "rename the method, or name the filter with nanorepo.FilterExpr"
			if fd.Name.Name == "FullText" {
				fix = "rename the method: the builder declares FullText for nanorepo.FullText and FullTextExpr"
			}
			file, line := l.position(l.pkg.Fset, fd.Pos())
			problems = append(problems, gen.Errorf(file, line, fix,
				"%sSearch.%s is declared here, and nano gen repo declares it for search %s", s.name,
				fd.Name.Name, s.name))
		}
	}
	return problems
}

// goName makes a Go name of an API name: received_at -> ReceivedAt,
// ip_address -> IPAddress, with the initialisms that gofmt users expect.
func goName(api string) string {
	var b strings.Builder
	for word := range strings.FieldsFuncSeq(api, func(r rune) bool { return r == '_' || r == '-' || r == '.' }) {
		if up := strings.ToUpper(word); slices.Contains(initialisms, up) {
			b.WriteString(up)
			continue
		}
		b.WriteString(strings.ToUpper(word[:1]) + word[1:])
	}
	out := b.String()
	if out == "" || !token.IsIdentifier(out) {
		return "Value" + strconv.Itoa(len(api))
	}
	return out
}

// initialisms are the words goName writes in capitals.
var initialisms = []string{ //nolint:gochecknoglobals // read-only table.
	"ID", "IP", "URL", "URI", "API", "HTTP", "JSON", "SQL", "UUID", "UI", "TTL",
}
