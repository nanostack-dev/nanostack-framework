package genrepo

import (
	"go/ast"
	"go/constant"
	"go/types"
	"slices"
	"strings"
	"unicode"

	"github.com/nanostack-dev/nanostack-framework/db/search"
)

// optionFix is the fix of an argument of nanorepo.Search that nano gen repo
// cannot read.
const optionFix = "write each option as a call of a nanorepo function in the nanorepo.Search call, " +
	"such as nanorepo.Filter(table.Items.Name, nanorepo.Eq)"

// onceOptions are the options that a search takes at most once.
var onceOptions = map[string]bool{ //nolint:gochecknoglobals // read-only table.
	"Map": true, "MapPage": true, "From": true, "Tiebreak": true, "DefaultSort": true, "DefaultSortExpr": true,
	"DefaultLimit": true, "API": true, "IgnoresFullText": true,
}

// goOnlyOptions are the options that GoOnly wraps: a filter, or FullText.
var goOnlyOptions = []string{ //nolint:gochecknoglobals // read-only table.
	"Filter", "FilterAs", "FilterExpr", "FilterExists", "Flag", "FullText", "FullTextExpr",
}

// onceGroup names the group of a once-option: Map and MapPage exclude each
// other, as DefaultSort and DefaultSortExpr do.
func onceGroup(name string) string {
	switch name {
	case "MapPage":
		return "Map"
	case "DefaultSortExpr":
		return "DefaultSort"
	}
	return name
}

// searchOption reads one argument of nanorepo.Search: a direct call of an
// option function of nanorepo, as the package documents.
func (c *collector) searchOption(s *searchDecl, arg ast.Expr, goOnly bool) {
	call, isCall := ast.Unparen(arg).(*ast.CallExpr)
	var fn *types.Func
	if isCall {
		fn = c.nanorepoFunc(call)
	}
	if fn == nil {
		c.errorf(arg, optionFix, "search %s: the argument %s is not a call of a nanorepo option; "+
			"nano gen repo reads each option from source", s.name, c.source(arg))
		return
	}
	name := fn.Name()
	if sig, _ := fn.Type().(*types.Signature); sig.Recv() != nil {
		name = "SortKey" // a method of SortSpec: SortKey(...).API(...), NullsFirst(), NullsLast()
	}
	if onceOptions[name] {
		file, line := c.l.position(c.l.pkg.Fset, call.Pos())
		if first, dup := s.given[onceGroup(name)]; dup {
			what := name + " is given twice"
			if other := s.givenAs[onceGroup(name)]; other != name {
				what = other + " and " + name + " exclude each other"
			}
			c.errorf(call, "keep one of the two", "search %s: %s: %s and %s", s.name, what, first, at{file, line})
			return
		}
		s.given[onceGroup(name)], s.givenAs[onceGroup(name)] = at{file, line}, name
	}
	if call.Ellipsis.IsValid() {
		c.errorf(call, optionFix, "search %s: %s spreads a slice; nano gen repo reads each argument from source",
			s.name, c.source(call))
		return
	}
	c.option(s, name, call, goOnly)
}

// option dispatches one option call by its function name.
func (c *collector) option(s *searchDecl, name string, call *ast.CallExpr, goOnly bool) {
	if goOnly && !slices.Contains(goOnlyOptions, name) {
		c.errorf(call, "wrap only a filter or FullText in nanorepo.GoOnly",
			"search %s: GoOnly(%s) marks no filter and no full text", s.name, c.source(call))
		return
	}
	switch name {
	case "GoOnly":
		c.searchOption(s, call.Args[0], true)
	case "Map", "MapPage":
		s.mapper = &searchMapper{page: name == "MapPage", fn: call.Args[0]}
	case "From":
		c.from(s, call.Args[0])
	case "Scope", "ScopeOrNull", "ScopeNull":
		s.scopes = append(s.scopes, scopeDecl{kind: name, cols: c.columnRefs(s, call.Args)})
	case "ScopeFunc":
		if col := c.columnRef(s, call.Args[1]); col != nil {
			s.scopeFuncs = append(s.scopeFuncs, scopeFuncDecl{fn: call.Args[0], col: col})
		}
	case "Unscoped":
		if col := c.columnRef(s, call.Args[0]); col != nil {
			if why, ok := c.reason(s, name, call.Args[1]); ok {
				s.unscoped = append(s.unscoped, unscopedDecl{col: col, why: why})
			}
		}
	case "Condition":
		s.conditions = append(s.conditions, call.Args[0])
		s.exprs = append(s.exprs, exprFunc{role: name, fn: call.Args[0]})
	case "Filter", "FilterAs", "FilterExpr", "FilterExists", "Flag":
		c.filter(s, name, call, goOnly)
	case "FullText", "FullTextExpr":
		c.fullText(s, name, call, goOnly)
	case "Sort":
		for _, col := range c.columnRefs(s, call.Args) {
			s.sorts = append(s.sorts, &searchSort{kind: name, col: col, api: []string{col.col.sqlName}})
		}
	case "SortKey":
		if sort := c.sortKey(s, call); sort != nil {
			s.sorts = append(s.sorts, sort)
		}
	case "SortExpr":
		if field, ok := c.constString(s, call.Args[0]); ok {
			s.sorts = append(s.sorts,
				&searchSort{kind: name, field: field, expr: call.Args[1], api: []string{snake(field)}})
			s.exprs = append(s.exprs, exprFunc{role: name + " " + field, fn: call.Args[1]})
		}
	case "SortHandWritten":
		if api, ok := c.constString(s, call.Args[0]); ok {
			s.sorts = append(s.sorts, &searchSort{kind: name, api: []string{api}})
		}
	default:
		c.limitsAndAPI(s, name, call)
	}
}

// fullText reads FullText and FullTextExpr, with the Go-only mark of each
// entry.
func (c *collector) fullText(s *searchDecl, name string, call *ast.CallExpr, goOnly bool) {
	if name == "FullTextExpr" {
		for _, fn := range call.Args {
			s.fullTextGoOnly[fn] = goOnly
			s.exprs = append(s.exprs, exprFunc{role: name, fn: fn})
		}
		s.fullTextExprs = append(s.fullTextExprs, call.Args...)
		return
	}
	cols := c.columnRefs(s, call.Args)
	for _, col := range cols {
		s.fullTextGoOnly[col.expr] = goOnly
	}
	s.fullText = append(s.fullText, cols...)
}

// limitsAndAPI reads the order, page and API options.
func (c *collector) limitsAndAPI(s *searchDecl, name string, call *ast.CallExpr) {
	switch name {
	case "Tiebreak":
		s.tiebreak, s.tiebreakNode = c.columnRefs(s, call.Args), call
	case "DefaultSort":
		col := c.columnRef(s, call.Args[0])
		if desc, ok := c.descending(s, call.Args[1]); ok && col != nil {
			s.defaultSort = &defaultSort{col: col, desc: desc, node: call}
		}
	case "DefaultSortExpr":
		field, ok := c.constString(s, call.Args[0])
		if desc, isDir := c.descending(s, call.Args[1]); ok && isDir {
			s.defaultSort = &defaultSort{field: field, desc: desc, node: call}
		}
	case "DefaultLimit":
		tv := c.l.pkg.TypesInfo.Types[call.Args[0]]
		n, exact := int64(0), false
		if tv.Value != nil {
			n, exact = constant.Int64Val(constant.ToInt(tv.Value))
		}
		if !exact || n < 1 || n > search.MaxLimit {
			c.errorf(call, "pass a constant between 1 and 1000 (search.MaxLimit), such as nanorepo.DefaultLimit(20)",
				"search %s: DefaultLimit(%s) is not a constant between 1 and %d: search.CheckPage refuses "+
					"any other page size, so every request that gives no limit would fail", s.name,
				c.source(call.Args[0]), search.MaxLimit)
			return
		}
		s.defaultLimit = int32(n) // 1 to search.MaxLimit
	case "API":
		if inst, ok := c.l.pkg.TypesInfo.Instances[c.funcIdent(call)]; ok && inst.TypeArgs.Len() == 1 {
			s.api, s.apiNode = inst.TypeArgs.At(0), call
		}
	case "IgnoresFullText":
		s.ignoresFullText, _ = c.reason(s, name, call.Args[0])
		s.ignoresNode = call
	case "APIHandWritten":
		if s.handNode == nil {
			s.handNode = call
		}
		for _, a := range call.Args {
			if prop, ok := c.constString(s, a); ok {
				s.apiHandWritten = append(s.apiHandWritten, prop)
			}
		}
	}
}

// descending reads nanorepo.Asc or nanorepo.Desc. A direction that is not
// a constant is a problem: read as ascending, it would change the order.
func (c *collector) descending(s *searchDecl, arg ast.Expr) (desc, ok bool) {
	tv := c.l.pkg.TypesInfo.Types[arg]
	if tv.Value == nil || tv.Value.Kind() != constant.Int {
		c.errorf(arg, "pass nanorepo.Asc or nanorepo.Desc",
			"search %s: %s is not a constant direction", s.name, c.source(arg))
		return false, false
	}
	n, _ := constant.Int64Val(tv.Value)
	return n == 1, true
}

// reason reads the reason of Unscoped or IgnoresFullText: a constant string
// that is not blank, because a reviewer reads it in -explain and in the
// exemption file.
func (c *collector) reason(s *searchDecl, option string, arg ast.Expr) (string, bool) {
	why, ok := c.constString(s, arg)
	if !ok {
		return "", false
	}
	if strings.TrimSpace(why) == "" {
		c.errorf(arg, "pass the reason a reviewer reads",
			"search %s: %s has a blank reason", s.name, option)
		return "", false
	}
	return why, true
}

// nanorepoFunc returns the nanorepo function or method that call calls, or
// nil.
func (c *collector) nanorepoFunc(call *ast.CallExpr) *types.Func {
	fn, _ := calledFunc(c.l.pkg.TypesInfo, call)
	if fn == nil {
		if sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok {
			fn, _ = c.l.pkg.TypesInfo.Uses[sel.Sel].(*types.Func)
		}
	}
	if fn == nil || fn.Pkg() == nil || fn.Pkg().Path() != nanorepoPath {
		return nil
	}
	return fn
}

// funcIdent returns the identifier of the function a call names.
func (c *collector) funcIdent(call *ast.CallExpr) *ast.Ident {
	_, sel := calledFunc(c.l.pkg.TypesInfo, call)
	if sel == nil {
		return nil
	}
	return sel.Sel
}

// constString reads a constant string argument. A string with a control
// character is refused: -explain and the exemption file print each string
// on one line, and a newline could forge a line.
func (c *collector) constString(s *searchDecl, arg ast.Expr) (string, bool) {
	tv := c.l.pkg.TypesInfo.Types[arg]
	if tv.Value == nil || tv.Value.Kind() != constant.String {
		c.errorf(arg, "pass a string literal", "search %s: %s is not a constant string", s.name, c.source(arg))
		return "", false
	}
	str := constant.StringVal(tv.Value)
	if strings.ContainsFunc(str, unicode.IsControl) {
		c.errorf(arg, "remove the control characters",
			"search %s: %s holds a control character, such as a newline", s.name, c.source(arg))
		return "", false
	}
	return str, true
}

// columnRefs reads column arguments.
func (c *collector) columnRefs(s *searchDecl, args []ast.Expr) []*colRef {
	out := make([]*colRef, 0, len(args))
	for _, a := range args {
		if col := c.columnRef(s, a); col != nil {
			out = append(out, col)
		}
	}
	return out
}

// columnRef reads a column argument: a column field of a table variable,
// such as table.Items.Name, or of an instance with an alias, such as
// table.Tenants.AS("owner").Name.
func (c *collector) columnRef(s *searchDecl, arg ast.Expr) *colRef {
	sel, ok := ast.Unparen(arg).(*ast.SelectorExpr)
	if !ok {
		c.errorf(arg, "name a column of a generated table, such as table.Items.Name",
			"search %s: %s is not a column of a table variable", s.name, c.source(arg))
		return nil
	}
	tableExpr, alias := ast.Unparen(sel.X), ""
	if as, isAS := c.aliasCall(tableExpr); isAS {
		var isString bool
		if alias, isString = c.aliasName(s, as.Args[0], tableOperand(tableExpr)); !isString {
			return nil
		}
		tableExpr = tableOperand(tableExpr)
	}
	if _, isTable := tableExpr.(*ast.SelectorExpr); !isTable {
		if _, isIdent := tableExpr.(*ast.Ident); !isIdent {
			c.errorf(arg, "name a column of a generated table, such as table.Items.Name",
				"search %s: %s is not a column of a table variable", s.name, c.source(arg))
			return nil
		}
	}
	t := c.searchTable(s, tableExpr)
	if t == nil {
		return nil
	}
	col, found := t.column(sel.Sel.Name)
	if !found {
		c.errorf(arg, "name a column of a generated table, such as table.Items.Name",
			"search %s: %s is not a column of %s", s.name, c.source(arg), t.sqlName)
		return nil
	}
	return &colRef{expr: arg, table: t, alias: alias, tableText: c.source(tableExpr), col: col}
}

// aliasCall reports whether e is X.AS(alias) on a table variable X.
func (c *collector) aliasCall(e ast.Expr) (*ast.CallExpr, bool) {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return nil, false
	}
	sel, isSel := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !isSel || sel.Sel.Name != "AS" {
		return nil, false
	}
	v, isVar := c.l.pkg.TypesInfo.Uses[tableIdent(sel.X)].(*types.Var)
	return call, isVar && tableNamed(v.Type()) != nil
}

// filter reads Filter, FilterAs, FilterExpr, FilterExists and Flag.
func (c *collector) filter(s *searchDecl, name string, call *ast.CallExpr, goOnly bool) {
	f := &searchFilter{kind: name, goOnly: goOnly, node: call}
	var ops []ast.Expr
	switch name {
	case "Filter", "FilterAs":
		if f.col = c.columnRef(s, call.Args[0]); f.col == nil {
			return
		}
		f.name, ops = f.col.col.field, call.Args[1:]
		if inst, ok := c.l.pkg.TypesInfo.Instances[c.funcIdent(call)]; ok && name == "FilterAs" {
			f.as = inst.TypeArgs.At(0)
		}
	case "FilterExists":
		if f.child = c.searchTable(s, call.Args[0]); f.child == nil {
			return
		}
		if f.link, f.col = c.columnRef(s, call.Args[1]), c.columnRef(s, call.Args[2]); f.link == nil || f.col == nil {
			return
		}
		f.name, ops = f.child.goName+f.col.col.field, call.Args[3:]
	case "FilterExpr", "Flag":
		var ok bool
		if f.name, ok = c.constString(s, call.Args[0]); !ok {
			return
		}
		f.expr, ops = call.Args[1], call.Args[2:]
		s.exprs = append(s.exprs, exprFunc{role: name + " " + f.name, fn: f.expr})
	}
	for _, a := range ops {
		op, ok := c.op(s, a)
		if !ok {
			c.errorf(a, "pass nanorepo.Eq, In, InFold, Contains, ContainsAny, Present or Range(...), "+
				"optionally with .API(\"property\")",
				"search %s: %s is not a nanorepo operator", s.name, c.source(a))
			return
		}
		f.ops = append(f.ops, op)
	}
	s.filters = append(s.filters, f)
}

// op reads an operator: nanorepo.In, nanorepo.In.API("ids"),
// nanorepo.Range("since", "until").HalfOpen().
func (c *collector) op(s *searchDecl, arg ast.Expr) (searchOp, bool) {
	info := c.l.pkg.TypesInfo
	switch x := ast.Unparen(arg).(type) {
	case *ast.SelectorExpr, *ast.Ident:
		id, _ := x.(*ast.Ident)
		if sel, isSel := x.(*ast.SelectorExpr); isSel {
			id = sel.Sel
		}
		if v, ok := info.Uses[id].(*types.Var); ok && v.Pkg() != nil && v.Pkg().Path() == nanorepoPath &&
			v.Parent() == v.Pkg().Scope() {
			return searchOp{name: v.Name(), node: arg}, true
		}
	case *ast.CallExpr:
		fn := c.nanorepoFunc(x)
		if fn == nil || x.Ellipsis.IsValid() {
			return searchOp{}, false
		}
		if fn.Name() == "Range" {
			return searchOp{name: "Range", api: c.strings(s, x.Args), node: arg}, true
		}
		sel, isSel := ast.Unparen(x.Fun).(*ast.SelectorExpr)
		if !isSel {
			return searchOp{}, false
		}
		op, ok := c.op(s, sel.X)
		switch fn.Name() {
		case "API":
			op.api = c.strings(s, x.Args)
		case "HalfOpen", "Exclusive":
			op.bounds = fn.Name()
		default:
			return searchOp{}, false
		}
		op.node = arg
		return op, ok
	}
	return searchOp{}, false
}

// strings reads the constant strings of args, the API names of an
// operator or a sort. A name that is not a constant string is a problem:
// left out, it would change what the API binds (Range("since", until)
// would bind the lower bound only).
func (c *collector) strings(s *searchDecl, args []ast.Expr) []string {
	var out []string
	for _, a := range args {
		if str, ok := c.constString(s, a); ok {
			out = append(out, str)
		}
	}
	return out
}

// sortKey reads nanorepo.SortKey(column) and its chain of .API(...),
// .NullsFirst() and .NullsLast().
func (c *collector) sortKey(s *searchDecl, call *ast.CallExpr) *searchSort {
	fn := c.nanorepoFunc(call)
	if fn.Name() == "SortKey" {
		col := c.columnRef(s, call.Args[0])
		if col == nil {
			return nil
		}
		return &searchSort{kind: "SortKey", col: col, api: []string{col.col.sqlName}}
	}
	sel, _ := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	inner, isCall := ast.Unparen(sel.X).(*ast.CallExpr)
	if !isCall || c.nanorepoFunc(inner) == nil {
		c.errorf(call, optionFix, "search %s: %s does not start from nanorepo.SortKey(column)", s.name, c.source(call))
		return nil
	}
	sort := c.sortKey(s, inner)
	if sort == nil {
		return nil
	}
	switch fn.Name() {
	case "API":
		if len(call.Args) == 0 {
			c.errorf(call, `name the sort_by values, such as .API("name")`,
				"search %s: %s gives no sort_by value", s.name, strings.TrimSpace(c.source(call)))
			return nil
		}
		sort.api = c.strings(s, call.Args)
	case "NullsFirst", "NullsLast":
		sort.nulls = fn.Name()
	}
	return sort
}
